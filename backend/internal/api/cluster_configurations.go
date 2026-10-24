package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/rbac"
)

func registerConfigurationRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	managed := group.Group("")
	managed.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("platform.clusters.manage", false))
	managed.GET("/cluster-connection-profiles", func(e *core.RequestEvent) error { return response(e, clusterconfig.For(e.App).Registry.Views()) })
	routes := managed.Group("/cluster-configurations")
	routes.GET("", func(e *core.RequestEvent) error {
		page, size, err := pagination(e)
		if err != nil {
			return err
		}
		total, err := e.App.CountRecords("clusters")
		if err != nil {
			return e.InternalServerError("", err)
		}
		var records []*core.Record
		if err := e.App.RecordQuery("clusters").OrderBy("name", "id").Limit(int64(size)).Offset(int64((page - 1) * size)).All(&records); err != nil {
			return e.InternalServerError("", err)
		}
		manager := clusterconfig.For(e.App)
		items := []clusterconfig.Configuration{}
		for _, record := range records {
			items = append(items, manager.View(record))
		}
		return response(e, map[string]any{"items": items, "page": page, "perPage": size, "totalItems": total})
	})
	routes.POST("", func(e *core.RequestEvent) error {
		var input clusterconfig.Input
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		view, err := clusterconfig.For(e.App).Save("", input)
		if err != nil {
			return configurationError(e, err)
		}
		configurationLog(e, "create", view)
		return response(e, view)
	})
	routes.PATCH("/{clusterId}", func(e *core.RequestEvent) error {
		var input clusterconfig.Input
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		view, err := clusterconfig.For(e.App).Save(e.Request.PathValue("clusterId"), input)
		if err != nil {
			return configurationError(e, err)
		}
		configurationLog(e, "update", view)
		return response(e, view)
	})
	routes.POST("/{clusterId}/test", func(e *core.RequestEvent) error {
		var input struct {
			Revision int `json:"revision"`
		}
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		view, err := clusterconfig.For(e.App).Test(e.Request.Context(), e.Request.PathValue("clusterId"), input.Revision)
		if err != nil {
			return configurationError(e, err)
		}
		configurationLog(e, "test", view)
		return response(e, view)
	})
	routes.POST("/{clusterId}/disable", func(e *core.RequestEvent) error {
		var input struct {
			Revision int `json:"revision"`
		}
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		view, err := clusterconfig.For(e.App).Disable(e.Request.PathValue("clusterId"), input.Revision)
		if err != nil {
			return configurationError(e, err)
		}
		configurationLog(e, "disable", view)
		return response(e, view)
	})
}

func readConfigurationBody(e *core.RequestEvent, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(e.Response, e.Request.Body, 16384))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return e.BadRequestError("配置格式不正确或包含不支持的字段。", nil)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return e.BadRequestError("请求只允许包含一份 JSON 配置。", nil)
	}
	return nil
}
func configurationError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, clusterconfig.ErrInvalid):
		return e.BadRequestError("请检查集群名称、标识、连接方式、API 版本和超时设置；集群标识创建后不可修改。", nil)
	case errors.Is(err, clusterconfig.ErrConflict):
		return e.Error(409, "配置已变更、标识已被使用或连接已停用，请刷新后重试。", nil)
	case errors.Is(err, clusterconfig.ErrBusy):
		return e.Error(409, "连接检测正在进行，请稍后重试。", nil)
	case errors.Is(err, clusterconfig.ErrNotFound):
		return e.NotFoundError("集群配置不存在。", nil)
	default:
		return e.InternalServerError("", err)
	}
}
func configurationLog(e *core.RequestEvent, action string, view clusterconfig.Configuration) {
	slog.InfoContext(e.Request.Context(), "cluster configuration operation", "requestId", e.Get("requestId"), "actorId", e.Auth.Id, "action", action, "clusterId", view.ID, "revision", view.Revision, "status", view.LastProbe.Status)
}
