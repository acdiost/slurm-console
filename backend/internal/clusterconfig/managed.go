package clusterconfig

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/slurm/protocol"
)

var ErrSetupUnavailable = errors.New("Slurm connection prerequisites or connection check failed")

type SetupInput struct {
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Endpoint       string `json:"endpoint"`
	AuthType       string `json:"authType"`
	Username       string `json:"username"`
	Token          string `json:"token"`
	CAPEM          string `json:"caPem"`
	APIVersion     string `json:"apiVersion"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// LoadManaged merges persistent web-managed profiles into the environment registry.
// Validation finishes before publication; environment profiles are never replaced.
func LoadManaged(app core.App) error {
	r := For(app).Registry
	r.mutation.Lock()
	defer r.mutation.Unlock()
	rows, err := app.FindAllRecords("managed_cluster_profiles")
	if err != nil {
		return err
	}
	managed := make(map[string]Profile, len(rows))
	for _, row := range rows {
		var p Profile
		data, err := json.Marshal(row.GetRaw("profile"))
		if err != nil || json.Unmarshal(data, &p) != nil {
			return ErrInvalid
		}
		ref := row.GetString("ref")
		if !strings.HasPrefix(ref, "managed-") {
			return ErrInvalid
		}
		for _, path := range []string{p.TokenFile, p.CAFile} {
			if path == "" {
				continue
			}
			rel, err := filepath.Rel(filepath.Join(app.DataDir(), "managed-slurm"), path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return ErrInvalid
			}
		}
		if p.AuthType == "jwt" {
			token, err := readRestrictedFile(p.TokenFile, 8192, true)
			if err != nil || len(token) == 0 || strings.ContainsAny(string(token), "\r\n\x00") {
				return ErrSetupUnavailable
			}
		}
		managed[ref] = p
	}
	validated, err := NewRegistry(managed)
	if err != nil {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	merged := make(map[string]Profile, len(r.profiles)+len(managed))
	for ref, p := range r.profiles {
		if !r.managed[ref] {
			merged[ref] = p
		}
	}
	for ref, p := range validated.profiles {
		if _, exists := merged[ref]; exists {
			return ErrConflict
		}
		merged[ref] = p
	}
	if len(merged) > 100 {
		return ErrInvalid
	}
	r.profiles = merged
	r.managed = make(map[string]bool, len(managed))
	for ref := range managed {
		r.managed[ref] = true
	}
	return nil
}

// Setup validates and probes a provisional connection before committing its
// profile, cluster, history and audit together. Credentials only enter a 0600 file.
func (m *Manager) Setup(ctx context.Context, in SetupInput, actor audit.Actor, fresh func(core.App) error) (Configuration, error) {
	if in.AuthType != "local" && in.AuthType != "jwt" {
		return Configuration{}, ErrInvalid
	}
	if in.AuthType == "local" && (!strings.HasPrefix(in.Endpoint, "unix:///") || in.Username != "" || in.Token != "" || in.CAPEM != "") {
		return Configuration{}, ErrInvalid
	}
	if in.AuthType == "jwt" && (in.Token == "" || len(in.Token) > 8192 || strings.ContainsAny(in.Token, "\r\n\x00")) {
		return Configuration{}, ErrInvalid
	}
	if len(in.CAPEM) > 8192 {
		return Configuration{}, ErrInvalid
	}
	version := in.APIVersion
	if version == "auto" {
		version = protocol.Versions()[0]
	}
	ref := "managed-" + strings.ToLower(rand.Text())
	profile := Profile{Label: strings.TrimSpace(in.Name), Endpoint: in.Endpoint, AuthType: in.AuthType, Username: in.Username}
	// Validate endpoint/identity and cluster fields before touching the filesystem.
	if in.AuthType == "jwt" {
		profile.TokenFile = filepath.Join(m.App.DataDir(), "managed-slurm", "pending.jwt")
	}
	registry, err := NewRegistry(map[string]Profile{ref: profile})
	if err != nil {
		return Configuration{}, ErrInvalid
	}
	input := Input{Name: in.Name, Slug: in.Slug, ConnectionRef: ref, APIVersion: version, Enabled: true, TimeoutSeconds: in.TimeoutSeconds}
	staged := &Manager{App: m.App, Registry: registry}
	if err = staged.validate(&input); err != nil {
		return Configuration{}, err
	}
	if _, err = m.App.FindFirstRecordByData("clusters", "slug", input.Slug); err == nil {
		return Configuration{}, ErrConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Configuration{}, err
	}
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	default:
		return Configuration{}, ErrBusy
	}
	var directory string
	var createdRoot string
	committed := false
	defer func() {
		if !committed && directory != "" {
			_ = os.RemoveAll(directory)
		}
		if !committed && createdRoot != "" {
			_ = os.Remove(createdRoot)
		}
	}()
	if in.AuthType == "jwt" {
		root := filepath.Join(m.App.DataDir(), "managed-slurm")
		if _, statErr := os.Lstat(root); os.IsNotExist(statErr) {
			createdRoot = root
		}
		if err = os.MkdirAll(root, 0700); err != nil {
			return Configuration{}, ErrSetupUnavailable
		}
		info, statErr := os.Lstat(root)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return Configuration{}, ErrSetupUnavailable
		}
		directory, err = os.MkdirTemp(root, "profile-")
		if err != nil {
			return Configuration{}, ErrSetupUnavailable
		}
		profile.TokenFile = filepath.Join(directory, "token")
		if err = os.WriteFile(profile.TokenFile, []byte(in.Token), 0600); err != nil {
			return Configuration{}, ErrSetupUnavailable
		}
		if in.CAPEM != "" {
			profile.CAFile = filepath.Join(directory, "ca.pem")
			if err = os.WriteFile(profile.CAFile, []byte(in.CAPEM), 0600); err != nil {
				return Configuration{}, ErrSetupUnavailable
			}
		}
		registry, err = NewRegistry(map[string]Profile{ref: profile})
		if err != nil {
			return Configuration{}, ErrInvalid
		}
		staged.Registry = registry
	}
	profile, _ = registry.Get(ref)
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	if in.APIVersion == "auto" {
		discovered, err := profile.Discover(ctx, timeout)
		if err != nil {
			return Configuration{}, ErrSetupUnavailable
		}
		input.APIVersion = discovered.Recommended
	}
	probe := Probe(ctx, profile, input.APIVersion, timeout)
	if probe.Status != "connected" || ctx.Err() != nil {
		return Configuration{}, ErrSetupUnavailable
	}
	if in.Token != "" {
		encoded, _ := json.Marshal(probe)
		if strings.Contains(string(encoded), in.Token) {
			return Configuration{}, ErrSetupUnavailable
		}
	}
	// Serialize publishers without holding the read lock during database work.
	m.Registry.mutation.Lock()
	defer m.Registry.mutation.Unlock()
	m.Registry.mu.RLock()
	full := len(m.Registry.profiles) >= 100
	_, exists := m.Registry.profiles[ref]
	m.Registry.mu.RUnlock()
	if exists {
		return Configuration{}, ErrConflict
	}
	if full {
		return Configuration{}, ErrInvalid
	}
	var view Configuration
	err = audit.Mutate(m.App, actor, func(tx core.App) (audit.Event, error) {
		if ctx.Err() != nil {
			return audit.Event{}, ctx.Err()
		}
		if err := fresh(tx); err != nil {
			return audit.Event{}, err
		}
		c, err := tx.FindCollectionByNameOrId("managed_cluster_profiles")
		if err != nil {
			return audit.Event{}, err
		}
		r := core.NewRecord(c)
		r.Set("ref", ref)
		r.Set("profile", profile)
		if err = tx.Save(r); err != nil {
			return audit.Event{}, err
		}
		writer := &Manager{App: tx, Registry: registry}
		view, err = writer.Save("", input)
		if err != nil {
			return audit.Event{}, err
		}
		cluster, err := tx.FindRecordById("clusters", view.ID)
		if err != nil {
			return audit.Event{}, err
		}
		cluster.Set("last_probe", probe)
		if err = tx.Save(cluster); err != nil {
			return audit.Event{}, err
		}
		view = writer.View(cluster)
		return audit.Event{Action: "clusters.create", ClusterID: view.ID, TargetType: "cluster", TargetID: view.ID, Message: "集群连接已验证并保存。", Changes: []audit.Change{{Field: "name", After: view.Name}, {Field: "slug", After: view.Slug}, {Field: "connectionRef", After: ref}, {Field: "apiVersion", After: view.APIVersion}, {Field: "enabled", After: "true"}, {Field: "revision", After: "1"}}}, nil
	})
	if err != nil {
		return Configuration{}, err
	}
	m.Registry.mu.Lock()
	if m.Registry.profiles == nil {
		m.Registry.profiles = map[string]Profile{}
	}
	if m.Registry.managed == nil {
		m.Registry.managed = map[string]bool{}
	}
	m.Registry.profiles[ref] = profile
	m.Registry.managed[ref] = true
	m.Registry.mu.Unlock()
	committed = true
	return view, nil
}
