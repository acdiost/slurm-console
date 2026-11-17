package migrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"slurm-console/backend/internal/confighistory"
)

// A cluster uses one identity provider. An empty value is retained only when
// legacy grants disagree, so an administrator must review the choice.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("clusters")
		if err != nil {
			return err
		}
		collection.Fields.Add(&core.SelectField{Name: "authentication_method", MaxSelect: 1, Values: []string{"pam", "ldap", "freeipa"}})
		if err = app.Save(collection); err != nil {
			return err
		}
		users, err := app.FindAllRecords("users")
		if err != nil {
			return err
		}
		sources := make(map[string]string, len(users))
		for _, user := range users {
			source := user.GetString("auth_source")
			if source == "pam" || source == "ldap" || source == "freeipa" {
				sources[user.Id] = source
			}
		}
		platformGrants, err := app.FindAllRecords("user_platform_roles")
		if err != nil {
			return err
		}
		platformSources := map[string]bool{}
		for _, grant := range platformGrants {
			if source := sources[grant.GetString("user")]; source != "" {
				platformSources[source] = true
			}
		}
		clusterGrants, err := app.FindAllRecords("user_cluster_roles")
		if err != nil {
			return err
		}
		clusterSources := map[string]map[string]bool{}
		for _, grant := range clusterGrants {
			if source := sources[grant.GetString("user")]; source != "" {
				id := grant.GetString("cluster")
				if clusterSources[id] == nil {
					clusterSources[id] = map[string]bool{}
				}
				clusterSources[id][source] = true
			}
		}
		directoryMappings, err := app.FindAllRecords("directory_role_mappings")
		if err != nil {
			return err
		}
		for _, mapping := range directoryMappings {
			id := mapping.GetString("cluster")
			if id == "" {
				platformSources["freeipa"] = true
				continue
			}
			if clusterSources[id] == nil {
				clusterSources[id] = map[string]bool{}
			}
			clusterSources[id]["freeipa"] = true
		}
		clusters, err := app.FindAllRecords("clusters")
		if err != nil {
			return err
		}
		for _, cluster := range clusters {
			evidence := clusterSources[cluster.Id]
			if len(evidence) == 0 {
				evidence = platformSources
			}
			method := "pam"
			if len(evidence) > 1 {
				continue
			}
			for source := range evidence {
				method = source
			}
			// Preserve the previous revision before recording the inferred choice.
			if err = confighistory.Capture(app, cluster, nil); err != nil {
				return err
			}
			cluster.Set("authentication_method", method)
			cluster.Set("revision", cluster.GetInt("revision")+1)
			if err = app.Save(cluster); err != nil {
				return err
			}
			if err = confighistory.Capture(app, cluster, nil); err != nil {
				return err
			}
		}
		return nil
	}, func(core.App) error {
		return errors.New("cluster authentication choices and history must be retained; restore a consistent backup")
	})
}
