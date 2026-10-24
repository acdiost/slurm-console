package clusterconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const storeKey = "platform.clusterConnections"

var ErrConflict = errors.New("configuration changed; reload before continuing")
var ErrBusy = errors.New("connection check is already running")
var ErrInvalid = errors.New("invalid cluster connection configuration")
var ErrNotFound = errors.New("cluster configuration not found")

type Input struct {
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	ConnectionRef  string `json:"connectionRef"`
	APIVersion     string `json:"apiVersion"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Revision       int    `json:"revision"`
}
type Configuration struct {
	ID string `json:"id"`
	Input
	Endpoint         string      `json:"endpoint"`
	AuthType         string      `json:"authType"`
	ProfileAvailable bool        `json:"profileAvailable"`
	LastProbe        ProbeResult `json:"lastProbe"`
}
type Manager struct {
	App      core.App
	Registry *Registry
	busy     sync.Map
	slots    chan struct{}
}

func Register(app core.App, registry *Registry) *Manager {
	manager := &Manager{App: app, Registry: registry, slots: make(chan struct{}, 4)}
	app.Store().Set(storeKey, manager)
	return manager
}
func For(app core.App) *Manager {
	manager, _ := app.Store().Get(storeKey).(*Manager)
	if manager == nil {
		return Register(app, &Registry{Profiles: map[string]Profile{}})
	}
	return manager
}

func (m *Manager) View(record *core.Record) Configuration {
	view := Configuration{ID: record.Id, Input: Input{Name: record.GetString("name"), Slug: record.GetString("slug"), Description: record.GetString("description"), ConnectionRef: record.GetString("connection_ref"), APIVersion: record.GetString("api_version"), Enabled: record.GetBool("enabled"), TimeoutSeconds: record.GetInt("timeout_seconds"), Revision: record.GetInt("revision")}, LastProbe: ProbeResult{Status: "not_tested", Message: "尚未检测连接。", Controllers: []Controller{}}}
	if profile, ok := m.Registry.Profiles[view.ConnectionRef]; ok {
		view.ProfileAvailable = true
		view.Endpoint = profile.Endpoint
		view.AuthType = profile.AuthType
	}
	if data := record.GetRaw("last_probe"); data != nil {
		encoded, _ := json.Marshal(data)
		var probe ProbeResult
		if json.Unmarshal(encoded, &probe) == nil && probe.Status != "" {
			view.LastProbe = probe
		}
	}
	if view.LastProbe.Controllers == nil {
		view.LastProbe.Controllers = []Controller{}
	}
	if view.TimeoutSeconds == 0 {
		view.TimeoutSeconds = 5
	}
	return view
}

func (m *Manager) validate(input *Input) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 100 || utf8.RuneCountInString(input.Description) > 1000 || len(input.Slug) > 64 || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(input.Slug) || input.TimeoutSeconds < 1 || input.TimeoutSeconds > 30 {
		return ErrInvalid
	}
	profile, ok := m.Registry.Profiles[input.ConnectionRef]
	if !ok || !slices.Contains(profile.Versions, input.APIVersion) {
		return ErrInvalid
	}
	return nil
}

func (m *Manager) Save(id string, input Input) (Configuration, error) {
	if err := m.validate(&input); err != nil {
		return Configuration{}, err
	}
	if id == "" && input.Revision != 0 || id != "" && input.Revision < 0 {
		return Configuration{}, ErrInvalid
	}
	var saved *core.Record
	err := m.App.RunInTransaction(func(tx core.App) error {
		var record *core.Record
		if id == "" {
			collection, err := tx.FindCollectionByNameOrId("clusters")
			if err != nil {
				return err
			}
			record = core.NewRecord(collection)
		} else {
			var err error
			record, err = tx.FindRecordById("clusters", id)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if record.GetInt("revision") != input.Revision {
				return ErrConflict
			}
			if record.GetString("slug") != input.Slug {
				return ErrInvalid
			}
		}
		if existing, err := tx.FindFirstRecordByData("clusters", "slug", input.Slug); err == nil && existing.Id != record.Id {
			return ErrConflict
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		record.Set("name", input.Name)
		record.Set("slug", input.Slug)
		record.Set("description", input.Description)
		record.Set("connection_ref", input.ConnectionRef)
		record.Set("api_version", input.APIVersion)
		record.Set("enabled", input.Enabled)
		record.Set("timeout_seconds", input.TimeoutSeconds)
		record.Set("revision", input.Revision+1)
		record.Set("last_probe", ProbeResult{Status: "not_tested", Message: "配置已更新，请检测连接。", Controllers: []Controller{}})
		if err := tx.Save(record); err != nil {
			return err
		}
		saved = record
		return nil
	})
	if err != nil {
		return Configuration{}, err
	}
	return m.View(saved), nil
}

func (m *Manager) Test(ctx context.Context, id string, revision int) (Configuration, error) {
	if _, busy := m.busy.LoadOrStore(id, true); busy {
		return Configuration{}, ErrBusy
	}
	defer m.busy.Delete(id)
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	default:
		return Configuration{}, ErrBusy
	}
	record, err := m.App.FindRecordById("clusters", id)
	if errors.Is(err, sql.ErrNoRows) {
		return Configuration{}, ErrNotFound
	}
	if err != nil {
		return Configuration{}, err
	}
	view := m.View(record)
	if view.Revision != revision || !view.Enabled {
		return Configuration{}, ErrConflict
	}
	if checked, err := time.Parse(time.RFC3339Nano, view.LastProbe.CheckedAt); err == nil && time.Since(checked) >= 0 && time.Since(checked) < 5*time.Second {
		return view, nil
	}
	profile, ok := m.Registry.Profiles[view.ConnectionRef]
	probe := ProbeResult{Status: "profile_unavailable", Message: "服务端连接配置不可用，请联系管理员。", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), Controllers: []Controller{}}
	if ok && slices.Contains(profile.Versions, view.APIVersion) && view.TimeoutSeconds >= 1 && view.TimeoutSeconds <= 30 {
		probe = Probe(ctx, profile, view.APIVersion, time.Duration(view.TimeoutSeconds)*time.Second)
	}
	if ctx.Err() != nil {
		return Configuration{}, ctx.Err()
	}
	err = m.App.RunInTransaction(func(tx core.App) error {
		current, err := tx.FindRecordById("clusters", id)
		if err != nil {
			return err
		}
		if current.GetInt("revision") != revision || !current.GetBool("enabled") {
			return ErrConflict
		}
		current.Set("last_probe", probe)
		if err := tx.Save(current); err != nil {
			return err
		}
		record = current
		return nil
	})
	if err != nil {
		return Configuration{}, err
	}
	return m.View(record), nil
}

func (m *Manager) Disable(id string, revision int) (Configuration, error) {
	var saved *core.Record
	err := m.App.RunInTransaction(func(tx core.App) error {
		record, err := tx.FindRecordById("clusters", id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if record.GetInt("revision") != revision {
			return ErrConflict
		}
		record.Set("enabled", false)
		record.Set("revision", revision+1)
		record.Set("last_probe", ProbeResult{Status: "not_tested", Message: "连接已停用。", Controllers: []Controller{}})
		if err := tx.Save(record); err != nil {
			return err
		}
		saved = record
		return nil
	})
	if err != nil {
		return Configuration{}, err
	}
	return m.View(saved), nil
}

func (m *Manager) CheckEnabled(ctx context.Context) {
	var records []*core.Record
	if m.App.RecordQuery("clusters").AndWhere(dbx.HashExp{"enabled": true}).OrderBy("id").Limit(1000).All(&records) != nil {
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		view, err := m.Test(ctx, record.Id, record.GetInt("revision"))
		slog.InfoContext(ctx, "cluster startup check", "actor", "system", "clusterId", record.Id, "revision", record.GetInt("revision"), "status", view.LastProbe.Status, "error", err)
	}
}
