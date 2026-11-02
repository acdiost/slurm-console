package confighistory_test

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/confighistory"
	_ "slurm-console/backend/migrations"
)

func TestUpgradeCapturesCurrentBaselineOnceWithoutInventingHistory(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	defer app.ResetBootstrapState()
	if e := app.Bootstrap(); e != nil {
		t.Fatal(e)
	}
	var old core.MigrationsList
	old.Copy(core.SystemMigrations)
	for _, m := range core.AppMigrations.Items() {
		if m.File < "1793611000_config_history.go" {
			old.Add(m)
		}
	}
	if _, e := core.NewMigrationsRunner(app, old).Up(); e != nil {
		t.Fatal(e)
	}
	c, _ := app.FindCollectionByNameOrId("clusters")
	r := core.NewRecord(c)
	r.Set("name", "Existing cluster")
	r.Set("slug", "existing")
	r.Set("description", "Current description")
	r.Set("revision", 7)
	r.Set("timeout_seconds", 5)
	if e := app.Save(r); e != nil {
		t.Fatal(e)
	}
	started := time.Now().UTC().Add(-time.Second)
	if e := app.RunAllMigrations(); e != nil {
		t.Fatal(e)
	}
	if e := app.RunAllMigrations(); e != nil {
		t.Fatal(e)
	}
	rows, e := app.FindRecordsByFilter("cluster_config_versions", "", "", 10, 0)
	if e != nil || len(rows) != 1 {
		t.Fatal(e, len(rows))
	}
	version := confighistory.Metadata(rows[0])
	snap, e := confighistory.Decode(rows[0])
	if e != nil || version.Kind != "baseline" || version.Revision != 7 || version.ActorID != "" || version.OperationID != "" || snap.Description != "Current description" {
		t.Fatal(e, version, snap)
	}
	when, e := time.Parse("2006-01-02 15:04:05.000Z", version.RecordedAt)
	if e != nil || when.Before(started) {
		t.Fatal("backdated baseline", e, when)
	}
	if n, _ := app.CountRecords("operation_audits", nil); n != 0 {
		t.Fatal("migration invented prior audit")
	}
	rows[0].Set("snapshot", map[string]any{"name": "partial", "slug": "partial"})
	if e := app.Save(rows[0]); e != nil {
		t.Fatal(e)
	}
	if _, e := confighistory.Decode(rows[0]); e == nil {
		t.Fatal("partial snapshot fabricated default values")
	}
}

func TestNullSnapshotFieldIsNotReplacedWithAZeroValue(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	defer app.ResetBootstrapState()
	if e := app.Bootstrap(); e != nil {
		t.Fatal(e)
	}
	if e := app.RunAllMigrations(); e != nil {
		t.Fatal(e)
	}
	c, _ := app.FindCollectionByNameOrId("cluster_config_versions")
	r := core.NewRecord(c)
	r.Set("schema_version", 1)
	r.Set("snapshot", map[string]any{"name": "Stored", "slug": "stored", "description": "", "connectionRef": "", "apiVersion": "", "enabled": nil, "timeoutSeconds": 0})
	if _, e := confighistory.Decode(r); e == nil {
		t.Fatal("null boolean silently became false")
	}
}
