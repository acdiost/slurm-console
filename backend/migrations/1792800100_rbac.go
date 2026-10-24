package migrations

import (
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// New base collections default to locked API rules (nil).
		roles := core.NewBaseCollection("roles")
		roles.Fields.Add(&core.TextField{Name: "code", Required: true, Max: 64}, &core.TextField{Name: "name", Required: true, Max: 100}, &core.SelectField{Name: "scope", Required: true, MaxSelect: 1, Values: []string{"platform", "cluster"}})
		roles.Indexes = append(roles.Indexes, "CREATE UNIQUE INDEX idx_roles_code ON roles (code)")
		if err := app.Save(roles); err != nil {
			return err
		}
		permissions := core.NewBaseCollection("permissions")
		permissions.Fields.Add(&core.TextField{Name: "code", Required: true, Max: 100}, &core.TextField{Name: "name", Required: true, Max: 100}, &core.SelectField{Name: "scope", Required: true, MaxSelect: 1, Values: []string{"platform", "cluster"}})
		permissions.Indexes = append(permissions.Indexes, "CREATE UNIQUE INDEX idx_permissions_code ON permissions (code)")
		if err := app.Save(permissions); err != nil {
			return err
		}
		clusters := core.NewBaseCollection("clusters")
		clusters.Fields.Add(&core.TextField{Name: "name", Required: true, Max: 100}, &core.TextField{Name: "slug", Required: true, Max: 64, Pattern: "^[a-z0-9][a-z0-9-]*$"}, &core.TextField{Name: "description", Max: 1000})
		clusters.Indexes = append(clusters.Indexes, "CREATE UNIQUE INDEX idx_clusters_slug ON clusters (slug)")
		if err := app.Save(clusters); err != nil {
			return err
		}
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		relation := func(name, id string) *core.RelationField {
			return &core.RelationField{Name: name, CollectionId: id, Required: true, MaxSelect: 1, CascadeDelete: true}
		}
		bindings := core.NewBaseCollection("role_permissions")
		bindings.Fields.Add(relation("role", roles.Id), relation("permission", permissions.Id))
		bindings.Indexes = append(bindings.Indexes, "CREATE UNIQUE INDEX idx_role_permissions_pair ON role_permissions (role, permission)")
		if err := app.Save(bindings); err != nil {
			return err
		}
		platformGrants := core.NewBaseCollection("user_platform_roles")
		platformGrants.Fields.Add(relation("user", users.Id), relation("role", roles.Id))
		platformGrants.Indexes = append(platformGrants.Indexes, "CREATE UNIQUE INDEX idx_user_platform_role ON user_platform_roles (user, role)")
		if err := app.Save(platformGrants); err != nil {
			return err
		}
		clusterGrants := core.NewBaseCollection("user_cluster_roles")
		clusterGrants.Fields.Add(relation("user", users.Id), relation("cluster", clusters.Id), relation("role", roles.Id))
		clusterGrants.Indexes = append(clusterGrants.Indexes, "CREATE UNIQUE INDEX idx_user_cluster_role ON user_cluster_roles (user, cluster, role)")
		if err := app.Save(clusterGrants); err != nil {
			return err
		}
		definitions := []struct{ code, name, scope string }{
			{"platform.users.manage", "管理平台用户", "platform"}, {"platform.roles.manage", "管理角色授权", "platform"}, {"platform.clusters.manage", "管理集群配置", "platform"}, {"platform.audit.read", "查看平台审计", "platform"},
			{"clusters.read", "查看集群", "cluster"}, {"jobs.read", "查看作业", "cluster"}, {"jobs.cancel", "取消作业", "cluster"}, {"jobs.requeue", "重新排队", "cluster"}, {"nodes.read", "查看节点", "cluster"}, {"nodes.drain", "排空节点", "cluster"}, {"nodes.resume", "恢复节点", "cluster"}, {"partitions.read", "查看分区", "cluster"}, {"accounts.read", "查看账户", "cluster"}, {"accounts.update", "管理账户", "cluster"}, {"qos.read", "查看 QOS", "cluster"}, {"qos.update", "管理 QOS", "cluster"}, {"associations.read", "查看关联", "cluster"}, {"associations.update", "管理关联", "cluster"}, {"metrics.read", "查看监控指标", "cluster"},
		}
		permissionRecords := map[string]*core.Record{}
		for _, definition := range definitions {
			record := core.NewRecord(permissions)
			record.Set("code", definition.code)
			record.Set("name", definition.name)
			record.Set("scope", definition.scope)
			if err := app.Save(record); err != nil {
				return err
			}
			permissionRecords[definition.code] = record
		}
		for _, definition := range []struct{ code, name, scope string }{{"super_admin", "Super Admin", "platform"}, {"cluster_admin", "Cluster Admin", "cluster"}, {"operator", "Operator", "cluster"}, {"viewer", "Viewer", "cluster"}} {
			role := core.NewRecord(roles)
			role.Set("code", definition.code)
			role.Set("name", definition.name)
			role.Set("scope", definition.scope)
			if err := app.Save(role); err != nil {
				return err
			}
			for _, permission := range definitions {
				allowed := definition.code == "super_admin" || (definition.code == "cluster_admin" && permission.scope == "cluster")
				readOnly := permission.scope == "cluster" && (len(permission.code) > 5 && permission.code[len(permission.code)-5:] == ".read")
				if definition.code == "viewer" {
					allowed = readOnly
				}
				if definition.code == "operator" {
					allowed = readOnly || permission.code == "jobs.cancel" || permission.code == "nodes.drain" || permission.code == "nodes.resume"
				}
				if allowed {
					binding := core.NewRecord(bindings)
					binding.Set("role", role.Id)
					binding.Set("permission", permissionRecords[permission.code].Id)
					if err := app.Save(binding); err != nil {
						return fmt.Errorf("seed role permission: %w", err)
					}
				}
			}
		}
		return nil
	}, func(core.App) error {
		return errors.New("RBAC migration retains account grants; restore a pre-migration backup to roll back")
	})
}
