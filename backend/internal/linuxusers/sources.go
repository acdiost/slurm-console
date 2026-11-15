package linuxusers

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/rbac"
)

const sourceCollection = "linux_user_sources"
const importCollection = "linux_user_source_import"
const importID = "linuxsources001"

var sourceUsername = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var sourceHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`)
var legacyClusterID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type SourceView struct {
	Configured           bool   `json:"configured"`
	Revision             int    `json:"revision"`
	Label                string `json:"label"`
	Transport            string `json:"transport"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	Username             string `json:"username"`
	KnownHosts           string `json:"knownHosts"`
	PrivateKeyConfigured bool   `json:"privateKeyConfigured"`
	ManageLocal          bool   `json:"manageLocal"`
}

type SourceInput struct {
	Revision    int    `json:"revision"`
	Label       string `json:"label"`
	Transport   string `json:"transport"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PrivateKey  string `json:"privateKey"`
	KnownHosts  string `json:"knownHosts"`
	ManageLocal bool   `json:"manageLocal"`
}

func sourceRecord(app core.App, cluster string) (*core.Record, error) {
	r, err := app.FindFirstRecordByData(sourceCollection, "cluster", cluster)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}
func sourceRevision(r *core.Record) int {
	if r == nil {
		return 0
	}
	return r.GetInt("revision")
}
func sourceProfile(r *core.Record) (Profile, bool) {
	if r == nil || !r.GetBool("configured") {
		return Profile{}, false
	}
	return Profile{Label: r.GetString("label"), Transport: r.GetString("transport"), Host: r.GetString("host"), Port: r.GetInt("port"), Username: r.GetString("username"), PrivateKey: r.GetString("private_key"), KnownHosts: r.GetString("known_hosts"), ManageLocal: r.GetBool("manage_local")}, true
}
func sourceView(r *core.Record) SourceView {
	v := SourceView{Revision: sourceRevision(r), Transport: "local"}
	p, ok := sourceProfile(r)
	if !ok {
		return v
	}
	return SourceView{Configured: true, Revision: v.Revision, Label: p.Label, Transport: p.Transport, Host: p.Host, Port: p.Port, Username: p.Username, KnownHosts: p.KnownHosts, PrivateKeyConfigured: p.PrivateKey != "", ManageLocal: p.ManageLocal}
}
func setSource(r *core.Record, p Profile, configured bool, revision int) {
	r.Set("configured", configured)
	r.Set("revision", revision)
	r.Set("label", p.Label)
	r.Set("transport", p.Transport)
	r.Set("host", p.Host)
	r.Set("port", p.Port)
	r.Set("username", p.Username)
	r.Set("private_key", p.PrivateKey)
	r.Set("known_hosts", p.KnownHosts)
	r.Set("manage_local", p.ManageLocal)
}
func validSource(p Profile) bool {
	if p.Label == "" || len(p.Label) > 160 || !utf8.ValidString(p.Label) || p.Label != strings.TrimSpace(p.Label) || strings.ContainsFunc(p.Label, unicode.IsControl) {
		return false
	}
	if p.Transport == "local" {
		return p.Host == "" && p.Port == 0 && p.Username == "" && p.PrivateKey == "" && p.KnownHosts == ""
	}
	return p.Transport == "ssh" && !p.ManageLocal && p.Username != "root" && sourceUsername.MatchString(p.Username) && p.Port >= 1 && p.Port <= 65535 && (net.ParseIP(p.Host) != nil || sourceHost.MatchString(p.Host)) && len(p.Host) <= 253 && len(p.PrivateKey) <= 65536 && len(p.KnownHosts) <= 1048576 && strings.TrimSpace(p.PrivateKey) != "" && strings.TrimSpace(p.KnownHosts) != "" && !strings.ContainsRune(p.PrivateKey, 0) && !strings.ContainsRune(p.KnownHosts, 0)
}
func authorizeSource(app core.App, actor, cluster string) (*core.Record, error) {
	a, err := rbac.Resolve(app, actor)
	if err != nil {
		return nil, err
	}
	if !a.Allows("platform.settings.manage", "") || !a.Allows("linux_users.read", cluster) {
		return nil, fail("forbidden")
	}
	c, err := app.FindRecordById("clusters", cluster)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fail("not_found")
	}
	return c, err
}
func (s *Service) GetSource(actor, cluster string) (SourceView, error) {
	if _, err := authorizeSource(s.app, actor, cluster); err != nil {
		return SourceView{}, err
	}
	r, err := sourceRecord(s.app, cluster)
	if err != nil {
		return SourceView{}, err
	}
	return sourceView(r), nil
}
func (s *Service) SaveSource(ctx context.Context, actor, cluster, requestID string, input SourceInput) (SourceView, error) {
	c, err := authorizeSource(s.app, actor, cluster)
	if err != nil {
		return SourceView{}, err
	}
	old, err := sourceRecord(s.app, cluster)
	if err != nil {
		return SourceView{}, err
	}
	if input.Revision < 0 {
		return SourceView{}, fail("invalid_source")
	}
	if sourceRevision(old) != input.Revision {
		return SourceView{}, fail("changed")
	}
	p := Profile{Label: input.Label, Transport: input.Transport, Host: input.Host, Port: input.Port, Username: input.Username, PrivateKey: input.PrivateKey, KnownHosts: input.KnownHosts, ManageLocal: input.ManageLocal}
	if p.Transport == "local" {
		p.Host, p.Username, p.PrivateKey, p.KnownHosts, p.Port = "", "", "", "", 0
	} else if p.Transport == "ssh" && p.PrivateKey == "" {
		if saved, ok := sourceProfile(old); ok && saved.Transport == "ssh" {
			p.PrivateKey = saved.PrivateKey
		}
	}
	if !validSource(p) {
		return SourceView{}, fail("invalid_source")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request := Request{Version: 1}
	result, err := s.provider.Read(probeCtx, p, request)
	if err != nil || probeCtx.Err() != nil || ValidateSnapshot(result, request) != nil {
		return SourceView{}, fail("probe_failed")
	}
	return s.commitSource(ctx, actor, cluster, requestID, input.Revision, c.GetInt("revision"), p, true)
}
func (s *Service) DeleteSource(ctx context.Context, actor, cluster, requestID string, revision int) (SourceView, error) {
	c, err := authorizeSource(s.app, actor, cluster)
	if err != nil {
		return SourceView{}, err
	}
	if revision < 0 {
		return SourceView{}, fail("invalid_source")
	}
	return s.commitSource(ctx, actor, cluster, requestID, revision, c.GetInt("revision"), Profile{Transport: "local"}, false)
}
func (s *Service) commitSource(ctx context.Context, actor, cluster, requestID string, revision, clusterRevision int, p Profile, configured bool) (SourceView, error) {
	// Serialize source commits with the final local-control check and OS execution.
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	var view SourceView
	err := s.app.RunInTransaction(func(tx core.App) error {
		if ctx.Err() != nil {
			return fail("changed")
		}
		c, err := authorizeSource(tx, actor, cluster)
		if err != nil {
			return err
		}
		if c.GetInt("revision") != clusterRevision {
			return fail("changed")
		}
		r, err := sourceRecord(tx, cluster)
		if err != nil {
			return err
		}
		if sourceRevision(r) != revision {
			return fail("changed")
		}
		before := sourceView(r)
		if r == nil {
			collection, err := tx.FindCollectionByNameOrId(sourceCollection)
			if err != nil {
				return err
			}
			r = core.NewRecord(collection)
			r.Set("cluster", cluster)
		}
		setSource(r, p, configured, revision+1)
		if err = tx.Save(r); err != nil {
			return err
		}
		c.Set("revision", clusterRevision+1)
		if err = tx.Save(c); err != nil {
			return err
		}
		// Hooks may revoke authorization or change the source while saving.
		if _, err = authorizeSource(tx, actor, cluster); err != nil {
			return err
		}
		current, err := sourceRecord(tx, cluster)
		if err != nil {
			return err
		}
		if sourceRevision(current) != revision+1 {
			return fail("changed")
		}
		view = sourceView(current)
		user, err := tx.FindRecordById("users", actor)
		if err != nil {
			return err
		}
		action := "linux_user_source.update"
		if !configured {
			action = "linux_user_source.delete"
		}
		// Audit only state and revisions; never profile content or credentials.
		if err = audit.Write(tx, audit.PlatformActor(user, requestID), audit.Event{Action: action, ClusterID: cluster, TargetType: "linux_user_source", TargetID: cluster, Message: "Linux 身份来源配置已更新。", Changes: []audit.Change{{Field: "configured", Before: strconv.FormatBool(before.Configured), After: strconv.FormatBool(configured)}, {Field: "revision", Before: strconv.Itoa(revision), After: strconv.Itoa(revision + 1)}}}); err != nil {
			return err
		}
		if _, err = authorizeSource(tx, actor, cluster); err != nil {
			return err
		}
		current, err = sourceRecord(tx, cluster)
		if err != nil {
			return err
		}
		if sourceRevision(current) != revision+1 {
			return fail("changed")
		}
		latestCluster, err := tx.FindRecordById("clusters", cluster)
		if err != nil || latestCluster.GetInt("revision") != clusterRevision+1 {
			return fail("changed")
		}
		return nil
	})
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) {
			return SourceView{}, failure
		}
		return SourceView{}, fail("unavailable")
	}
	return view, nil
}

func (s *Service) ConfiguredCount() int {
	n, err := s.app.CountRecords(sourceCollection, dbx.HashExp{"configured": true})
	if err != nil {
		return 0
	}
	return int(n)
}

// ImportLegacy runs after migrations. A persisted marker prevents all future file reads,
// including when the environment still refers to a removed legacy JSON/key file.
func ImportLegacy(app core.App, path string) error {
	state, err := app.FindRecordById(importCollection, importID)
	if err != nil {
		return fail("import_failed")
	}
	if state.GetBool("completed") {
		return nil
	}
	legacy := struct {
		Clusters map[string]legacyProfile `json:"clusters"`
	}{Clusters: map[string]legacyProfile{}}
	if path != "" {
		raw, err := restricted(path, 65536, false)
		if err != nil || StrictJSON(raw, &legacy) != nil || legacy.Clusters == nil || len(legacy.Clusters) > 100 {
			return fail("import_failed")
		}
	}
	err = app.RunInTransaction(func(tx core.App) error {
		state, err := tx.FindRecordById(importCollection, importID)
		if err != nil {
			return fail("import_failed")
		}
		if state.GetBool("completed") {
			return nil
		}
		collection, err := tx.FindCollectionByNameOrId(sourceCollection)
		if err != nil {
			return fail("import_failed")
		}
		for id, old := range legacy.Clusters {
			if !legacyClusterID.MatchString(id) {
				return fail("import_failed")
			}
			existing, err := sourceRecord(tx, id)
			if err != nil {
				return fail("import_failed")
			}
			if existing != nil {
				continue
			}
			if _, err = tx.FindRecordById("clusters", id); errors.Is(err, sql.ErrNoRows) {
				continue
			} else if err != nil {
				return fail("import_failed")
			}
			p := Profile{Label: old.Label, Transport: old.Transport, Host: old.Host, Port: old.Port, Username: old.Username, ManageLocal: old.ManageLocal}
			if p.Transport == "ssh" {
				key, err := restricted(old.IdentityFile, 65536, true)
				if err != nil {
					return fail("import_failed")
				}
				p.PrivateKey = string(key)
				clear(key)
				hosts, err := restricted(old.KnownHostsFile, 1048576, false)
				if err != nil {
					return fail("import_failed")
				}
				p.KnownHosts = string(hosts)
			} else if old.IdentityFile != "" || old.KnownHostsFile != "" {
				return fail("import_failed")
			}
			if !validSource(p) {
				return fail("import_failed")
			}
			r := core.NewRecord(collection)
			r.Set("cluster", id)
			setSource(r, p, true, 1)
			if err = tx.Save(r); err != nil {
				return fail("import_failed")
			}
		}
		state.Set("completed", true)
		if err = tx.Save(state); err != nil {
			return fail("import_failed")
		}
		return nil
	})
	if err != nil {
		return fail("import_failed")
	}
	return nil
}
