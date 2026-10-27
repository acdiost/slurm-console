package backup

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type replacement struct {
	target, new, old, container string
	installed                   bool
}
type Installation struct{ items []replacement }

func (change *Installation) Rollback() error {
	var problems []string
	for i := len(change.items) - 1; i >= 0; i-- {
		item := change.items[i]
		if item.installed {
			if err := os.RemoveAll(item.target); err != nil {
				problems = append(problems, err.Error())
				continue
			}
		}
		if item.old != "" {
			if err := os.Rename(item.old, item.target); err != nil {
				problems = append(problems, err.Error())
			}
		}
		if item.new != "" {
			_ = os.RemoveAll(item.new)
		}
		if item.container != "" {
			_ = os.RemoveAll(item.container)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("restore rollback needs administrator attention: %s", strings.Join(problems, "; "))
	}
	return nil
}
func (change *Installation) Commit() error {
	for _, item := range change.items {
		if item.old != "" {
			if err := os.RemoveAll(item.old); err != nil {
				return fmt.Errorf("restore succeeded but old files require cleanup: %s", item.old)
			}
		}
	}
	return nil
}
func safeParent(parent string) error {
	ancestor := parent
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		ancestor = filepath.Dir(ancestor)
	}
	if err := checkParents(ancestor); err != nil {
		return err
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	return checkParents(parent)
}
func normalize(name string, mode int64, directory bool, uid, gid int) (os.FileMode, int, int) {
	owner, group := os.Geteuid(), os.Getegid()
	permissions := os.FileMode(0644)
	if directory {
		permissions = 0755
	}
	if strings.HasPrefix(name, "var/lib/slurm-console/pb_data") {
		owner, group = uid, gid
		permissions = 0600
		if directory {
			permissions = 0700
		}
	} else if strings.HasPrefix(name, "etc/slurm-console") {
		group = gid
		permissions = os.FileMode(mode) & 0640
		if directory {
			permissions = 0750
		}
	} else if name == "etc/sudoers.d/slurm-console-pam" {
		permissions = 0440
	} else if name == "opt/slurm-console/slurm-console" || name == "usr/local/libexec/slurm-console-pam-helper" {
		permissions = 0755
	}
	return permissions, owner, group
}

// Prepare all replacement trees before any rename; original paths remain next to
// their replacements until the caller confirms the restored service is healthy.
func Apply(root, stage string, m Manifest, uid, gid int) (change *Installation, resultErr error) {
	change = &Installation{}
	if m.Purpose != "snapshot" {
		return change, fmt.Errorf("only verified snapshots may be restored")
	}
	if err := validateManifest(m); err != nil {
		return change, err
	}
	defer func() {
		if resultErr != nil {
			if err := change.Rollback(); err != nil {
				resultErr = fmt.Errorf("%w; %v", resultErr, err)
			}
		}
	}()
	for _, g := range Groups {
		target := filepath.Join(root, g.Path)
		if err := safeParent(filepath.Dir(target)); err != nil {
			return change, err
		}
		if info, err := os.Lstat(target); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return change, fmt.Errorf("refusing symlink restore target: %s", target)
			}
		} else if !os.IsNotExist(err) {
			return change, err
		}
		entries := []Entry{}
		for _, e := range m.Entries {
			if e.Path == g.Path || g.Dir && strings.HasPrefix(e.Path, g.Path+"/") {
				entries = append(entries, e)
			}
		}
		item := replacement{target: target}
		if len(entries) > 0 {
			temp, err := os.MkdirTemp(filepath.Dir(target), ".slurm-console-new-")
			if err != nil {
				return change, err
			}
			item.new = temp
			if !g.Dir {
				item.new = filepath.Join(temp, "file")
				item.container = temp
			}
			// Keep cleanup ownership even if preparing this group fails midway.
			change.items = append(change.items, item)
			for _, e := range entries {
				relative := ""
				if g.Dir {
					relative = strings.TrimPrefix(e.Path, g.Path+"/")
				}
				destination := item.new
				if relative != "" {
					destination = filepath.Join(item.new, filepath.FromSlash(relative))
				}
				if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
					return change, err
				}
				input, err := os.Open(filepath.Join(stage, filepath.FromSlash(e.Path)))
				if err != nil {
					return change, err
				}
				output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					input.Close()
					return change, err
				}
				_, err = io.Copy(output, input)
				input.Close()
				if err == nil {
					err = output.Sync()
				}
				closeErr := output.Close()
				if err != nil {
					return change, err
				}
				if closeErr != nil {
					return change, closeErr
				}
				permissions, owner, group := normalize(e.Path, e.Mode, false, uid, gid)
				if err := os.Chown(destination, owner, group); err != nil {
					return change, err
				}
				if err := os.Chmod(destination, permissions); err != nil {
					return change, err
				}
			}
			if g.Dir {
				if err := filepath.WalkDir(item.new, func(p string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() {
						return nil
					}
					permissions, owner, group := normalize(g.Path, 0750, true, uid, gid)
					if err := os.Chown(p, owner, group); err != nil {
						return err
					}
					return os.Chmod(p, permissions)
				}); err != nil {
					return change, err
				}
			}
		} else {
			change.items = append(change.items, item)
		}
	}
	for i := range change.items {
		item := &change.items[i]
		if _, err := os.Lstat(item.target); err == nil {
			hold, err := os.MkdirTemp(filepath.Dir(item.target), ".slurm-console-old-")
			if err != nil {
				return change, err
			}
			if err := os.Remove(hold); err != nil {
				return change, err
			}
			if err := os.Rename(item.target, hold); err != nil {
				return change, err
			}
			item.old = hold
		} else if !os.IsNotExist(err) {
			return change, err
		}
		if item.new != "" {
			if err := os.Rename(item.new, item.target); err != nil {
				return change, err
			}
			item.installed = true
			if item.container != "" {
				_ = os.Remove(item.container)
				item.container = ""
			}
			item.new = ""
		}
	}
	return change, nil
}
