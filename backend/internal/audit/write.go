// Package audit persists selected administrative evidence without credentials.
package audit

import (
	"crypto/rand"
	"fmt"
	"os"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
)

type Actor struct{ ID, Label, Source, RequestID string }
type Change struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}
type Event struct {
	Action, ClusterID, TargetType, TargetID, Message string
	Changes                                          []Change
}

func LocalActor() Actor {
	uid := strconv.Itoa(os.Geteuid())
	return Actor{ID: "local:uid:" + uid, Label: "本地 UID " + uid, Source: "local", RequestID: rand.Text()}
}
func PlatformActor(user *core.Record, requestID string) Actor {
	return Actor{ID: user.Id, Label: user.GetString("name"), Source: "platform", RequestID: requestID}
}

// Stamp snapshots identity labels at intent time; older rows remain readable by ID.
func Stamp(app core.App, record *core.Record, actor Actor, cluster, typeName, target string) error {
	record.Set("actor_id", actor.ID)
	record.Set("actor_label", actor.Label)
	record.Set("actor_source", actor.Source)
	record.Set("request_id", actor.RequestID)
	record.Set("target_type", typeName)
	record.Set("target_id", target)
	record.Set("cluster_id", cluster)
	if cluster != "" {
		c, err := app.FindRecordById("clusters", cluster)
		if err != nil {
			return err
		}
		record.Set("cluster_name", c.GetString("name"))
		record.Set("cluster_slug", c.GetString("slug"))
	}
	return nil
}
func Write(app core.App, actor Actor, event Event) error {
	c, err := app.FindCollectionByNameOrId("operation_audits")
	if err != nil {
		return err
	}
	r := core.NewRecord(c)
	if err := Stamp(app, r, actor, event.ClusterID, event.TargetType, event.TargetID); err != nil {
		return err
	}
	r.Set("operation_key", rand.Text())
	r.Set("action", event.Action)
	r.Set("status", "succeeded")
	r.Set("message", event.Message)
	r.Set("changes", event.Changes)
	r.Set("observed_states", []string{})
	if event.ClusterID != "" {
		cluster, err := app.FindRecordById("clusters", event.ClusterID)
		if err != nil {
			return err
		}
		r.Set("cluster_revision", cluster.GetInt("revision"))
	}
	return app.Save(r)
}

// Mutate commits the platform change and its audit row in one transaction.
func Mutate(app core.App, actor Actor, change func(core.App) (Event, error)) error {
	return app.RunInTransaction(func(tx core.App) error {
		event, err := change(tx)
		if err != nil {
			return err
		}
		if err := Write(tx, actor, event); err != nil {
			return fmt.Errorf("persist audit (change rolled back): %w", err)
		}
		return nil
	})
}
func SaveLocal(app core.App, record *core.Record, event Event) error {
	return Mutate(app, LocalActor(), func(tx core.App) (Event, error) {
		if err := tx.Save(record); err != nil {
			return Event{}, err
		}
		if event.TargetID == "" {
			event.TargetID = record.Id
		}
		if event.TargetType == "cluster" && event.ClusterID == "" {
			event.ClusterID = record.Id
		}
		return event, nil
	})
}
