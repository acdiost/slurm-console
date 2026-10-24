package rbac

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

func Commands(app core.App) *cobra.Command {
	root := &cobra.Command{Use: "access", Short: "Manage platform and cluster grants from the trusted administration host"}
	bootstrap := func() error {
		if err := app.Bootstrap(); err != nil {
			return err
		}
		return app.RunAllMigrations()
	}
	for _, action := range []string{"grant", "revoke"} {
		var email, roleCode, clusterKey string
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			defer app.ResetBootstrapState()
			if err := bootstrap(); err != nil {
				return err
			}
			user, err := app.FindAuthRecordByEmail("users", strings.TrimSpace(email))
			if err != nil {
				return fmt.Errorf("find platform user: %w", err)
			}
			clusterID := ""
			if clusterKey != "" {
				cluster, err := app.FindFirstRecordByFilter("clusters", "id = {:key} || slug = {:key}", dbx.Params{"key": clusterKey})
				if err != nil {
					return fmt.Errorf("find cluster inventory: %w", err)
				}
				clusterID = cluster.Id
			}
			if action == "grant" {
				if _, err := Grant(app, user.Id, roleCode, clusterID); err != nil {
					return err
				}
			} else {
				role, err := app.FindFirstRecordByFilter("roles", "code = {:code}", dbx.Params{"code": roleCode})
				if err != nil {
					return err
				}
				name, filter := "user_platform_roles", "user = {:user} && role = {:role}"
				params := dbx.Params{"user": user.Id, "role": role.Id}
				if role.GetString("scope") == "cluster" {
					if clusterID == "" {
						return fmt.Errorf("cluster role requires --cluster")
					}
					name = "user_cluster_roles"
					filter += " && cluster = {:cluster}"
					params["cluster"] = clusterID
				} else if clusterID != "" {
					return fmt.Errorf("platform role cannot be scoped to a cluster")
				}
				record, err := app.FindFirstRecordByFilter(name, filter, params)
				if err != nil {
					return err
				}
				if err := app.Delete(record); err != nil {
					return err
				}
			}
			slog.Info("local access change", "operationId", rand.Text(), "actorUid", os.Geteuid(), "result", "success", "action", action, "userId", user.Id, "role", roleCode, "clusterId", clusterID)
			fmt.Fprintf(cmd.OutOrStdout(), "Access %s completed.\n", action)
			return nil
		}}
		cmd.Flags().StringVar(&email, "email", "", "Platform user email")
		cmd.Flags().StringVar(&roleCode, "role", "", "Role code: super_admin, cluster_admin, operator, viewer")
		cmd.Flags().StringVar(&clusterKey, "cluster", "", "Cluster ID or slug (required for cluster roles)")
		_ = cmd.MarkFlagRequired("email")
		_ = cmd.MarkFlagRequired("role")
		root.AddCommand(cmd)
	}
	var name, slug string
	inventory := &cobra.Command{Use: "register-cluster", Short: "Register non-sensitive cluster metadata for authorization (no connection)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		defer app.ResetBootstrapState()
		if err := bootstrap(); err != nil {
			return err
		}
		collection, err := app.FindCollectionByNameOrId("clusters")
		if err != nil {
			return err
		}
		record := core.NewRecord(collection)
		record.Set("name", strings.TrimSpace(name))
		record.Set("slug", strings.TrimSpace(slug))
		if err := app.Save(record); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Cluster inventory registered:", record.Id)
		return nil
	}}
	inventory.Flags().StringVar(&name, "name", "", "Display name")
	inventory.Flags().StringVar(&slug, "slug", "", "Stable inventory slug")
	_ = inventory.MarkFlagRequired("name")
	_ = inventory.MarkFlagRequired("slug")
	root.AddCommand(inventory)
	return root
}
