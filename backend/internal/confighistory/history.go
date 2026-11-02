// Package confighistory preserves only reviewed platform connection settings.
package confighistory

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var ErrInvalid = errors.New("invalid configuration version")
var ErrConflict = errors.New("configuration version already has different content")

type Snapshot struct {
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	ConnectionRef  string `json:"connectionRef"`
	APIVersion     string `json:"apiVersion"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

func FromRecord(r *core.Record) Snapshot {
	return Snapshot{r.GetString("name"), r.GetString("slug"), r.GetString("description"), r.GetString("connection_ref"), r.GetString("api_version"), r.GetBool("enabled"), r.GetInt("timeout_seconds")}
}
func (s Snapshot) valid() bool {
	return s.Name != "" && s.Slug != "" && utf8.ValidString(s.Name) && utf8.RuneCountInString(s.Name) <= 100 && len(s.Slug) <= 64 && utf8.ValidString(s.Description) && utf8.RuneCountInString(s.Description) <= 1000 && len(s.ConnectionRef) <= 64 && len(s.APIVersion) <= 16 && s.TimeoutSeconds >= 0 && s.TimeoutSeconds <= 30
}
func (s Snapshot) Values() map[string]string {
	return map[string]string{"name": s.Name, "slug": s.Slug, "description": s.Description, "connectionRef": s.ConnectionRef, "apiVersion": s.APIVersion, "enabled": strconv.FormatBool(s.Enabled), "timeoutSeconds": strconv.Itoa(s.TimeoutSeconds)}
}

var Fields = []string{"name", "slug", "description", "connectionRef", "apiVersion", "enabled", "timeoutSeconds"}

type Field struct {
	Field   string  `json:"field"`
	Before  *string `json:"before"`
	After   *string `json:"after"`
	Changed bool    `json:"changed"`
}

func Diff(before *Snapshot, after Snapshot) []Field {
	out := make([]Field, 0, len(Fields))
	a := after.Values()
	for _, k := range Fields {
		v := a[k]
		f := Field{Field: k, After: &v, Changed: true}
		if before != nil {
			old := before.Values()[k]
			f.Before = &old
			f.Changed = old != v
		}
		out = append(out, f)
	}
	return out
}

type Version struct {
	ID            string `json:"id"`
	ClusterID     string `json:"clusterId"`
	Revision      int    `json:"revision"`
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Action        string `json:"action"`
	ActorID       string `json:"actorId"`
	ActorLabel    string `json:"actorLabel"`
	ActorSource   string `json:"actorSource"`
	OperationID   string `json:"operationId"`
	RequestID     string `json:"requestId"`
	RecordedAt    string `json:"recordedAt"`
}

func Metadata(r *core.Record) Version {
	return Version{r.Id, r.GetString("cluster_id"), r.GetInt("revision"), r.GetInt("schema_version"), r.GetString("kind"), r.GetString("action"), r.GetString("actor_id"), r.GetString("actor_label"), r.GetString("actor_source"), r.GetString("operation_id"), r.GetString("request_id"), r.GetString("created")}
}
func Decode(r *core.Record) (Snapshot, error) {
	var s Snapshot
	if r.GetInt("schema_version") != 1 {
		return s, ErrInvalid
	}
	raw, e := json.Marshal(r.GetRaw("snapshot"))
	if e != nil {
		return s, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(Fields) {
		return s, ErrInvalid
	}
	for _, key := range Fields {
		if v, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return s, ErrInvalid
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || !s.valid() {
		return s, ErrInvalid
	}
	return s, nil
}

// Capture must share the caller's transaction with the configuration and audit.
// A baseline has no invented actor or historical timestamp. Text IDs survive deletion.
func Capture(app core.App, cluster *core.Record, evidence *core.Record) error {
	if !app.IsTransactional() {
		return errors.New("configuration version requires transaction")
	}
	snapshot := FromRecord(cluster)
	if !snapshot.valid() {
		return ErrInvalid
	}
	old, err := app.FindFirstRecordByFilter("cluster_config_versions", "cluster_id={:cluster} && revision={:revision}", dbx.Params{"cluster": cluster.Id, "revision": cluster.GetInt("revision")})
	if err == nil {
		stored, e := Decode(old)
		if e != nil {
			return e
		}
		if stored != snapshot {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	collection, err := app.FindCollectionByNameOrId("cluster_config_versions")
	if err != nil {
		return err
	}
	r := core.NewRecord(collection)
	r.Set("cluster_id", cluster.Id)
	r.Set("revision", cluster.GetInt("revision"))
	r.Set("schema_version", 1)
	r.Set("snapshot", snapshot)
	r.Set("kind", "baseline")
	r.Set("action", "baseline")
	if evidence != nil {
		r.Set("kind", "change")
		for dst, src := range map[string]string{"action": "action", "actor_id": "actor_id", "actor_label": "actor_label", "actor_source": "actor_source", "operation_id": "operation_key", "request_id": "request_id"} {
			r.Set(dst, evidence.GetString(src))
		}
	}
	return app.Save(r)
}
