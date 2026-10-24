package api

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
)

func registerClusterRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	clusters := group.Group("/clusters")
	clusters.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	clusters.GET("", func(e *core.RequestEvent) error {
		page, size, err := pagination(e)
		if err != nil {
			return err
		}
		access, err := rbac.Resolve(e.App, e.Auth.Id)
		if err != nil {
			return e.InternalServerError("", err)
		}
		var filter dbx.Expression
		if !access.Allows("clusters.read", "") {
			ids := []any{}
			for _, entry := range access.Clusters {
				if access.Allows("clusters.read", entry.Cluster.ID) {
					ids = append(ids, entry.Cluster.ID)
				}
			}
			if len(ids) == 0 {
				return response(e, map[string]any{"items": []rbac.Cluster{}, "page": page, "perPage": size, "totalItems": 0})
			}
			filter = dbx.In("id", ids...)
		}
		total, err := e.App.CountRecords("clusters", filter)
		if err != nil {
			return e.InternalServerError("", err)
		}
		var records []*core.Record
		query := e.App.RecordQuery("clusters").OrderBy("name", "id").Limit(int64(size)).Offset(int64((page - 1) * size))
		if filter != nil {
			query.AndWhere(filter)
		}
		if err := query.All(&records); err != nil {
			return e.InternalServerError("", err)
		}
		items := make([]rbac.Cluster, 0, len(records))
		for _, record := range records {
			items = append(items, rbac.ClusterDTO(record))
		}
		return response(e, map[string]any{"items": items, "page": page, "perPage": size, "totalItems": total})
	})
	clusters.GET("/{clusterId}", func(e *core.RequestEvent) error {
		record, err := e.App.FindRecordById("clusters", e.Request.PathValue("clusterId"))
		if errors.Is(err, sql.ErrNoRows) {
			return e.NotFoundError("集群不存在。", nil)
		}
		if err != nil {
			return e.InternalServerError("", err)
		}
		return response(e, rbac.ClusterDTO(record))
	}).BindFunc(rbac.Require("clusters.read", true))
}

func pagination(e *core.RequestEvent) (int, int, error) {
	page, size := 1, 20
	for _, field := range []struct {
		name   string
		target *int
		max    int
	}{{"page", &page, 1000000}, {"perPage", &size, 100}} {
		if raw := e.Request.URL.Query().Get(field.name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > field.max {
				return 0, 0, e.BadRequestError("分页参数不合法。", nil)
			}
			*field.target = value
		}
	}
	return page, size, nil
}
