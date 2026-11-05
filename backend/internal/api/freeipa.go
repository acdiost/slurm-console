package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/freeipa"
)

const directoryRead = "platform.directory.read"
const directoryManage = "platform.directory.manage"

var directoryWriteLock sync.Mutex
var directoryOperationID = regexp.MustCompile(`^[A-Za-z0-9-]{20,64}$`)

func registerFreeIPARoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/directory")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbacDirectoryRead)
	r.GET("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		p := auth.FreeIPA(e.App)
		if p == nil {
			return response(e, freeipa.Info{})
		}
		return response(e, p.Info())
	})
	r.GET("/users", directoryUsers)
	r.GET("/groups", directoryGroups)
	r.POST("/changes", directoryChange).BindFunc(func(e *core.RequestEvent) error {
		if err := platformFresh(e, e.App, directoryManage); err != nil {
			return err
		}
		return e.Next()
	})
	r.GET("/operations/{operationId}", directoryOperation)
	r.GET("/operations", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		rows, err := e.App.FindRecordsByFilter("operation_audits", "actor_id={:actor} && target_type='directory'", "-created,-id", 10, 0, dbx.Params{"actor": e.Auth.Id})
		if err != nil {
			return directoryFailure(e, err)
		}
		items := []map[string]any{}
		for _, r := range rows {
			items = append(items, directoryRecordView(r))
		}
		return response(e, map[string]any{"items": items})
	})
	r.GET("/role-mappings", directoryMappings).BindFunc(directoryRolePermission)
	r.POST("/role-mappings", createDirectoryMapping).BindFunc(directoryRolePermission)
	r.DELETE("/role-mappings/{mappingId}", deleteDirectoryMapping).BindFunc(directoryRolePermission)
}
func rbacDirectoryRead(e *core.RequestEvent) error {
	if err := platformFresh(e, e.App, directoryRead); err != nil {
		return err
	}
	return e.Next()
}
func directoryRolePermission(e *core.RequestEvent) error {
	if err := platformFresh(e, e.App, "platform.roles.manage"); err != nil {
		return err
	}
	return e.Next()
}
func directoryProvider(e *core.RequestEvent) (freeipa.Provider, error) {
	p := auth.FreeIPA(e.App)
	if p == nil {
		return nil, e.Error(503, "尚未配置 FreeIPA。", nil)
	}
	return p, nil
}
func directoryFailure(e *core.RequestEvent, err error) error {
	if errors.Is(err, freeipa.ErrNotFound) {
		return e.NotFoundError("目录条目不存在。", nil)
	}
	if errors.Is(err, freeipa.ErrDenied) || errors.Is(err, freeipa.ErrRejected) {
		return e.BadRequestError("目录拒绝操作，请核对身份、权限或重复条目。", nil)
	}
	var apiErr *router.ApiError
	if errors.As(err, &apiErr) {
		return err
	}
	return e.Error(503, "FreeIPA 暂不可用，或查询超过上限；请缩小筛选后重试。", nil)
}
func directoryQuery(e *core.RequestEvent) (int, int, string, error) {
	if err := slurmQuery(e, "page", "perPage", "q"); err != nil {
		return 0, 0, "", err
	}
	p, n, err := pagination(e)
	q := e.Request.URL.Query().Get("q")
	if !platformText(q, 0, 100, false) {
		return 0, 0, "", e.BadRequestError("目录搜索条件不合法。", nil)
	}
	return p, n, q, err
}
func directoryUsers(e *core.RequestEvent) error {
	page, size, q, err := directoryQuery(e)
	if err != nil {
		return err
	}
	p, err := directoryProvider(e)
	if err != nil {
		return err
	}
	items, err := p.Users(e.Request.Context(), q)
	if err != nil {
		return directoryFailure(e, err)
	}
	if err = platformFresh(e, e.App, directoryRead); err != nil {
		return err
	}
	total := len(items)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	return response(e, map[string]any{"items": items[start:end], "totalItems": total, "page": page, "perPage": size})
}
func directoryGroups(e *core.RequestEvent) error {
	page, size, q, err := directoryQuery(e)
	if err != nil {
		return err
	}
	p, err := directoryProvider(e)
	if err != nil {
		return err
	}
	items, err := p.Groups(e.Request.Context(), q)
	if err != nil {
		return directoryFailure(e, err)
	}
	if err = platformFresh(e, e.App, directoryRead); err != nil {
		return err
	}
	total := len(items)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	return response(e, map[string]any{"items": items[start:end], "totalItems": total, "page": page, "perPage": size})
}

type directoryInput struct {
	freeipa.Change
	OperationID  string `json:"operationId"`
	Version      string `json:"version"`
	Confirmation string `json:"confirmation"`
	Reason       string `json:"reason"`
}

func directoryRecordView(r *core.Record) map[string]any {
	return map[string]any{"operationId": r.GetString("operation_key"), "action": r.GetString("action"), "target": r.GetString("target_id"), "status": r.GetString("status"), "message": r.GetString("message"), "updatedAt": r.GetString("updated")}
}
func directoryOperation(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	r, err := e.App.FindFirstRecordByFilter("operation_audits", "operation_key={:key} && actor_id={:actor} && target_type='directory'", dbx.Params{"key": e.Request.PathValue("operationId"), "actor": e.Auth.Id})
	if err != nil {
		return e.NotFoundError("操作不存在。", nil)
	}
	return response(e, directoryRecordView(r))
}
func protectedGroup(name string) bool {
	return slices.Contains([]string{"admins", "trust admins", "editors", "ipausers"}, name)
}
func directoryChange(e *core.RequestEvent) error {
	var in directoryInput
	if err := platformBody(e, &in, "action", "target", "member", "firstName", "lastName", "email", "description", "operationId", "version", "confirmation", "reason"); err != nil {
		return err
	}
	if !freeipa.Name.MatchString(in.Target) || in.Confirmation != in.Target || !platformText(in.Reason, 2, 220, false) || len(in.OperationID) < 20 || len(in.OperationID) > 64 || !directoryOperationID.MatchString(in.OperationID) {
		return e.BadRequestError("核对目标、操作编号与原因，并输入完整目标名称确认。", nil)
	}
	actions := []string{"user.create", "user.enable", "user.disable", "group.create", "group.add-member", "group.remove-member"}
	if !slices.Contains(actions, in.Action) {
		return e.BadRequestError("不支持的目录操作。", nil)
	}
	isCreate := strings.HasSuffix(in.Action, "create")
	if !isCreate && len(in.Version) != 64 {
		return e.BadRequestError("请刷新目录条目后重试。", nil)
	}
	if in.Action == "user.create" && (!platformText(in.FirstName, 1, 100, false) || !platformText(in.LastName, 1, 100, false)) {
		return e.BadRequestError("创建用户须提供姓和名。", nil)
	}
	if in.Email != "" {
		a, err := mail.ParseAddress(in.Email)
		if err != nil || a.Address != in.Email || len(in.Email) > 255 {
			return e.BadRequestError("邮箱格式不合法。", nil)
		}
	}
	if !platformText(in.Description, 0, 500, false) {
		return e.BadRequestError("组描述不合法。", nil)
	}
	p, err := directoryProvider(e)
	if err != nil {
		return err
	}
	if p.ProtectedUser(in.Target) && strings.HasPrefix(in.Action, "user.") || strings.HasPrefix(in.Action, "group.") && protectedGroup(in.Target) || in.Member != "" && p.ProtectedUser(in.Member) {
		return e.ForbiddenError("此内置或服务身份受保护。", nil)
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	inputHash := hex.EncodeToString(sum[:])
	clear(raw)
	// A single process sends directory mutations serially. Persisted intents prevent
	// replay across restart, actors, timeouts and lost responses; writes never retry.
	if !directoryWriteLock.TryLock() {
		return e.Error(429, "目录有写操作正在处理，请稍后查询。", nil)
	}
	defer directoryWriteLock.Unlock()
	if old, err := e.App.FindFirstRecordByData("operation_audits", "operation_key", in.OperationID); err == nil {
		if old.GetString("actor_id") != e.Auth.Id || old.GetString("input_hash") != inputHash {
			return e.Error(409, "操作编号已用于其他请求。", nil)
		}
		return response(e, directoryRecordView(old))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return directoryFailure(e, err)
	}
	if !isCreate {
		if strings.HasPrefix(in.Action, "user.") {
			u, err := p.User(e.Request.Context(), in.Target)
			if err != nil {
				return directoryFailure(e, err)
			}
			if u.Version != in.Version {
				return e.Error(409, "目录用户已变化，请刷新核对。", nil)
			}
			if slices.Contains(u.Groups, "admins") {
				return e.ForbiddenError("FreeIPA 管理员受保护。", nil)
			}
		} else {
			g, err := p.Group(e.Request.Context(), in.Target)
			if err != nil {
				return directoryFailure(e, err)
			}
			if g.Version != in.Version {
				return e.Error(409, "目录组已变化，请刷新核对。", nil)
			}
			if !freeipa.Name.MatchString(in.Member) {
				return e.BadRequestError("成员用户名不合法。", nil)
			}
			u, err := p.User(e.Request.Context(), in.Member)
			if err != nil {
				return directoryFailure(e, err)
			}
			if slices.Contains(u.Groups, "admins") {
				return e.ForbiddenError("FreeIPA 管理员受保护。", nil)
			}
			count, err := e.App.CountRecords("directory_role_mappings", dbx.HashExp{"group_id": g.ID})
			if err != nil {
				return directoryFailure(e, err)
			}
			if count > 0 {
				if err = platformFresh(e, e.App, "platform.roles.manage"); err != nil {
					return err
				}
			}
		}
	}
	var intent *core.Record
	err = e.App.RunInTransaction(func(tx core.App) error {
		if err := platformFresh(e, tx, directoryRead, directoryManage); err != nil {
			return err
		}
		pending, err := tx.CountRecords("operation_audits", dbx.And(dbx.HashExp{"target_type": "directory", "target_id": in.Target}, dbx.In("status", "processing", "unknown")))
		if err != nil {
			return err
		}
		if pending > 0 {
			return e.Error(409, "此目标仍有结果待核实的目录操作，请先通过原操作编号查询并由管理员核对。", nil)
		}
		c, err := tx.FindCollectionByNameOrId("operation_audits")
		if err != nil {
			return err
		}
		intent = core.NewRecord(c)
		if err = audit.Stamp(tx, intent, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), "", "directory", in.Target); err != nil {
			return err
		}
		intent.Set("operation_key", in.OperationID)
		intent.Set("action", "directory."+in.Action)
		intent.Set("status", "processing")
		intent.Set("reason", in.Reason)
		intent.Set("input_hash", inputHash)
		intent.Set("message", "目录操作已登记，尚未确认结果。")
		intent.Set("observed_states", []string{})
		changes := []audit.Change{}
		switch in.Action {
		case "group.add-member":
			changes = append(changes, audit.Change{Field: "username", After: in.Member})
		case "group.remove-member":
			changes = append(changes, audit.Change{Field: "username", Before: in.Member})
		case "user.create":
			changes = append(changes, audit.Change{Field: "username", After: in.Target}, audit.Change{Field: "userName", After: in.FirstName + " " + in.LastName})
		case "group.create":
			changes = append(changes, audit.Change{Field: "groupName", After: in.Target}, audit.Change{Field: "description", After: in.Description})
		case "user.enable":
			changes = append(changes, audit.Change{Field: "enabled", After: "true"})
		case "user.disable":
			changes = append(changes, audit.Change{Field: "enabled", After: "false"})
		}
		intent.Set("changes", changes)
		return tx.Save(intent)
	})
	if err != nil {
		return directoryFailure(e, err)
	}
	err = p.Change(e.Request.Context(), in.Change)
	status, message := "succeeded", "FreeIPA 已确认目录操作完成。"
	if err != nil {
		status, message = "unknown", "请求未获得明确结果。请保留操作编号，由目录管理员核对；平台不会自动重试。"
		if errors.Is(err, freeipa.ErrDenied) || errors.Is(err, freeipa.ErrRejected) || errors.Is(err, freeipa.ErrNotFound) {
			status, message = "failed", "FreeIPA 明确拒绝了该操作。"
		}
	}
	intent.Set("status", status)
	intent.Set("message", message)
	if e.App.Save(intent) != nil {
		return e.Error(503, "目录操作可能已发送，结果记录失败。请保留操作编号查询，勿重试新操作。", nil)
	}
	return response(e, directoryRecordView(intent))
}

type directoryMapping struct {
	ID        string `json:"id"`
	Group     string `json:"group"`
	RoleCode  string `json:"roleCode"`
	RoleName  string `json:"roleName"`
	ClusterID string `json:"clusterId"`
	UpdatedAt string `json:"updatedAt"`
}

func directoryMappingView(app core.App, r *core.Record) (directoryMapping, error) {
	role, err := app.FindRecordById("roles", r.GetString("role"))
	if err != nil {
		return directoryMapping{}, err
	}
	return directoryMapping{r.Id, r.GetString("group_name"), role.GetString("code"), role.GetString("name"), r.GetString("cluster"), r.GetString("updated")}, nil
}
func directoryMappings(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	rows, err := e.App.FindRecordsByFilter("directory_role_mappings", "", "group_name", 101, 0)
	if err != nil {
		return directoryFailure(e, err)
	}
	items := []directoryMapping{}
	for _, r := range rows {
		v, err := directoryMappingView(e.App, r)
		if err != nil {
			return directoryFailure(e, err)
		}
		items = append(items, v)
	}
	if err = platformFresh(e, e.App, directoryRead, "platform.roles.manage"); err != nil {
		return err
	}
	return response(e, map[string]any{"items": items})
}
func createDirectoryMapping(e *core.RequestEvent) error {
	var in struct {
		Group        string `json:"group"`
		RoleCode     string `json:"roleCode"`
		ClusterID    string `json:"clusterId"`
		Confirmation string `json:"confirmation"`
	}
	if err := platformBody(e, &in, "group", "roleCode", "clusterId", "confirmation"); err != nil {
		return err
	}
	if !freeipa.GroupName.MatchString(in.Group) || !platformText(in.RoleCode, 1, 64, false) || in.ClusterID != "" && !configVersionID.MatchString(in.ClusterID) || in.Confirmation != "CHANGE ACCESS" {
		return e.BadRequestError("核对组、角色与集群范围，输入 CHANGE ACCESS 确认。", nil)
	}
	if !directoryWriteLock.TryLock() {
		return e.Error(429, "目录授权正在变化，请稍后重试。", nil)
	}
	defer directoryWriteLock.Unlock()
	p, err := directoryProvider(e)
	if err != nil {
		return err
	}
	g, err := p.Group(e.Request.Context(), in.Group)
	if err != nil {
		return directoryFailure(e, err)
	}
	var saved directoryMapping
	err = audit.Mutate(e.App, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, directoryRead, "platform.roles.manage"); err != nil {
			return audit.Event{}, err
		}
		count, err := tx.CountRecords("directory_role_mappings")
		if err != nil {
			return audit.Event{}, err
		}
		if count >= 100 {
			return audit.Event{}, e.Error(409, "最多配置 100 条目录组映射。", nil)
		}
		role, err := tx.FindFirstRecordByData("roles", "code", in.RoleCode)
		if err != nil {
			return audit.Event{}, e.BadRequestError("角色不存在。", nil)
		}
		scope := role.GetString("scope")
		if scope == "cluster" && in.ClusterID == "" || scope == "platform" && in.ClusterID != "" {
			return audit.Event{}, e.BadRequestError("集群角色须指定集群，平台角色不能指定集群。", nil)
		}
		if in.ClusterID != "" {
			if _, err = tx.FindRecordById("clusters", in.ClusterID); err != nil {
				return audit.Event{}, e.BadRequestError("集群不存在。", nil)
			}
		}
		c, err := tx.FindCollectionByNameOrId("directory_role_mappings")
		if err != nil {
			return audit.Event{}, err
		}
		r := core.NewRecord(c)
		r.Set("group_name", in.Group)
		r.Set("group_id", g.ID)
		r.Set("role", role.Id)
		r.Set("cluster", in.ClusterID)
		if err = tx.Save(r); err != nil {
			return audit.Event{}, e.Error(409, "映射已存在或保存失败。", nil)
		}
		saved, err = directoryMappingView(tx, r)
		if err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "directory.mapping.create", ClusterID: in.ClusterID, TargetType: "directory_mapping", TargetID: r.Id, Message: "已绑定 FreeIPA 组的不可变身份；每次授权实时核对直接成员。", Changes: []audit.Change{{Field: "groupName", After: in.Group}, {Field: "roleCode", After: in.RoleCode}}}, nil
	})
	if err != nil {
		return directoryFailure(e, err)
	}
	return response(e, saved)
}
func deleteDirectoryMapping(e *core.RequestEvent) error {
	var in struct {
		UpdatedAt    string `json:"updatedAt"`
		Confirmation string `json:"confirmation"`
	}
	if err := platformBody(e, &in, "updatedAt", "confirmation"); err != nil {
		return err
	}
	if in.Confirmation != "CHANGE ACCESS" || !configVersionID.MatchString(e.Request.PathValue("mappingId")) {
		return e.BadRequestError("核对映射并输入 CHANGE ACCESS。", nil)
	}
	if !directoryWriteLock.TryLock() {
		return e.Error(429, "目录授权正在变化，请稍后重试。", nil)
	}
	defer directoryWriteLock.Unlock()
	err := audit.Mutate(e.App, audit.PlatformActor(e.Auth, e.Get("requestId").(string)), func(tx core.App) (audit.Event, error) {
		if err := platformFresh(e, tx, directoryRead, "platform.roles.manage"); err != nil {
			return audit.Event{}, err
		}
		r, err := tx.FindRecordById("directory_role_mappings", e.Request.PathValue("mappingId"))
		if err != nil {
			return audit.Event{}, e.NotFoundError("映射不存在。", nil)
		}
		if r.GetString("updated") != in.UpdatedAt {
			return audit.Event{}, e.Error(409, "映射已变化，请刷新。", nil)
		}
		v, err := directoryMappingView(tx, r)
		if err != nil {
			return audit.Event{}, err
		}
		if err = tx.Delete(r); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "directory.mapping.remove", ClusterID: v.ClusterID, TargetType: "directory_mapping", TargetID: r.Id, Message: "已移除目录组映射，手工授权不受影响。", Changes: []audit.Change{{Field: "groupName", Before: v.Group}, {Field: "roleCode", Before: v.RoleCode}}}, nil
	})
	if err != nil {
		return directoryFailure(e, err)
	}
	return response(e, map[string]bool{"removed": true})
}
