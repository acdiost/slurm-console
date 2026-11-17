package confighistory_test

import (
	"fmt"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/confighistory"
)

func TestAuthenticationMigrationInfersOnlyUnambiguousGrantEvidenceAndPreservesRecords(t *testing.T) {
	for _, scenario := range []struct {
		name                            string
		clusterSources, platformSources []string
		want                            string
		directoryScope                  string
	}{
		{"no external evidence", []string{"local"}, []string{"local"}, "pam", ""},
		{"explicit PAM", []string{"pam"}, nil, "pam", ""},
		{"explicit LDAP", []string{"ldap", "ldap"}, nil, "ldap", ""},
		{"explicit FreeIPA overrides platform conflicts", []string{"freeipa"}, []string{"pam", "ldap"}, "freeipa", ""},
		{"platform fallback", []string{"local"}, []string{"ldap", "ldap"}, "ldap", ""},
		{"conflicting explicit grants", []string{"pam", "ldap"}, []string{"freeipa"}, "", ""},
		{"conflicting platform grants", nil, []string{"ldap", "freeipa"}, "", ""},
		{"directory cluster mapping", nil, nil, "freeipa", "cluster"},
		{"directory platform mapping", nil, nil, "freeipa", "platform"},
		{"directory conflicts with manual PAM", []string{"pam"}, nil, "", "cluster"},
		{"explicit LDAP overrides directory platform", []string{"ldap"}, nil, "ldap", "platform"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
			defer app.ResetBootstrapState()
			if err := app.Bootstrap(); err != nil {
				t.Fatal(err)
			}
			var previous core.MigrationsList
			previous.Copy(core.SystemMigrations)
			for _, migration := range core.AppMigrations.Items() {
				if migration.File < "1794830000_cluster_authentication_method.go" {
					previous.Add(migration)
				}
			}
			if _, err := core.NewMigrationsRunner(app, previous).Up(); err != nil {
				t.Fatal(err)
			}
			clusters, err := app.FindCollectionByNameOrId("clusters")
			if err != nil {
				t.Fatal(err)
			}
			cluster := core.NewRecord(clusters)
			cluster.Set("name", "Retained legacy cluster")
			cluster.Set("slug", "legacy")
			cluster.Set("revision", 4)
			if err := app.Save(cluster); err != nil {
				t.Fatal(err)
			}
			users, _ := app.FindCollectionByNameOrId("users")
			clusterRoles, _ := app.FindCollectionByNameOrId("user_cluster_roles")
			platformRoles, _ := app.FindCollectionByNameOrId("user_platform_roles")
			viewer, _ := app.FindFirstRecordByData("roles", "code", "viewer")
			admin, _ := app.FindFirstRecordByData("roles", "code", "super_admin")
			userCount := 0
			for _, scope := range []struct {
				sources    []string
				collection *core.Collection
				roleID     string
			}{{scenario.clusterSources, clusterRoles, viewer.Id}, {scenario.platformSources, platformRoles, admin.Id}} {
				for _, source := range scope.sources {
					userCount++
					user := core.NewRecord(users)
					user.SetEmail(fmt.Sprintf("identity%d@example.com", userCount))
					user.SetPassword("retained-platform-password")
					user.Set("auth_source", source)
					user.Set("username", fmt.Sprintf("identity%d", userCount))
					user.Set("active", true)
					if err := app.Save(user); err != nil {
						t.Fatal(err)
					}
					grant := core.NewRecord(scope.collection)
					grant.Set("user", user.Id)
					grant.Set("role", scope.roleID)
					if scope.collection.Name == "user_cluster_roles" {
						grant.Set("cluster", cluster.Id)
					}
					if err := app.Save(grant); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario.directoryScope != "" {
				mappings, err := app.FindCollectionByNameOrId("directory_role_mappings")
				if err != nil {
					t.Fatal(err)
				}
				mapping := core.NewRecord(mappings)
				mapping.Set("group_name", "research")
				mapping.Set("group_id", "retained-directory-group")
				mapping.Set("role", admin.Id)
				if scenario.directoryScope == "cluster" {
					mapping.Set("role", viewer.Id)
					mapping.Set("cluster", cluster.Id)
				}
				if err := app.Save(mapping); err != nil {
					t.Fatal(err)
				}
			}
			linuxSources, _ := app.FindCollectionByNameOrId("linux_user_sources")
			source := core.NewRecord(linuxSources)
			source.Set("cluster", cluster.Id)
			source.Set("revision", 2)
			source.Set("transport", "ssh")
			source.Set("host", "retained.example.com")
			source.Set("private_key", "retained-server-managed-credential")
			if err := app.Save(source); err != nil {
				t.Fatal(err)
			}
			if err := app.RunAllMigrations(); err != nil {
				t.Fatal(err)
			}
			if err := app.RunAllMigrations(); err != nil {
				t.Fatal(err)
			}
			current, err := app.FindRecordById("clusters", cluster.Id)
			wantRevision := 4
			if scenario.want != "" {
				wantRevision++
			}
			if err != nil || current.GetString("authentication_method") != scenario.want || current.GetInt("revision") != wantRevision {
				t.Fatal("incorrect inference or revision", err, current)
			}
			for collection, want := range map[string]int{"users": userCount, "user_cluster_roles": len(scenario.clusterSources), "user_platform_roles": len(scenario.platformSources), "linux_user_sources": 1} {
				if count, err := app.CountRecords(collection); err != nil || int(count) != want {
					t.Fatal("migration changed existing records", collection, count, want, err)
				}
			}
			retained, err := app.FindRecordById("linux_user_sources", source.Id)
			if err != nil || retained.GetString("private_key") != source.GetString("private_key") || retained.GetInt("revision") != 2 {
				t.Fatal("Linux source credentials changed", err)
			}
			if scenario.want != "" {
				versions, err := app.FindRecordsByFilter("cluster_config_versions", "cluster_id={:id}", "revision", 10, 0, map[string]any{"id": cluster.Id})
				if err != nil || len(versions) != 2 {
					t.Fatal("inference did not retain previous and current versions", err, len(versions))
				}
				before, err := confighistory.Decode(versions[0])
				if err != nil || before.AuthenticationMethod != "" {
					t.Fatal("migration invented a historical choice", err, before)
				}
				after, err := confighistory.Decode(versions[1])
				if err != nil || after.AuthenticationMethod != scenario.want {
					t.Fatal("inferred choice missing from history", err, after)
				}
			}
		})
	}
}
