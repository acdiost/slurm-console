package rbac

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type Cluster struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}
type ClusterAccess struct {
	Cluster     Cluster  `json:"cluster"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}
type Access struct {
	PlatformRoles       []string        `json:"platformRoles"`
	PlatformPermissions []string        `json:"platformPermissions"`
	Clusters            []ClusterAccess `json:"clusters"`
}

func ClusterDTO(record *core.Record) Cluster {
	return Cluster{ID: record.Id, Name: record.GetString("name"), Slug: record.GetString("slug"), Description: record.GetString("description")}
}

// Resolve reads authoritative grants for each request; no permissions are trusted
// from JWT claims or browser state, and no authorization result is cached.
func Resolve(app core.App, userID string) (Access, error) {
	result := Access{PlatformRoles: []string{}, PlatformPermissions: []string{}, Clusters: []ClusterAccess{}}
	user, err := app.FindRecordById("users", userID)
	if err != nil {
		return result, err
	}
	if !user.GetBool("active") {
		return result, nil
	}
	grants, err := app.FindRecordsByFilter("user_platform_roles", "user = {:user}", "", 0, 0, dbx.Params{"user": userID})
	if err != nil {
		return result, err
	}
	for _, grant := range grants {
		role, err := app.FindRecordById("roles", grant.GetString("role"))
		if err != nil {
			return result, err
		}
		if role.GetString("scope") != "platform" {
			continue
		}
		permissions, err := rolePermissions(app, role.Id, "")
		if err != nil {
			return result, err
		}
		result.PlatformRoles = append(result.PlatformRoles, role.GetString("name"))
		result.PlatformPermissions = append(result.PlatformPermissions, permissions...)
	}
	grants, err = app.FindRecordsByFilter("user_cluster_roles", "user = {:user}", "", 0, 0, dbx.Params{"user": userID})
	if err != nil {
		return result, err
	}
	byCluster := map[string]*ClusterAccess{}
	for _, grant := range grants {
		role, err := app.FindRecordById("roles", grant.GetString("role"))
		if err != nil {
			return result, err
		}
		if role.GetString("scope") != "cluster" {
			continue
		}
		clusterID := grant.GetString("cluster")
		entry := byCluster[clusterID]
		if entry == nil {
			record, err := app.FindRecordById("clusters", clusterID)
			if err != nil {
				return result, err
			}
			entry = &ClusterAccess{Cluster: ClusterDTO(record), Roles: []string{}, Permissions: []string{}}
			byCluster[clusterID] = entry
		}
		permissions, err := rolePermissions(app, role.Id, "cluster")
		if err != nil {
			return result, err
		}
		entry.Roles = append(entry.Roles, role.GetString("name"))
		entry.Permissions = append(entry.Permissions, permissions...)
	}
	result.PlatformRoles = unique(result.PlatformRoles)
	result.PlatformPermissions = unique(result.PlatformPermissions)
	for _, entry := range byCluster {
		entry.Roles = unique(entry.Roles)
		entry.Permissions = unique(entry.Permissions)
		result.Clusters = append(result.Clusters, *entry)
	}
	sort.Slice(result.Clusters, func(i, j int) bool { return result.Clusters[i].Cluster.ID < result.Clusters[j].Cluster.ID })
	return result, nil
}

func rolePermissions(app core.App, roleID, scope string) ([]string, error) {
	bindings, err := app.FindRecordsByFilter("role_permissions", "role = {:role}", "", 0, 0, dbx.Params{"role": roleID})
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, binding := range bindings {
		permission, err := app.FindRecordById("permissions", binding.GetString("permission"))
		if err != nil {
			return nil, err
		}
		if scope == "" || permission.GetString("scope") == scope {
			result = append(result, permission.GetString("code"))
		}
	}
	return unique(result), nil
}

func unique(values []string) []string { sort.Strings(values); return slices.Compact(values) }

func (access Access) Allows(permission, clusterID string) bool {
	if slices.Contains(access.PlatformPermissions, permission) {
		return true
	}
	if clusterID == "" {
		return false
	}
	for _, entry := range access.Clusters {
		if entry.Cluster.ID == clusterID && slices.Contains(entry.Permissions, permission) {
			return true
		}
	}
	return false
}

func Require(permission string, clusterScoped bool) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if e.Auth == nil || e.Auth.Collection().Name != "users" || !e.Auth.GetBool("active") {
			return e.UnauthorizedError("请重新登录平台。", nil)
		}
		clusterID := ""
		if clusterScoped {
			clusterID = e.Request.PathValue("clusterId")
			if clusterID == "" {
				return e.ForbiddenError("缺少集群授权范围。", nil)
			}
		}
		access, err := Resolve(e.App, e.Auth.Id)
		if err != nil {
			return e.InternalServerError("", err)
		}
		if !access.Allows(permission, clusterID) {
			return e.ForbiddenError("你没有执行此操作的权限。", nil)
		}
		return e.Next()
	}
}

// Grant is used only by trusted local administration commands, never public CRUD.
func Grant(app core.App, userID, roleCode, clusterID string) (*core.Record, error) {
	if _, err := app.FindRecordById("users", userID); err != nil {
		return nil, err
	}
	role, err := app.FindFirstRecordByFilter("roles", "code = {:code}", dbx.Params{"code": roleCode})
	if err != nil {
		return nil, err
	}
	collectionName := "user_platform_roles"
	filter := "user = {:user} && role = {:role}"
	params := dbx.Params{"user": userID, "role": role.Id}
	if role.GetString("scope") == "cluster" {
		if clusterID == "" {
			return nil, fmt.Errorf("cluster role requires --cluster")
		}
		if _, err := app.FindRecordById("clusters", clusterID); err != nil {
			return nil, err
		}
		collectionName = "user_cluster_roles"
		filter += " && cluster = {:cluster}"
		params["cluster"] = clusterID
	} else if clusterID != "" {
		return nil, fmt.Errorf("platform role cannot be scoped to a cluster")
	}
	if existing, err := app.FindFirstRecordByFilter(collectionName, filter, params); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(collection)
	record.Set("user", userID)
	record.Set("role", role.Id)
	if clusterID != "" {
		record.Set("cluster", clusterID)
	}
	if err := app.Save(record); err != nil {
		return nil, err
	}
	return record, nil
}
