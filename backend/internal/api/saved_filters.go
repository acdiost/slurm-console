package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/savedfilters"
)

var filterConflict = errors.New("saved filter conflict")
var filterLimit = errors.New("saved filter limit")

type savedFilter struct {
	Compatible bool              `json:"compatible"`
	ID         string            `json:"id"`
	Module     string            `json:"module"`
	ClusterID  string            `json:"clusterId"`
	Name       string            `json:"name"`
	Conditions map[string]string `json:"conditions"`
	IsDefault  bool              `json:"isDefault"`
	Revision   int               `json:"revision"`
	Usable     bool              `json:"usable"`
	UpdatedAt  string            `json:"updatedAt"`
}
type filterInput struct {
	Module     string            `json:"module"`
	ClusterID  string            `json:"clusterId"`
	Name       string            `json:"name"`
	Conditions map[string]string `json:"conditions"`
	IsDefault  *bool             `json:"isDefault"`
}
type filterUpdate struct {
	Name       string            `json:"name"`
	Conditions map[string]string `json:"conditions"`
	IsDefault  *bool             `json:"isDefault"`
	Revision   int               `json:"revision"`
}

func filterScope(e *core.RequestEvent, module, cluster string) (rbac.Access, error) {
	d, ok := savedfilters.Spec(module)
	if !ok || d.Scoped != (cluster != "") || len(cluster) > 64 {
		return rbac.Access{}, e.BadRequestError("筛选模块或集群范围不合法。", nil)
	}
	a, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return a, err
	}
	if !savedfilters.CanEnter(a, module, cluster) {
		return a, e.ForbiddenError("没有该模块或集群的读取权限。", nil)
	}
	if cluster != "" {
		if _, err := e.App.FindRecordById("clusters", cluster); err != nil {
			return a, e.NotFoundError("集群不存在。", nil)
		}
	}
	return a, nil
}
func filterView(r *core.Record, a rbac.Access) (savedFilter, error) {
	var conditions map[string]string
	b, _ := json.Marshal(r.GetRaw("conditions"))
	err := json.Unmarshal(b, &conditions)
	clean, validation := savedfilters.Conditions(r.GetString("module"), conditions)
	compatible := err == nil && validation == nil
	if !compatible {
		clean = map[string]string{}
	}
	return savedFilter{ID: r.Id, Module: r.GetString("module"), ClusterID: r.GetString("cluster"), Name: r.GetString("name"), Conditions: clean, IsDefault: r.GetBool("is_default"), Revision: r.GetInt("revision"), Compatible: compatible, Usable: compatible && savedfilters.CanUse(a, r.GetString("module"), r.GetString("cluster"), clean), UpdatedAt: r.GetString("updated")}, nil
}

func validFilterName(name string) bool {
	return utf8.RuneCountInString(name) >= 1 && utf8.RuneCountInString(name) <= 40 && !strings.ContainsFunc(name, unicode.IsControl)
}
func ownFilter(app core.App, actor, id string) (*core.Record, error) {
	return app.FindFirstRecordByFilter("saved_filters", "id={:id} && owner={:owner}", dbx.Params{"id": id, "owner": actor})
}
func filterWriteError(e *core.RequestEvent, err error) error {
	if errors.Is(err, filterConflict) {
		return e.Error(409, "方案已被其他请求修改或名称已存在，请刷新方案列表后重试。", nil)
	}
	if errors.Is(err, filterLimit) {
		return e.Error(409, "当前范围最多保存 10 个方案，每个用户最多 100 个；请先删除不再使用的方案。", nil)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return e.NotFoundError("筛选方案不存在。", nil)
	}
	return e.InternalServerError("无法保存筛选方案。", err)
}
func storeFilter(tx core.App, actor, module, cluster, id, name string, conditions map[string]string, def bool, revision int) (*core.Record, error) {
	var r *core.Record
	var err error
	if id != "" {
		r, err = ownFilter(tx, actor, id)
		if err != nil {
			return nil, err
		}
		if r.GetInt("revision") != revision {
			return nil, filterConflict
		}
	} else {
		n, e := tx.CountRecords("saved_filters", dbx.HashExp{"owner": actor})
		if e != nil {
			return nil, e
		}
		if n >= 100 {
			return nil, filterLimit
		}
		n, e = tx.CountRecords("saved_filters", dbx.HashExp{"owner": actor, "module": module, "cluster": cluster})
		if e != nil {
			return nil, e
		}
		if n >= 10 {
			return nil, filterLimit
		}
		c, e := tx.FindCollectionByNameOrId("saved_filters")
		if e != nil {
			return nil, e
		}
		r = core.NewRecord(c)
	}
	duplicates, e := tx.CountRecords("saved_filters", dbx.And(dbx.HashExp{"owner": actor, "module": module, "cluster": cluster, "name_key": strings.ToLower(name)}, dbx.NewExp("id != {:id}", dbx.Params{"id": id})))
	if e != nil {
		return nil, e
	}
	if duplicates > 0 {
		return nil, filterConflict
	}
	if def {
		old, e := tx.FindRecordsByFilter("saved_filters", "owner={:owner} && module={:module} && cluster={:cluster} && is_default=true && id!={:id}", "", 0, 0, dbx.Params{"owner": actor, "module": module, "cluster": cluster, "id": id})
		if e != nil {
			return nil, e
		}
		for _, previous := range old {
			previous.Set("is_default", false)
			previous.Set("revision", previous.GetInt("revision")+1)
			if e := tx.Save(previous); e != nil {
				return nil, e
			}
		}
	}
	for k, v := range map[string]any{"owner": actor, "module": module, "cluster": cluster, "name": name, "name_key": strings.ToLower(name), "conditions": conditions, "is_default": def, "revision": revision + 1} {
		r.Set(k, v)
	}
	if err := tx.Save(r); err != nil {
		return nil, err
	}
	return r, nil
}
func registerSavedFilterRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/saved-filters")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	r.GET("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "module", "clusterId"); err != nil {
			return err
		}
		q := e.Request.URL.Query()
		a, err := filterScope(e, q.Get("module"), q.Get("clusterId"))
		if err != nil {
			return err
		}
		rows, err := e.App.FindRecordsByFilter("saved_filters", "owner={:owner} && module={:module} && cluster={:cluster}", "name", 11, 0, dbx.Params{"owner": e.Auth.Id, "module": q.Get("module"), "cluster": q.Get("clusterId")})
		if err != nil {
			return e.InternalServerError("", err)
		}
		if len(rows) > 10 {
			return e.Error(409, "存储的筛选方案超出范围限制，请联系管理员核对。", nil)
		}
		items := []savedFilter{}
		for _, row := range rows {
			v, err := filterView(row, a)
			if err != nil {
				return e.Error(409, "存储的筛选方案不兼容，请先更新或删除。", nil)
			}
			items = append(items, v)
		}
		return response(e, map[string]any{"items": items, "maxPerScope": 10, "maxPerUser": 100})
	})
	r.POST("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in filterInput
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		clean, err := savedfilters.Conditions(in.Module, in.Conditions)
		if err != nil || in.IsDefault == nil || !validFilterName(in.Name) {
			return e.BadRequestError("方案名称、筛选字段或时间范围不合法；只允许保存本模块的列表条件。", nil)
		}
		a, err := filterScope(e, in.Module, in.ClusterID)
		if err != nil {
			return err
		}
		if !savedfilters.CanUse(a, in.Module, in.ClusterID, clean) {
			return e.ForbiddenError("当前没有该方案对应列表的读取权限。", nil)
		}
		var row *core.Record
		err = e.App.RunInTransaction(func(tx core.App) error {
			var err error
			row, err = storeFilter(tx, e.Auth.Id, in.Module, in.ClusterID, "", in.Name, clean, *in.IsDefault, 0)
			return err
		})
		if err != nil {
			return filterWriteError(e, err)
		}
		v, err := filterView(row, a)
		if err != nil {
			return e.InternalServerError("", err)
		}
		return response(e, v)
	})
	r.PATCH("/{filterId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in filterUpdate
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		row, err := ownFilter(e.App, e.Auth.Id, e.Request.PathValue("filterId"))
		if err != nil {
			return filterWriteError(e, err)
		}
		module, cluster := row.GetString("module"), row.GetString("cluster")
		clean, err := savedfilters.Conditions(module, in.Conditions)
		if err != nil || in.Revision < 1 || in.IsDefault == nil || !validFilterName(in.Name) {
			return e.BadRequestError("方案名称、版本或筛选条件不合法。", nil)
		}
		a, err := filterScope(e, module, cluster)
		if err != nil {
			return err
		}
		if !savedfilters.CanUse(a, module, cluster, clean) {
			return e.ForbiddenError("当前没有该方案对应列表的读取权限。", nil)
		}
		err = e.App.RunInTransaction(func(tx core.App) error {
			var err error
			row, err = storeFilter(tx, e.Auth.Id, module, cluster, row.Id, in.Name, clean, *in.IsDefault, in.Revision)
			return err
		})
		if err != nil {
			return filterWriteError(e, err)
		}
		v, err := filterView(row, a)
		if err != nil {
			return e.InternalServerError("", err)
		}
		return response(e, v)
	})
	r.DELETE("/{filterId}", func(e *core.RequestEvent) error {
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
			return e.BadRequestError("请提供方案版本。", nil)
		}
		err := e.App.RunInTransaction(func(tx core.App) error {
			row, err := ownFilter(tx, e.Auth.Id, e.Request.PathValue("filterId"))
			if err != nil {
				return err
			}
			if row.GetInt("revision") != in.Revision {
				return filterConflict
			}
			return tx.Delete(row)
		})
		if err != nil {
			return filterWriteError(e, err)
		}
		return response(e, map[string]bool{"deleted": true})
	})
}
