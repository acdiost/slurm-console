package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"net/http"
	"slices"
	"slurm-console/backend/internal/auth"
	notices "slurm-console/backend/internal/notifications"
	"slurm-console/backend/internal/rbac"
	"strings"
	"time"
)

func notificationError(e *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, notices.ErrForbidden):
		return e.ForbiddenError("没有该集群对应来源的读取权限。", nil)
	case errors.Is(err, sql.ErrNoRows):
		return e.NotFoundError("规则或通知不存在。", nil)
	case errors.Is(err, notices.ErrConflict):
		return e.Error(409, "规则已变化，请刷新后重试。", nil)
	case errors.Is(err, notices.ErrLimit):
		return e.Error(409, "每人最多 20 条规则，平台最多 100 条；请删除不再使用的规则。", nil)
	case errors.Is(err, notices.ErrBusy):
		return e.Error(409, "告警检查或通知连接繁忙，请稍后重试。", nil)
	case errors.Is(err, notices.ErrInvalid):
		return e.BadRequestError("规则名称、来源、阈值、持续时间或启用状态不合法。", nil)
	}
	return e.InternalServerError("告警通知暂不可用。", err)
}
func notificationVisibility(owner string, a rbac.Access) dbx.Expression {
	scopes := []dbx.Expression{dbx.NewExp("1=0")}
	for _, kind := range notices.Kinds {
		p := notices.Permission(kind)
		if a.Allows(p, "") {
			scopes = append(scopes, dbx.HashExp{"kind": kind})
			continue
		}
		ids := []any{}
		for _, c := range a.Clusters {
			if a.Allows(p, c.Cluster.ID) {
				ids = append(ids, c.Cluster.ID)
			}
		}
		if len(ids) > 0 {
			scopes = append(scopes, dbx.And(dbx.HashExp{"kind": kind}, dbx.In("cluster", ids...)))
		}
	}
	return dbx.And(dbx.HashExp{"owner": owner}, dbx.Or(scopes...), dbx.NewExp("created >= {:cutoff}", dbx.Params{"cutoff": time.Now().UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05.000Z")}))
}
func notificationView(r *core.Record) map[string]any {
	sample := r.GetStringSlice("samples")
	if sample == nil {
		sample = []string{}
	}
	path := map[string]string{"cluster_health": "overview", "node_unavailable": "nodes", "job_pending": "jobs", "gpu_temperature": "gpu"}[r.GetString("kind")]
	href := "/" + path + "?cluster=" + r.GetString("cluster") + "&filterMode=custom"
	if path == "jobs" {
		href += "&state=PENDING"
	}
	return map[string]any{"id": r.Id, "clusterId": r.GetString("cluster"), "ruleId": r.GetString("rule_id"), "ruleName": r.GetString("rule_name"), "kind": r.GetString("kind"), "severity": r.GetString("severity"), "phase": r.GetString("phase"), "incidentId": r.GetString("incident_id"), "message": r.GetString("message"), "affectedCount": r.GetInt("affected_count"), "samples": sample, "read": r.GetBool("read"), "createdAt": r.GetString("created"), "href": href}
}
func registerNotificationRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	rules := group.Group("/notification-rules")
	rules.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	rules.GET("", func(e *core.RequestEvent) error {
		if err := slurmQuery(e, "clusterId"); err != nil {
			return err
		}
		a, err := rbac.Resolve(e.App, e.Auth.Id)
		if err != nil {
			return err
		}
		filter := "owner={:owner}"
		params := dbx.Params{"owner": e.Auth.Id}
		if cid := e.Request.URL.Query().Get("clusterId"); cid != "" {
			filter += " && cluster={:cluster}"
			params["cluster"] = cid
		}
		rows, err := e.App.FindRecordsByFilter("notification_rules", filter, "created,id", notices.MaxPerUser+1, 0, params)
		if err != nil {
			return err
		}
		items := []notices.Rule{}
		for _, r := range rows {
			items = append(items, notices.RuleView(r, a))
		}
		return response(e, map[string]any{"items": items, "maxPerUser": notices.MaxPerUser, "maxRules": notices.MaxRules, "intervalSeconds": 30})
	})
	for _, method := range []string{"POST", "PATCH"} {
		path := ""
		if method == "PATCH" {
			path = "/{ruleId}"
		}
		rules.Route(method, path, func(e *core.RequestEvent) error {
			if err := slurmQuery(e); err != nil {
				return err
			}
			var in notices.Input
			if e.Request.Method == "POST" {
				if err := readConfigurationBody(e, &in.Config); err != nil {
					return err
				}
			} else {
				if err := readConfigurationBody(e, &in); err != nil {
					return err
				}
			}
			id := e.Request.PathValue("ruleId")
			if e.Request.Method == "PATCH" {
				if _, err := notices.Own(e.App, e.Auth.Id, id); err != nil {
					return notificationError(e, err)
				}
			}
			r, err := notices.For(e.App).Save(e.Request.Context(), e.Auth.Id, id, in)
			if err != nil {
				return notificationError(e, err)
			}
			a, err := rbac.Resolve(e.App, e.Auth.Id)
			if err != nil {
				return err
			}
			return response(e, notices.RuleView(r, a))
		})
	}
	rules.DELETE("/{ruleId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in struct {
			Revision int `json:"revision"`
		}
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		if err := notices.For(e.App).Delete(e.Auth.Id, e.Request.PathValue("ruleId"), in.Revision); err != nil {
			return notificationError(e, err)
		}
		return response(e, map[string]bool{"deleted": true})
	})
	rules.POST("/{ruleId}/check", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in struct{}
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		r, err := notices.For(e.App).Evaluate(e.Request.Context(), e.Auth.Id, e.Request.PathValue("ruleId"))
		if err != nil {
			return notificationError(e, err)
		}
		a, err := rbac.Resolve(e.App, e.Auth.Id)
		if err != nil {
			return err
		}
		return response(e, notices.RuleView(r, a))
	})
	n := group.Group("/notifications")
	n.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	n.GET("", listNotifications)
	n.POST("/{notificationId}/read", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in struct {
			Read *bool `json:"read"`
		}
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		if in.Read == nil {
			return e.BadRequestError("请指定已读状态。", nil)
		}
		err := e.App.RunInTransaction(func(tx core.App) error {
			r, err := tx.FindFirstRecordByFilter("notifications", "owner={:owner} && id={:id}", dbx.Params{"owner": e.Auth.Id, "id": e.Request.PathValue("notificationId")})
			if err != nil {
				return err
			}
			a, err := rbac.Resolve(tx, e.Auth.Id)
			if err != nil {
				return err
			}
			if !notices.Allows(a, r.GetString("kind"), r.GetString("cluster")) {
				return notices.ErrForbidden
			}
			r.Set("read", *in.Read)
			return tx.Save(r)
		})
		if err != nil {
			return notificationError(e, err)
		}
		notices.For(e.App).Changed(e.Auth.Id)
		return response(e, map[string]bool{"read": *in.Read})
	})
	n.POST("/read-all", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var in struct{}
		if err := readConfigurationBody(e, &in); err != nil {
			return err
		}
		count := 0
		err := e.App.RunInTransaction(func(tx core.App) error {
			a, err := rbac.Resolve(tx, e.Auth.Id)
			if err != nil {
				return err
			}
			rows := []*core.Record{}
			if err := tx.RecordQuery("notifications").AndWhere(notificationVisibility(e.Auth.Id, a)).AndWhere(dbx.HashExp{"read": false}).All(&rows); err != nil {
				return err
			}
			for _, r := range rows {
				r.Set("read", true)
				if err := tx.Save(r); err != nil {
					return err
				}
				count++
			}
			return nil
		})
		if err != nil {
			return notificationError(e, err)
		}
		notices.For(e.App).Changed(e.Auth.Id)
		return response(e, map[string]int{"updated": count})
	})
	n.GET("/stream", notificationStream)
}
func listNotifications(e *core.RequestEvent) error {
	if err := slurmQuery(e, "clusterId", "read", "phase", "page", "perPage"); err != nil {
		return err
	}
	q := e.Request.URL.Query()
	if !slices.Contains([]string{"", "all", "unread", "read"}, q.Get("read")) || !slices.Contains([]string{"", "firing", "recovered"}, q.Get("phase")) {
		return e.BadRequestError("通知筛选不合法。", nil)
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	a, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	base := notificationVisibility(e.Auth.Id, a)
	filter := []dbx.Expression{base}
	if cid := q.Get("clusterId"); cid != "" {
		filter = append(filter, dbx.HashExp{"cluster": cid})
	}
	if v := q.Get("read"); v == "unread" || v == "read" {
		filter = append(filter, dbx.HashExp{"read": v == "read"})
	}
	if phase := q.Get("phase"); phase != "" {
		filter = append(filter, dbx.HashExp{"phase": phase})
	}
	total, err := e.App.CountRecords("notifications", dbx.And(filter...))
	if err != nil {
		return err
	}
	unread, err := e.App.CountRecords("notifications", dbx.And(base, dbx.HashExp{"read": false}))
	if err != nil {
		return err
	}
	rows := []*core.Record{}
	if err = e.App.RecordQuery("notifications").AndWhere(dbx.And(filter...)).OrderBy("created DESC", "id DESC").Limit(int64(size)).Offset(int64((page - 1) * size)).All(&rows); err != nil {
		return err
	}
	fresh, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return err
	}
	if permissionFingerprint(a) != permissionFingerprint(fresh) {
		return e.ForbiddenError("权限已变化，请刷新通知。", nil)
	}
	items := []map[string]any{}
	for _, r := range rows {
		items = append(items, notificationView(r))
	}
	return response(e, map[string]any{"items": items, "page": page, "perPage": size, "totalItems": total, "unreadCount": unread})
}
func notificationStream(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	flusher, ok := e.Response.(http.Flusher)
	if !ok {
		return e.Error(503, "通知实时连接不可用。", nil)
	}
	service := notices.For(e.App)
	events, unsubscribe, err := service.Subscribe(e.Auth.Id)
	if err != nil {
		return notificationError(e, err)
	}
	defer unsubscribe()
	e.Response.Header().Set("Content-Type", "text/event-stream")
	e.Response.Header().Set("X-Accel-Buffering", "no")
	e.Response.WriteHeader(200)
	send := func(message string) bool {
		_ = http.NewResponseController(e.Response).SetWriteDeadline(time.Now().Add(30 * time.Second))
		_, err := fmt.Fprint(e.Response, message)
		if err == nil {
			flusher.Flush()
		}
		return err == nil
	}
	if !send("event: ready\ndata: {}\n\n") {
		return nil
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(5 * time.Minute)
	defer lifetime.Stop()
	token := strings.TrimPrefix(e.Request.Header.Get("Authorization"), "Bearer ")
	for {
		select {
		case <-service.Done():
			return nil
		case <-e.Request.Context().Done():
			return nil
		case <-lifetime.C:
			return nil
		case <-events:
			if !send("event: changed\ndata: {}\n\n") {
				return nil
			}
		case <-heartbeat.C:
			u, err := e.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
			ctx, cancel := context.WithTimeout(e.Request.Context(), 3*time.Second)
			allowed := err == nil && u.Id == e.Auth.Id && auth.CheckBackgroundUser(ctx, e.App, u) == nil
			cancel()
			if !allowed {
				return nil
			}
			if !send(": heartbeat\n\n") {
				return nil
			}
		}
	}
}
