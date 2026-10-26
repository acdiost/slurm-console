package jobcontrol

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"slurm-console/backend/internal/audit"
)

type Identity struct {
	ID       string `json:"-"`
	Username string `json:"username"`
	UID      int64  `json:"uid"`
	Scope    string `json:"scope"`
	Updated  string `json:"-"`
}

func identity(app core.App, userID, clusterID string) (Identity, error) {
	record, err := app.FindFirstRecordByFilter("slurm_identities", "user={:user} && cluster={:cluster}", dbx.Params{"user": userID, "cluster": clusterID})
	if err != nil {
		return Identity{}, err
	}
	return Identity{record.Id, record.GetString("username"), record.GetInt64("uid"), record.GetString("scope"), record.GetString("updated")}, nil
}
func LinkIdentity(app core.App, userID, clusterID, username string, uid uint32, scope string) error {
	if uid == 0 || username == "root" || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,62}[$]?$`).MatchString(username) || (scope != "own" && scope != "all") {
		return fmt.Errorf("a non-root canonical Slurm username/UID and scope own|all are required")
	}
	if _, err := app.FindRecordById("users", userID); err != nil {
		return err
	}
	if _, err := app.FindRecordById("clusters", clusterID); err != nil {
		return err
	}
	current, err := app.FindFirstRecordByFilter("slurm_identities", "user={:user} && cluster={:cluster}", dbx.Params{"user": userID, "cluster": clusterID})
	if errors.Is(err, sql.ErrNoRows) {
		collection, findErr := app.FindCollectionByNameOrId("slurm_identities")
		if findErr != nil {
			return findErr
		}
		current = core.NewRecord(collection)
	} else if err != nil {
		return err
	}
	if current.Id != "" && current.GetString("username") == username && current.GetInt64("uid") == int64(uid) && current.GetString("scope") == scope {
		return nil
	}
	current.Set("user", userID)
	current.Set("cluster", clusterID)
	current.Set("username", username)
	current.Set("uid", uid)
	current.Set("scope", scope)
	return app.Save(current)
}
func Command(app core.App) *cobra.Command {
	root := &cobra.Command{Use: "slurm-identity", Short: "Explicitly associate platform users with Slurm identities from the trusted administration host"}
	for _, action := range []string{"link", "unlink"} {
		var email, clusterKey, username, scope string
		var uid uint32
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.Bootstrap(); err != nil {
				return err
			}
			defer app.ResetBootstrapState()
			if err := app.RunAllMigrations(); err != nil {
				return err
			}
			user, err := app.FindAuthRecordByEmail("users", strings.TrimSpace(email))
			if err != nil {
				return fmt.Errorf("platform user not found")
			}
			cluster, err := app.FindFirstRecordByFilter("clusters", "id={:key} || slug={:key}", dbx.Params{"key": clusterKey})
			if err != nil {
				return fmt.Errorf("cluster not found")
			}
			auditActor := audit.LocalActor()
			err = audit.Mutate(app, auditActor, func(tx core.App) (audit.Event, error) {
				previous, previousErr := identity(tx, user.Id, cluster.Id)
				if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
					return audit.Event{}, previousErr
				}
				if action == "link" {
					err = LinkIdentity(tx, user.Id, cluster.Id, username, uid, scope)
				} else {
					var record *core.Record
					record, err = tx.FindFirstRecordByFilter("slurm_identities", "user={:user} && cluster={:cluster}", dbx.Params{"user": user.Id, "cluster": cluster.Id})
					if err == nil {
						err = tx.Delete(record)
					}
				}
				if err != nil {
					return audit.Event{}, err
				}
				changes := []audit.Change{}
				before := map[string]string{}
				if previousErr == nil {
					before = map[string]string{"username": previous.Username, "uid": fmt.Sprint(previous.UID), "scope": previous.Scope}
				}
				after := map[string]string{}
				if action == "link" {
					after = map[string]string{"username": username, "uid": fmt.Sprint(uid), "scope": scope}
				}
				for _, field := range []string{"username", "uid", "scope"} {
					if before[field] != after[field] {
						changes = append(changes, audit.Change{Field: field, Before: before[field], After: after[field]})
					}
				}

				return audit.Event{Action: "identities." + action, ClusterID: cluster.Id, TargetType: "user", TargetID: user.Id, Message: "本地 Slurm 身份关联命令完成。", Changes: changes}, nil
			})
			if err != nil {
				return err
			}
			slog.Info("local Slurm identity change", "requestId", auditActor.RequestID, "actorUid", os.Geteuid(), "action", action, "userId", user.Id, "clusterId", cluster.Id, "scope", scope)
			fmt.Fprintln(cmd.OutOrStdout(), "Slurm identity", action, "completed; platform permissions and upstream Slurm privileges are unchanged.")
			return nil
		}}
		cmd.Flags().StringVar(&email, "email", "", "Platform account email")
		cmd.Flags().StringVar(&clusterKey, "cluster", "", "Cluster ID or slug")
		_ = cmd.MarkFlagRequired("email")
		_ = cmd.MarkFlagRequired("cluster")
		if action == "link" {
			cmd.Flags().StringVar(&username, "username", "", "Canonical Slurm username (not root)")
			cmd.Flags().Uint32Var(&uid, "uid", 0, "UID on the target cluster, verified by the administrator")
			cmd.Flags().StringVar(&scope, "scope", "own", "own: only this identity's jobs; all: explicitly delegate cluster-wide job cancellation, still limited by the fixed REST identity")
			_ = cmd.MarkFlagRequired("username")
			_ = cmd.MarkFlagRequired("uid")
		}
		root.AddCommand(cmd)
	}
	return root
}
