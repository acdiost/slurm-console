package api

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slurm-console/backend/internal/accounting"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
)

func registerAccountingRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	for _, resource := range []struct{ Path, Kind, Permission string }{{"accounts", "accounts", "accounts.read"}, {"slurm-users", "slurm_users", "slurm_users.read"}, {"associations", "associations", "associations.read"}} {
		routes.GET("/"+resource.Path, func(e *core.RequestEvent) error { return listAccounting(e, resource.Kind, resource.Permission) }).BindFunc(rbac.Require(resource.Permission, true))
		routes.GET("/"+resource.Path+"/{name}", func(e *core.RequestEvent) error { return accountingDetail(e, resource.Kind, resource.Permission) }).BindFunc(rbac.Require(resource.Permission, true))
	}
	routes.POST("/accounting-controls/prepare", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var input struct {
			Action string          `json:"action"`
			Spec   accounting.Spec `json:"spec"`
		}
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		v, err := accounting.For(e.App).Prepare(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), input.Action, input.Spec)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
	routes.POST("/accounting-controls/execute", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		var input accounting.Input
		if err := readConfigurationBody(e, &input); err != nil {
			return err
		}
		id, _ := e.Get("requestId").(string)
		v, err := accounting.For(e.App).Execute(e.Auth.Id, e.Request.PathValue("clusterId"), id, input)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
	routes.GET("/accounting-operations", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, err := accounting.For(e.App).Recent(e.Auth.Id, e.Request.PathValue("clusterId"))
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, map[string]any{"items": v})
	})
	routes.GET("/accounting-operations/{operationId}", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		id := e.Request.PathValue("operationId")
		if len(id) != 26 {
			return e.BadRequestError("操作编号不合法。", nil)
		}
		v, err := accounting.For(e.App).Get(e.Request.Context(), e.Auth.Id, e.Request.PathValue("clusterId"), id)
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, v)
	})
}
func accountingData(e *core.RequestEvent, kind, permission string) (services.SlurmResult, error) {
	cid := e.Request.PathValue("clusterId")
	result, err := services.SlurmFor(e.App).Read(e.Request.Context(), cid, kind, "")
	if err != nil {
		return result, slurmFailure(e, err)
	}
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return result, e.InternalServerError("", err)
	}
	if !access.Allows(permission, cid) {
		return result, e.ForbiddenError("该集群读取权限已撤销。", nil)
	}
	return result, nil
}
func listAccounting(e *core.RequestEvent, kind, permission string) error {
	if err := slurmQuery(e, "q", "page", "perPage", "sort", "order", "account", "user", "partition", "kind"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	query := e.Request.URL.Query()
	key, order := query.Get("sort"), query.Get("order")
	if key == "" {
		key = "name"
		if kind == "associations" {
			key = "id"
		}
	}
	if order == "" {
		order = "asc"
	}
	keys := []string{"name"}
	if kind == "associations" {
		keys = []string{"id", "account", "user", "partition"}
	}
	if !slices.Contains(keys, key) || !slices.Contains([]string{"asc", "desc"}, order) || !slices.Contains([]string{"", "account", "user"}, query.Get("kind")) {
		return e.BadRequestError("记账筛选或排序参数不合法。", nil)
	}
	if kind != "associations" && (query.Get("account") != "" || query.Get("user") != "" || query.Get("partition") != "" || query.Get("kind") != "") {
		return e.BadRequestError("当前资源不支持关联筛选。", nil)
	}
	result, err := accountingData(e, kind, permission)
	if err != nil {
		return err
	}
	switch kind {
	case "accounts":
		values := []models.Account{}
		if json.Unmarshal(result.Data, &values) != nil {
			return e.InternalServerError("", nil)
		}
		out := []models.Account{}
		for _, v := range values {
			if contains(query.Get("q"), v.Name+" "+v.Description+" "+v.Organization) {
				out = append(out, v)
			}
		}
		slices.SortFunc(out, func(a, b models.Account) int {
			cmp := strings.Compare(a.Name, b.Name)
			if order == "desc" {
				return -cmp
			}
			return cmp
		})
		return pageResult(e, out, result, page, size)
	case "slurm_users":
		values := []models.SlurmUser{}
		if json.Unmarshal(result.Data, &values) != nil {
			return e.InternalServerError("", nil)
		}
		out := []models.SlurmUser{}
		for _, v := range values {
			if contains(query.Get("q"), v.Name+" "+v.DefaultAccount) {
				out = append(out, v)
			}
		}
		slices.SortFunc(out, func(a, b models.SlurmUser) int {
			cmp := strings.Compare(a.Name, b.Name)
			if order == "desc" {
				return -cmp
			}
			return cmp
		})
		return pageResult(e, out, result, page, size)
	default:
		values := []models.Association{}
		if json.Unmarshal(result.Data, &values) != nil {
			return e.InternalServerError("", nil)
		}
		out := []models.Association{}
		for _, v := range values {
			if !contains(query.Get("q"), v.ID+" "+v.Account+" "+v.User+" "+v.Partition+" "+v.ParentAccount) {
				continue
			}
			if query.Get("account") != "" && query.Get("account") != v.Account || query.Get("user") != "" && query.Get("user") != v.User || query.Get("partition") != "" && query.Get("partition") != v.Partition || query.Get("kind") == "user" && v.User == "" || query.Get("kind") == "account" && v.User != "" {
				continue
			}
			out = append(out, v)
		}
		slices.SortFunc(out, func(a, b models.Association) int {
			value := func(v models.Association) string {
				switch key {
				case "account":
					return v.Account
				case "user":
					return v.User
				case "partition":
					return v.Partition
				}
				return ""
			}
			cmp := strings.Compare(value(a), value(b))
			if cmp == 0 {
				av, _ := strconv.ParseUint(a.ID, 10, 32)
				bv, _ := strconv.ParseUint(b.ID, 10, 32)
				if av < bv {
					cmp = -1
				} else if av > bv {
					cmp = 1
				}
			}
			if order == "desc" {
				return -cmp
			}
			return cmp
		})
		return pageResult(e, out, result, page, size)
	}
}
func accountingDetail(e *core.RequestEvent, kind, permission string) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	name := e.Request.PathValue("name")
	if kind == "associations" && !client.ValidJobID(name) || kind != "associations" && !client.ValidAccountingName(name) {
		return e.BadRequestError("记账目标名称或 ID 不合法。", nil)
	}
	result, err := accountingData(e, kind, permission)
	if err != nil {
		return err
	}
	var item any
	switch kind {
	case "accounts":
		var values []models.Account
		_ = json.Unmarshal(result.Data, &values)
		for _, v := range values {
			if v.Name == name {
				item = v
			}
		}
	case "slurm_users":
		var values []models.SlurmUser
		_ = json.Unmarshal(result.Data, &values)
		for _, v := range values {
			if v.Name == name {
				item = v
			}
		}
	case "associations":
		var values []models.Association
		_ = json.Unmarshal(result.Data, &values)
		for _, v := range values {
			if v.ID == name {
				item = v
			}
		}
	}
	if item == nil {
		return slurmFailure(e, client.Failure("not_found", "记账资源不存在或未关联所选集群。"))
	}
	return response(e, map[string]any{"item": item, "fetchedAt": result.FetchedAt, "cached": result.Cached})
}
