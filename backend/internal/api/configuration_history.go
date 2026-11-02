package api

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/confighistory"
	"slurm-console/backend/internal/rbac"
)

var configVersionID = regexp.MustCompile(`^[a-z0-9]{15}$`)

func configurationFresh(e *core.RequestEvent, app core.App, history bool) error {
	a, err := rbac.Resolve(app, e.Auth.Id)
	if err != nil {
		return err
	}
	if !a.Allows("platform.clusters.manage", "") || history && !a.Allows("platform.audit.read", "") {
		return e.ForbiddenError("集群配置管理或历史审计权限不可用。", nil)
	}
	return nil
}
func registerConfigurationHistoryRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	r := group.Group("/cluster-configurations")
	r.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("platform.clusters.manage", false))
	preview := func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var input clusterconfig.Input
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		view, err := clusterconfig.For(e.App).Preview(e.Request.PathValue("clusterId"), input)
		if err != nil {
			return configurationError(e, err)
		}
		if err := configurationFresh(e, e.App, false); err != nil {
			return err
		}
		return response(e, view)
	}
	r.POST("/preview", preview)
	r.POST("/{clusterId}/preview", preview)
	r.POST("/{clusterId}/disable-preview", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var input struct {
			Revision int `json:"revision"`
		}
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		view, err := clusterconfig.For(e.App).PreviewDisable(e.Request.PathValue("clusterId"), input.Revision)
		if err != nil {
			return configurationError(e, err)
		}
		if err := configurationFresh(e, e.App, false); err != nil {
			return err
		}
		return response(e, view)
	})
	r.GET("/{clusterId}/versions", configurationVersions).BindFunc(rbac.Require("platform.audit.read", false))
	r.GET("/{clusterId}/compare", compareConfigurationVersions).BindFunc(rbac.Require("platform.audit.read", false))
}
func configHistoryError(e *core.RequestEvent, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return e.Error(408, "版本读取超时或已取消。", nil)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return e.NotFoundError("此集群的版本记录不存在。", nil)
	}
	if errors.Is(err, confighistory.ErrInvalid) {
		return e.Error(503, "版本格式不可用。", nil)
	}
	return e.InternalServerError("版本读取未完成。", err)
}
func configurationVersions(e *core.RequestEvent) error {
	if err := slurmQuery(e, "page", "perPage"); err != nil {
		return err
	}
	cid := e.Request.PathValue("clusterId")
	if !configVersionID.MatchString(cid) {
		return e.BadRequestError("集群标识不合法。", nil)
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()
	items := []confighistory.Version{}
	var total int64
	state := "no_history"
	var currentRevision *int
	err = e.App.RunInTransaction(func(tx core.App) error {
		filter := dbx.HashExp{"cluster_id": cid}
		if err := tx.DB().Select("COUNT(*)").From("cluster_config_versions").Where(filter).WithContext(ctx).Row(&total); err != nil {
			return err
		}
		var rows []*core.Record
		if err := tx.RecordQuery("cluster_config_versions").AndWhere(filter).OrderBy("revision DESC").Limit(int64(size)).Offset(int64((page - 1) * size)).WithContext(ctx).All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			items = append(items, confighistory.Metadata(row))
		}
		current, err := tx.FindRecordById("clusters", cid)
		if errors.Is(err, sql.ErrNoRows) {
			state = "missing"
			if total == 0 {
				return sql.ErrNoRows
			}
			return nil
		}
		if err != nil {
			return err
		}
		rev := current.GetInt("revision")
		currentRevision = &rev
		if total == 0 {
			return nil
		}
		var heads []*core.Record
		if err := tx.RecordQuery("cluster_config_versions").AndWhere(filter).OrderBy("revision DESC").Limit(1).WithContext(ctx).All(&heads); err != nil {
			return err
		}
		snap, err := confighistory.Decode(heads[0])
		if err != nil {
			return err
		}
		state = "different"
		if heads[0].GetInt("revision") == rev && snap == confighistory.FromRecord(current) {
			state = "matched"
		}
		return nil
	})
	if err != nil {
		return configHistoryError(e, err)
	}
	if err := configurationFresh(e, e.App, true); err != nil {
		return err
	}
	return response(e, map[string]any{"items": items, "page": page, "perPage": size, "totalItems": total, "clusterId": cid, "currentStatus": state, "currentRevision": currentRevision, "fetchedAt": time.Now().UTC().Format(time.RFC3339Nano)})
}
func compareConfigurationVersions(e *core.RequestEvent) error {
	if err := slurmQuery(e, "from", "to"); err != nil {
		return err
	}
	cid := e.Request.PathValue("clusterId")
	from, to := e.Request.URL.Query().Get("from"), e.Request.URL.Query().Get("to")
	if !configVersionID.MatchString(cid) || !configVersionID.MatchString(from) || !configVersionID.MatchString(to) {
		return e.BadRequestError("请选择此集群的两个明确版本标识。", nil)
	}
	ctx, cancel := context.WithTimeout(e.Request.Context(), 5*time.Second)
	defer cancel()
	var before, after confighistory.Snapshot
	var a, b confighistory.Version
	err := e.App.RunInTransaction(func(tx core.App) error {
		read := func(id string) (*core.Record, error) {
			var rows []*core.Record
			err := tx.RecordQuery("cluster_config_versions").AndWhere(dbx.HashExp{"id": id, "cluster_id": cid}).Limit(1).WithContext(ctx).All(&rows)
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return nil, sql.ErrNoRows
			}
			return rows[0], nil
		}
		x, err := read(from)
		if err != nil {
			return err
		}
		y, err := read(to)
		if err != nil {
			return err
		}
		before, err = confighistory.Decode(x)
		if err != nil {
			return err
		}
		after, err = confighistory.Decode(y)
		if err != nil {
			return err
		}
		a = confighistory.Metadata(x)
		b = confighistory.Metadata(y)
		return nil
	})
	if err != nil {
		return configHistoryError(e, err)
	}
	if err := configurationFresh(e, e.App, true); err != nil {
		return err
	}
	return response(e, map[string]any{"clusterId": cid, "from": a, "to": b, "before": before, "after": after, "fields": confighistory.Diff(&before, after), "fetchedAt": time.Now().UTC().Format(time.RFC3339Nano)})
}
