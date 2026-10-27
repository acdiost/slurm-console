package api

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/linuxusers"
	"slurm-console/backend/internal/rbac"
)

func registerLinuxUserRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}/linux-users")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser).BindFunc(rbac.Require("linux_users.read", true))
	routes.GET("", listLinuxUsers)
	routes.GET("/{username}", getLinuxUser)
}
func linuxUserFailure(e *core.RequestEvent, err error) error {
	if e.Request.Context().Err() != nil {
		return e.Error(408, "Linux 用户查询已取消。", nil)
	}
	var failure *linuxusers.Failure
	if !errors.As(err, &failure) {
		return e.InternalServerError("", err)
	}
	status := http.StatusServiceUnavailable
	switch failure.Code {
	case "not_found":
		status = 404
	case "changed", "disabled":
		status = 409
	case "busy":
		status = 429
	}
	e.Set("publicErrorCode", "LINUX_USERS_"+strings.ToUpper(failure.Code))
	e.Set("publicErrorMessage", failure.Message)
	return e.Error(status, failure.Message, nil)
}
func readLinuxUsers(e *core.RequestEvent, name string) (linuxusers.Result, error) {
	id := e.Request.PathValue("clusterId")
	result, err := linuxusers.For(e.App).Read(e.Request.Context(), id, name)
	if err != nil {
		return result, linuxUserFailure(e, err)
	}
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return result, e.InternalServerError("", err)
	}
	if !access.Allows("linux_users.read", id) {
		return result, e.ForbiddenError("Linux 用户读取授权已变化。", nil)
	}
	return result, nil
}
func listLinuxUsers(e *core.RequestEvent) error {
	if err := clusterQuery(e, "page", "perPage", "q", "source", "kind", "sort", "order"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	source, kind, key, order := q.Get("source"), q.Get("kind"), q.Get("sort"), q.Get("order")
	if key == "" {
		key = "name"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"", "local", "nss", "unknown"}, source) || !slices.Contains([]string{"", "privileged", "system", "regular", "unknown"}, kind) || !slices.Contains([]string{"name", "uid", "gid"}, key) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("Linux 用户筛选或排序不合法。", nil)
	}
	result, err := readLinuxUsers(e, "")
	if err != nil {
		return err
	}
	values := make([]linuxusers.User, 0, len(result.Items))
	for _, u := range result.Items {
		if (source == "" || source == u.Source) && (kind == "" || kind == u.Kind) && (contains(q.Get("q"), u.Name) || contains(q.Get("q"), u.Comment) || contains(q.Get("q"), u.Home)) {
			values = append(values, u)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		a, b := values[i], values[j]
		if key == "uid" && a.UID != b.UID {
			if order == "desc" {
				return a.UID > b.UID
			}
			return a.UID < b.UID
		}
		if key == "gid" && a.GID != b.GID {
			if order == "desc" {
				return a.GID > b.GID
			}
			return a.GID < b.GID
		}
		if key == "name" && order == "desc" {
			return a.Name > b.Name
		}
		return a.Name < b.Name
	})
	total := len(values)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	return response(e, map[string]any{"items": values[start:end], "page": page, "perPage": size, "totalItems": total, "fetchedAt": result.FetchedAt, "cached": false, "sourceLabel": result.SourceLabel, "enumeration": result.Enumeration, "uidMin": result.UIDMin, "revision": result.Revision})
}
func getLinuxUser(e *core.RequestEvent) error {
	if err := clusterQuery(e); err != nil {
		return err
	}
	name := e.Request.PathValue("username")
	if !linuxusers.ValidName(name) {
		return e.BadRequestError("Linux 用户名不合法。", nil)
	}
	result, err := readLinuxUsers(e, name)
	if err != nil {
		return err
	}
	return response(e, map[string]any{"item": result.Items[0], "fetchedAt": result.FetchedAt, "cached": false, "sourceLabel": result.SourceLabel, "enumeration": result.Enumeration, "uidMin": result.UIDMin, "revision": result.Revision})
}
