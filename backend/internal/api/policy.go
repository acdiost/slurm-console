package api

import (
	"encoding/json"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"slices"
	"slurm-console/backend/internal/accounting"
	"slurm-console/backend/internal/auth"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
	"strings"
)

func registerPolicyRoutes(group *router.RouterGroup[*core.RequestEvent]) {
	routes := group.Group("/clusters/{clusterId}")
	routes.Bind(apis.RequireAuth("users")).BindFunc(auth.RequireActiveUser)
	routes.GET("/qos", listQOS).BindFunc(rbac.Require("qos.read", true)).BindFunc(rbac.Require("associations.read", true))
	routes.GET("/qos/{name}", qosDetail).BindFunc(rbac.Require("qos.read", true)).BindFunc(rbac.Require("associations.read", true))
	routes.GET("/fairshare", listFairshare).BindFunc(rbac.Require("fairshare.read", true)).BindFunc(rbac.Require("associations.read", true))
	routes.GET("/qos-operations", func(e *core.RequestEvent) error {
		if err := slurmQuery(e); err != nil {
			return err
		}
		v, err := accounting.For(e.App).RecentQOS(e.Auth.Id, e.Request.PathValue("clusterId"))
		if err != nil {
			return slurmFailure(e, err)
		}
		return response(e, map[string]any{"items": v})
	}).BindFunc(rbac.Require("qos.read", true))
}

func policyData(e *core.RequestEvent, kind, permission string) (services.SlurmResult, rbac.Access, error) {
	result, err := services.SlurmFor(e.App).Read(e.Request.Context(), e.Request.PathValue("clusterId"), kind, "")
	if err != nil {
		return result, rbac.Access{}, slurmFailure(e, err)
	}
	access, err := rbac.Resolve(e.App, e.Auth.Id)
	if err != nil {
		return result, access, e.InternalServerError("", err)
	}
	if !access.Allows(permission, e.Request.PathValue("clusterId")) || !access.Allows("associations.read", e.Request.PathValue("clusterId")) {
		return result, access, e.ForbiddenError("该集群策略读取权限已撤销。", nil)
	}
	return result, access, nil
}

func visibleQOS(snapshot models.PolicySnapshot, global bool) []models.QOS {
	if global {
		return snapshot.QOS
	}
	refs := map[string]bool{}
	for _, a := range snapshot.Associations {
		for _, q := range a.QOS {
			refs[q] = true
		}
		if a.DefaultQOS != "" {
			refs[a.DefaultQOS] = true
		}
	}
	out := []models.QOS{}
	for _, q := range snapshot.QOS {
		if refs[q.Name] {
			out = append(out, q)
		}
	}
	return out
}

func listQOS(e *core.RequestEvent) error {
	if err := slurmQuery(e, "q", "page", "perPage", "sort", "order"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	key, order := q.Get("sort"), q.Get("order")
	if key == "" {
		key = "name"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"name", "priority"}, key) || !slices.Contains([]string{"asc", "desc"}, order) {
		return e.BadRequestError("QOS 排序参数不合法。", nil)
	}
	result, access, err := policyData(e, "qos", "qos.read")
	if err != nil {
		return err
	}
	var snapshot models.PolicySnapshot
	if json.Unmarshal(result.Data, &snapshot) != nil {
		return e.InternalServerError("", nil)
	}
	global := access.Allows("platform.qos.read", "")
	values := []models.QOS{}
	for _, v := range visibleQOS(snapshot, global) {
		if contains(q.Get("q"), v.Name+" "+v.Description) {
			values = append(values, v)
		}
	}
	slices.SortFunc(values, func(a, b models.QOS) int {
		cmp := strings.Compare(a.Name, b.Name)
		if key == "priority" {
			av, bv := int64(-1), int64(-1)
			if a.Priority.Value != nil {
				av = *a.Priority.Value
			}
			if b.Priority.Value != nil {
				bv = *b.Priority.Value
			}
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
	total := len(values)
	start := min((page-1)*size, total)
	values = values[start:min(start+size, total)]
	scope := "referenced"
	if global {
		scope = "global"
	}
	return response(e, map[string]any{"items": values, "page": page, "perPage": size, "totalItems": total, "fetchedAt": result.FetchedAt, "cached": result.Cached, "slurmCluster": snapshot.Cluster, "catalogScope": scope})
}

func qosDetail(e *core.RequestEvent) error {
	if err := slurmQuery(e); err != nil {
		return err
	}
	name := e.Request.PathValue("name")
	if !client.ValidAccountingName(name) {
		return e.BadRequestError("QOS 名称不合法。", nil)
	}
	result, access, err := policyData(e, "qos", "qos.read")
	if err != nil {
		return err
	}
	var snapshot models.PolicySnapshot
	if json.Unmarshal(result.Data, &snapshot) != nil {
		return e.InternalServerError("", nil)
	}
	for _, v := range visibleQOS(snapshot, access.Allows("platform.qos.read", "")) {
		if v.Name == name {
			return response(e, map[string]any{"item": v, "fetchedAt": result.FetchedAt, "cached": result.Cached})
		}
	}
	return slurmFailure(e, client.Failure("not_found", "QOS 不存在或不在当前可见范围。"))
}

func listFairshare(e *core.RequestEvent) error {
	if err := slurmQuery(e, "q", "page", "perPage", "sort", "order", "account", "user", "kind"); err != nil {
		return err
	}
	page, size, err := pagination(e)
	if err != nil {
		return err
	}
	q := e.Request.URL.Query()
	key, order := q.Get("sort"), q.Get("order")
	if key == "" {
		key = "account"
	}
	if order == "" {
		order = "asc"
	}
	if !slices.Contains([]string{"account", "user", "factor", "id"}, key) || !slices.Contains([]string{"asc", "desc"}, order) || !slices.Contains([]string{"", "account", "user"}, q.Get("kind")) {
		return e.BadRequestError("Fairshare 筛选或排序参数不合法。", nil)
	}
	result, _, err := policyData(e, "fairshare", "fairshare.read")
	if err != nil {
		return err
	}
	var raw []models.Fairshare
	if json.Unmarshal(result.Data, &raw) != nil {
		return e.InternalServerError("", nil)
	}
	values := []models.Fairshare{}
	for _, v := range raw {
		if !contains(q.Get("q"), v.ID+" "+v.Account+" "+v.User+" "+v.Partition) || q.Get("account") != "" && q.Get("account") != v.Account || q.Get("user") != "" && q.Get("user") != v.User || q.Get("kind") == "account" && v.User != "" || q.Get("kind") == "user" && v.User == "" {
			continue
		}
		values = append(values, v)
	}
	slices.SortFunc(values, func(a, b models.Fairshare) int {
		cmp := 0
		switch key {
		case "account":
			cmp = strings.Compare(a.Account, b.Account)
		case "user":
			cmp = strings.Compare(a.User, b.User)
		case "factor":
			av, bv := float64(-1), float64(-1)
			if a.Factor.Value != nil {
				av = *a.Factor.Value
			}
			if b.Factor.Value != nil {
				bv = *b.Factor.Value
			}
			if av < bv {
				cmp = -1
			} else if av > bv {
				cmp = 1
			}
		}
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
	return pageResult(e, values, result, page, size)
}
