package api

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/bulkcontrol"
	"slurm-console/backend/internal/rbac"
)

var templateConflict = errors.New("template revision/name conflict")
var templateQuota = errors.New("template quota exceeded")

type templateInput struct {
	Name          string   `json:"name"`
	Action        string   `json:"action"`
	Description   string   `json:"description"`
	DefaultReason string   `json:"defaultReason"`
	Checklist     []string `json:"checklist"`
	Revision      int      `json:"revision"`
}
type operationTemplate struct {
	ID            string   `json:"id"`
	ClusterID     string   `json:"clusterId"`
	Name          string   `json:"name"`
	Action        string   `json:"action"`
	Description   string   `json:"description"`
	DefaultReason string   `json:"defaultReason"`
	Checklist     []string `json:"checklist"`
	Revision      int      `json:"revision"`
	UpdatedAt     string   `json:"updatedAt"`
}

func templateCan(a rbac.Access, cid, action string) bool {
	return bulkcontrol.ValidAction(action) && a.Allows(action, cid) && a.Allows(strings.Split(action, ".")[0]+".read", cid)
}
func templateScope(e *core.RequestEvent, app core.App) (rbac.Access, error) {
	a, err := rbac.Resolve(app, e.Auth.Id)
	if err != nil {
		return a, err
	}
	cid := e.Request.PathValue("clusterId")
	if !templateCan(a, cid, "jobs.cancel") && !templateCan(a, cid, "nodes.drain") && !templateCan(a, cid, "nodes.resume") {
		return a, e.ForbiddenError("需要此集群至少一项支持的操作权限和对应资源读取权限。", nil)
	}
	if _, err := app.FindRecordById("clusters", cid); err != nil {
		return a, e.NotFoundError("集群不存在。", nil)
	}
	return a, nil
}
func templateText(s string, min, max int, multiline bool) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) >= min && utf8.RuneCountInString(s) <= max && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && !(multiline && r == '\n') })
}
func validTemplate(in templateInput) bool {
	if !templateText(in.Name, 1, 40, false) || !bulkcontrol.ValidAction(in.Action) || !templateText(in.Description, 0, 500, true) || !templateText(in.DefaultReason, 0, 220, false) || in.DefaultReason != "" && !templateText(in.DefaultReason, 2, 220, false) || in.Checklist == nil || len(in.Checklist) > 8 {
		return false
	}
	for _, c := range in.Checklist {
		if c != strings.TrimSpace(c) || !templateText(c, 1, 160, false) {
			return false
		}
	}
	return true
}
func templateView(r *core.Record) (operationTemplate, error) {
	checks := []string{}
	if e := r.UnmarshalJSONField("checklist", &checks); e != nil {
		return operationTemplate{}, e
	}
	in := templateInput{Name: r.GetString("name"), Action: r.GetString("action"), Description: r.GetString("description"), DefaultReason: r.GetString("default_reason"), Checklist: checks, Revision: r.GetInt("revision")}
	if !validTemplate(in) || in.Revision < 1 {
		return operationTemplate{}, templateConflict
	}
	return operationTemplate{r.Id, r.GetString("cluster"), in.Name, in.Action, in.Description, in.DefaultReason, checks, in.Revision, r.GetString("updated")}, nil
}
func ownTemplate(e *core.RequestEvent, app core.App) (*core.Record, error) {
	return app.FindFirstRecordByFilter("operation_templates", "id={:id} && owner={:owner} && cluster={:cluster}", dbx.Params{"id": e.Request.PathValue("templateId"), "owner": e.Auth.Id, "cluster": e.Request.PathValue("clusterId")})
}
func templateError(e *core.RequestEvent, err error) error {
	if errors.Is(err, templateConflict) {
		return e.Error(409, "模板版本已变化、内容不兼容或名称已存在，请刷新后重试。", nil)
	}
	if errors.Is(err, templateQuota) {
		return e.Error(409, "每个集群最多 20 个本人模板，每人最多 100 个；请先清理不用的模板。", nil)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return e.NotFoundError("模板不存在。", nil)
	}
	// Preserve intentional authorization errors from transaction-time rechecks.
	var apiErr *router.ApiError
	if errors.As(err, &apiErr) {
		return err
	}
	return e.InternalServerError("模板处理未完成。", err)
}
func registerOperationTemplateRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/clusters/{clusterId}/operation-templates")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	r.GET("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		a, err := templateScope(e, e.App)
		if err != nil {
			return err
		}
		rows, err := e.App.FindRecordsByFilter("operation_templates", "owner={:owner} && cluster={:cluster}", "name", 21, 0, dbx.Params{"owner": e.Auth.Id, "cluster": e.Request.PathValue("clusterId")})
		if err != nil {
			return templateError(e, err)
		}
		if len(rows) > 20 {
			return templateError(e, templateQuota)
		}
		a, err = templateScope(e, e.App)
		if err != nil {
			return err
		}
		items := []operationTemplate{}
		for _, row := range rows {
			if !templateCan(a, e.Request.PathValue("clusterId"), row.GetString("action")) {
				continue
			}
			v, err := templateView(row)
			if err != nil {
				return templateError(e, err)
			}
			items = append(items, v)
		}
		return response(e, map[string]any{"items": items, "maxPerCluster": 20, "maxPerUser": 100})
	})
	save := func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in templateInput
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		in.DefaultReason = strings.TrimSpace(in.DefaultReason)
		update := e.Request.PathValue("templateId") != ""
		if !validTemplate(in) || update && in.Revision < 1 || !update && in.Revision != 0 {
			return e.BadRequestError("模板名称、动作、单行原因、清单或版本不合法。", nil)
		}
		var row *core.Record
		requestID, _ := e.Get("requestId").(string)
		err := audit.Mutate(e.App, audit.PlatformActor(e.Auth, requestID), func(tx core.App) (audit.Event, error) {
			a, err := templateScope(e, tx)
			if err != nil {
				return audit.Event{}, err
			}
			cid := e.Request.PathValue("clusterId")
			if !templateCan(a, cid, in.Action) {
				return audit.Event{}, e.ForbiddenError("没有此模板动作的读取及操作权限。", nil)
			}
			beforeName, beforeAction, beforeRevision := "", "", "0"
			if update {
				row, err = ownTemplate(e, tx)
				if err != nil {
					return audit.Event{}, err
				}
				if !templateCan(a, cid, row.GetString("action")) {
					return audit.Event{}, e.ForbiddenError("原模板动作权限不可用。", nil)
				}
				if row.GetInt("revision") != in.Revision {
					return audit.Event{}, templateConflict
				}
				beforeName, beforeAction, beforeRevision = row.GetString("name"), row.GetString("action"), strconv.Itoa(row.GetInt("revision"))
			} else {
				count, err := tx.CountRecords("operation_templates", dbx.HashExp{"owner": e.Auth.Id})
				if err != nil {
					return audit.Event{}, err
				}
				local, err := tx.CountRecords("operation_templates", dbx.HashExp{"owner": e.Auth.Id, "cluster": cid})
				if err != nil {
					return audit.Event{}, err
				}
				if count >= 100 || local >= 20 {
					return audit.Event{}, templateQuota
				}
				c, err := tx.FindCollectionByNameOrId("operation_templates")
				if err != nil {
					return audit.Event{}, err
				}
				row = core.NewRecord(c)
			}
			count, err := tx.CountRecords("operation_templates", dbx.And(dbx.HashExp{"owner": e.Auth.Id, "cluster": cid, "name_key": strings.ToLower(in.Name)}, dbx.NewExp("id!={:id}", dbx.Params{"id": row.Id})))
			if err != nil {
				return audit.Event{}, err
			}
			if count > 0 {
				return audit.Event{}, templateConflict
			}
			for k, v := range map[string]any{"owner": e.Auth.Id, "cluster": cid, "name": in.Name, "name_key": strings.ToLower(in.Name), "action": in.Action, "description": in.Description, "default_reason": in.DefaultReason, "checklist": in.Checklist, "revision": in.Revision + 1} {
				row.Set(k, v)
			}
			if err := tx.Save(row); err != nil {
				return audit.Event{}, err
			}
			action := "operation_templates.create"
			if update {
				action = "operation_templates.update"
			}
			return audit.Event{Action: action, ClusterID: cid, TargetType: "operation_template", TargetID: row.Id, Message: "保存本人运维操作模板，未执行 Slurm 控制。", Changes: []audit.Change{{Field: "name", Before: beforeName, After: in.Name}, {Field: "templateAction", Before: beforeAction, After: in.Action}, {Field: "revision", Before: beforeRevision, After: strconv.Itoa(in.Revision + 1)}}}, nil
		})
		if err != nil {
			return templateError(e, err)
		}
		v, err := templateView(row)
		if err != nil {
			return templateError(e, err)
		}
		return response(e, v)
	}
	r.POST("", save)
	r.PATCH("/{templateId}", save)
	r.DELETE("/{templateId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in struct {
			Revision int `json:"revision"`
		}
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		if in.Revision < 1 {
			return e.BadRequestError("请提供模板版本。", nil)
		}
		requestID, _ := e.Get("requestId").(string)
		err := audit.Mutate(e.App, audit.PlatformActor(e.Auth, requestID), func(tx core.App) (audit.Event, error) {
			a, err := templateScope(e, tx)
			if err != nil {
				return audit.Event{}, err
			}
			row, err := ownTemplate(e, tx)
			if err != nil {
				return audit.Event{}, err
			}
			if !templateCan(a, e.Request.PathValue("clusterId"), row.GetString("action")) {
				return audit.Event{}, e.ForbiddenError("模板动作权限不可用。", nil)
			}
			if row.GetInt("revision") != in.Revision {
				return audit.Event{}, templateConflict
			}
			if err := tx.Delete(row); err != nil {
				return audit.Event{}, err
			}
			return audit.Event{Action: "operation_templates.delete", ClusterID: e.Request.PathValue("clusterId"), TargetType: "operation_template", TargetID: row.Id, Message: "删除本人运维模板，未执行 Slurm 控制。", Changes: []audit.Change{{Field: "name", Before: row.GetString("name"), After: ""}, {Field: "revision", Before: strconv.Itoa(in.Revision), After: ""}}}, nil
		})
		if err != nil {
			return templateError(e, err)
		}
		return response(e, map[string]bool{"deleted": true})
	})
	r.POST("/{templateId}/prepare", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in struct {
			Revision int      `json:"revision"`
			Targets  []string `json:"targets"`
		}
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		a, err := templateScope(e, e.App)
		if err != nil {
			return err
		}
		row, err := ownTemplate(e, e.App)
		if err != nil {
			return templateError(e, err)
		}
		v, err := templateView(row)
		if err != nil {
			return templateError(e, err)
		}
		if !templateCan(a, v.ClusterID, v.Action) {
			return e.ForbiddenError("没有此模板动作权限。", nil)
		}
		if in.Revision < 1 || v.Revision != in.Revision {
			return templateError(e, templateConflict)
		}
		if !bulkcontrol.ValidTargets(v.Action, in.Targets) {
			return e.BadRequestError("请提供 1–10 个不重复的明确作业 ID 或单个节点名；不接受表达式。", nil)
		}
		prepared, err := bulkcontrol.For(e.App).Prepare(e.Request.Context(), e.Auth.Id, v.ClusterID, v.Action, in.Targets)
		if err != nil {
			return slurmFailure(e, err)
		}
		a, err = templateScope(e, e.App)
		if err != nil {
			return err
		}
		fresh, err := ownTemplate(e, e.App)
		if err != nil {
			return templateError(e, err)
		}
		if fresh.GetInt("revision") != v.Revision {
			return templateError(e, templateConflict)
		}
		if !templateCan(a, v.ClusterID, v.Action) {
			return e.ForbiddenError("模板动作权限已撤销。", nil)
		}
		return response(e, map[string]any{"template": v, "prepared": prepared})
	})
}
