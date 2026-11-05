package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/aiassist"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/gpumetrics"
	"slurm-console/backend/internal/linuxusers"
	"slurm-console/backend/internal/platformsettings"
	"slurm-console/backend/internal/rbac"
)

func registerPlatformSettingsRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	group.GET("/platform-info", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, err := platformsettings.Read(e.App)
		if err != nil {
			return e.Error(503, "平台信息暂不可用。", nil)
		}
		return response(e, v.Identity)
	})
	r := group.Group("/platform-settings")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("platform.settings.read", false))
	r.GET("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, err := platformsettings.Read(e.App)
		if err != nil {
			return e.Error(503, "设置读取失败。", nil)
		}
		return response(e, v)
	})
	r.GET("/ai", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, _, err := platformsettings.AIConfig(e.App)
		if err != nil {
			return e.Error(503, "AI 设置读取失败。", nil)
		}
		return response(e, v)
	})
	r.GET("/runtime", platformRuntime)
	r.PATCH("", savePlatformSettings).BindFunc(rbac.Require("platform.settings.manage", false))
	r.PATCH("/ai", savePlatformAI).BindFunc(rbac.Require("platform.settings.manage", false))
}
func platformFresh(e *core.RequestEvent, app core.App, permissions ...string) error {
	a, err := rbac.Resolve(app, e.Auth.Id)
	if err != nil {
		return err
	}
	for _, p := range permissions {
		if !a.Allows(p, "") {
			return e.ForbiddenError("平台管理权限已撤销或账户已停用。", nil)
		}
	}
	return nil
}
func platformText(v string, min, max int, multi bool) bool {
	return utf8.ValidString(v) && v == strings.TrimSpace(v) && utf8.RuneCountInString(v) >= min && utf8.RuneCountInString(v) <= max && !strings.ContainsFunc(v, func(r rune) bool { return unicode.IsControl(r) && !(multi && r == '\n') || unicode.Is(unicode.Cf, r) })
}
func platformJSON(raw []byte, target any, required ...string) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) error
	value = func(depth int) error {
		if depth > 16 {
			return errors.New("nesting")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok {
					return errors.New("key")
				}
				key = strings.ToLower(key)
				if keys[key] {
					return errors.New("duplicate")
				}
				keys[key] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		default:
			return errors.New("delimiter")
		}
		_, e = d.Token()
		return e
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("object required")
	}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return errors.New("field required")
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
func platformBody(e *core.RequestEvent, target any, required ...string) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	raw, err := io.ReadAll(http.MaxBytesReader(e.Response, e.Request.Body, 16384))
	if err != nil || platformJSON(raw, target, required...) != nil {
		return e.BadRequestError("请提交完整 JSON 字段，不允许未知字段、重复键或空值。", nil)
	}
	return nil
}
func platformFailure(e *core.RequestEvent, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, platformsettings.ErrConflict) {
		return e.Error(409, "设置或用户授权已变化，请刷新后重新核对。", nil)
	}
	var apiError *router.ApiError
	if errors.As(err, &apiError) {
		return err
	}
	return e.Error(503, "保存未完成，事务已回滚，请刷新后核对。", nil)
}
func savePlatformSettings(e *core.RequestEvent) error {
	var in struct {
		platformsettings.Identity
		Revision int `json:"revision"`
	}
	if err := platformBody(e, &in, "name", "description", "loginNotice", "supportContact", "revision"); err != nil {
		return err
	}
	if !platformText(in.Name, 2, 64, false) || !platformText(in.Description, 0, 240, true) || !platformText(in.LoginNotice, 0, 1000, true) || !platformText(in.SupportContact, 0, 160, false) || in.Revision < 1 {
		return e.BadRequestError("名称、说明、公告、联系方式或修订号不合法。", nil)
	}
	mutex := platformsettings.WriteMutex(e.App)
	mutex.Lock()
	defer mutex.Unlock()
	err := audit.Mutate(e.App, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, "platform.settings.read", "platform.settings.manage"); err != nil {
			return audit.Event{}, err
		}
		r, err := tx.FindRecordById("platform_settings", platformsettings.ID)
		if err != nil {
			return audit.Event{}, e.Error(503, "设置读取失败。", nil)
		}
		if r.GetInt("revision") != in.Revision {
			return audit.Event{}, platformsettings.ErrConflict
		}
		fields := [][3]string{{"platformName", r.GetString("name"), in.Name}, {"platformDescription", r.GetString("description"), in.Description}, {"loginNotice", r.GetString("login_notice"), in.LoginNotice}, {"supportContact", r.GetString("support_contact"), in.SupportContact}}
		changes := []audit.Change{}
		for _, v := range fields {
			if v[1] != v[2] {
				changes = append(changes, audit.Change{Field: v[0], Before: v[1], After: v[2]})
			}
		}
		if len(changes) == 0 {
			return audit.Event{}, e.BadRequestError("设置没有变化。", nil)
		}
		platformsettings.Set(r, in.Identity)
		r.Set("revision", in.Revision+1)
		if err = tx.Save(r); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "platform.settings.update", TargetType: "platform_settings", TargetID: platformsettings.ID, Changes: changes, Message: "已更新公开的平台基础信息。"}, nil
	})
	if err != nil {
		return platformFailure(e, err)
	}
	v, err := platformsettings.Read(e.App)
	if err != nil {
		return e.Error(503, "设置已保存，请刷新核对。", nil)
	}
	return response(e, v)
}
func savePlatformAI(e *core.RequestEvent) error {
	var in struct {
		Config       json.RawMessage `json:"config"`
		Revision     int             `json:"revision"`
		Confirmation string          `json:"confirmation"`
	}
	if err := platformBody(e, &in, "config", "revision", "confirmation"); err != nil {
		return err
	}
	var cfg platformsettings.AI
	if platformJSON(in.Config, &cfg, "enabled", "label", "endpoint", "model", "clusterIds", "timeoutSeconds", "maxOutputTokens", "tokenParameter") != nil || in.Confirmation != "APPLY AI" || in.Revision < 1 || !platformText(cfg.Label, 1, 100, false) || !platformText(cfg.Endpoint, 0, 2048, false) || !platformText(cfg.Model, 0, 120, false) || cfg.ClusterIDs == nil || len(cfg.ClusterIDs) > 100 || cfg.TimeoutSeconds < 5 || cfg.TimeoutSeconds > 60 || cfg.MaxOutputTokens < 512 || cfg.MaxOutputTokens > 4096 || cfg.TokenParameter != "max_tokens" && cfg.TokenParameter != "max_completion_tokens" {
		return e.BadRequestError("AI 配置不合法，须核对接收地址与集群并输入 APPLY AI。", nil)
	}
	seen := map[string]bool{}
	for _, id := range cfg.ClusterIDs {
		if !configVersionID.MatchString(id) || seen[id] {
			return e.BadRequestError("AI 集群列表不合法。", nil)
		}
		seen[id] = true
		if _, err := e.App.FindRecordById("clusters", id); err != nil {
			return e.BadRequestError("AI 集群不存在。", nil)
		}
	}
	profile, err := platformsettings.Profile(e.App, cfg)
	if err != nil {
		return e.BadRequestError("AI 启用校验失败，请核对 HTTPS 地址、模型、集群及服务器密钥/CA 配置。", nil)
	}
	mutex := platformsettings.WriteMutex(e.App)
	mutex.Lock()
	defer mutex.Unlock()
	err = audit.Mutate(e.App, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, "platform.settings.read", "platform.settings.manage"); err != nil {
			return audit.Event{}, err
		}
		r, err := tx.FindRecordById("platform_settings", platformsettings.ID)
		if err != nil {
			return audit.Event{}, err
		}
		if r.GetInt("revision") != in.Revision {
			return audit.Event{}, platformsettings.ErrConflict
		}
		old, _, err := platformsettings.AIConfig(tx)
		if err != nil {
			return audit.Event{}, err
		}
		before, _ := json.Marshal(old.AI)
		after, _ := json.Marshal(cfg)
		if bytes.Equal(before, after) && old.Source == "platform" {
			return audit.Event{}, e.BadRequestError("AI 设置没有变化。", nil)
		}
		r.Set("ai_override", cfg)
		r.Set("revision", in.Revision+1)
		if err = tx.Save(r); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "platform.ai.update", TargetType: "platform_settings", TargetID: platformsettings.ID, Changes: []audit.Change{{Field: "aiConfiguration", Before: string(before), After: string(after)}}, Message: "已保存非敏感 AI 配置并使旧预览失效；未发送模型请求。"}, nil
	})
	if err != nil {
		return platformFailure(e, err)
	}
	aiassist.Register(e.App, profile)
	v, _, err := platformsettings.AIConfig(e.App)
	if err != nil {
		return e.Error(503, "AI 配置已保存，请刷新核对。", nil)
	}
	return response(e, v)
}
func platformRuntime(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	pam, ldap := auth.ConfigurationStatus(e.App)
	ai, _, err := platformsettings.AIConfig(e.App)
	if err != nil {
		return e.Error(503, "运行信息暂不可用。", nil)
	}
	count := len(clusterconfig.For(e.App).Registry.Views())
	metrics := gpumetrics.For(e.App).ConfiguredCount()
	linux := linuxusers.For(e.App).ConfiguredCount()
	return response(e, map[string]any{"runtime": platformsettings.Runtime(), "integrations": []map[string]any{{"id": "local", "name": "平台密码认证", "configured": true, "count": 0}, {"id": "pam", "name": "PAM 系统认证", "configured": pam, "count": 0}, {"id": "ldap", "name": "LDAP 目录认证", "configured": ldap, "count": 0}, {"id": "freeipa", "name": "FreeIPA 目录服务", "configured": auth.FreeIPA(e.App) != nil, "count": 0}, {"id": "slurm", "name": "Slurm 连接方式", "configured": count > 0, "count": count}, {"id": "metrics", "name": "Prometheus / DCGM", "configured": metrics > 0, "count": metrics}, {"id": "linux", "name": "Linux 用户管理来源", "configured": linux > 0, "count": linux}, {"id": "ai", "name": "AI 运维辅助", "configured": ai.Enabled, "count": len(ai.ClusterIDs)}}})
}
