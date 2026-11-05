package rbac

import (
	"context"
	"errors"
	"github.com/pocketbase/pocketbase/core"
	"slices"
	"slurm-console/backend/internal/freeipa"
	"time"
)

// Effective directory roles are read live and never written into manual grants.
// Both user and group UUIDs must still match; a recreated name inherits nothing.
func directoryRoles(app core.App, user *core.Record, result *Access, clusters map[string]*ClusterAccess) error {
	if user.GetString("auth_source") != "freeipa" {
		return nil
	}
	mappings, e := app.FindRecordsByFilter("directory_role_mappings", "", "", 101, 0)
	if e != nil {
		return e
	}
	if len(mappings) > 100 {
		return errors.New("directory mapping limit exceeded")
	}
	if len(mappings) == 0 {
		return nil
	}
	p, _ := app.Store().Get(freeipa.StoreKey).(freeipa.Provider)
	if p == nil {
		return freeipa.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	u, e := p.User(ctx, user.GetString("username"))
	if e != nil {
		return e
	}
	if u.ID == "" || u.Username != user.GetString("username") || u.ID != user.GetString("freeipa_entry_id") || !u.Enabled || u.AccountExpired || u.PasswordExpired {
		return freeipa.ErrDenied
	}
	checked := map[string]string{}
	for _, m := range mappings {
		name := m.GetString("group_name")
		if !slices.Contains(u.Groups, name) {
			continue
		}
		id, ok := checked[name]
		if !ok {
			g, err := p.Group(ctx, name)
			if errors.Is(err, freeipa.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			id = g.ID
			checked[name] = id
		}
		if id == "" || id != m.GetString("group_id") {
			continue
		}
		role, err := app.FindRecordById("roles", m.GetString("role"))
		if err != nil {
			return err
		}
		cid := m.GetString("cluster")
		scope := role.GetString("scope")
		if scope == "platform" && cid != "" || scope == "cluster" && cid == "" {
			return errors.New("invalid directory role scope")
		}
		if scope == "platform" {
			permissions, err := rolePermissions(app, role.Id, "")
			if err != nil {
				return err
			}
			result.PlatformRoles = append(result.PlatformRoles, role.GetString("name"))
			result.PlatformPermissions = append(result.PlatformPermissions, permissions...)
			continue
		}
		entry := clusters[cid]
		if entry == nil {
			r, err := app.FindRecordById("clusters", cid)
			if err != nil {
				return err
			}
			entry = &ClusterAccess{Cluster: ClusterDTO(r), Roles: []string{}, Permissions: []string{}}
			clusters[cid] = entry
		}
		permissions, err := rolePermissions(app, role.Id, "cluster")
		if err != nil {
			return err
		}
		entry.Roles = append(entry.Roles, role.GetString("name"))
		entry.Permissions = append(entry.Permissions, permissions...)
	}
	return nil
}
