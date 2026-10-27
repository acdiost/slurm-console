//go:build linux || darwin

package maintenance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedAdministrationAndExclusiveMaintenanceExcludeEachOther(t *testing.T) {
	t.Setenv("SLURM_CONSOLE_LOCK_FD", "")
	path := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Geteuid())
	first, err := acquireAt(path, false, uid)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireAt(path, false, uid)
	if err != nil {
		t.Fatal(err)
	}
	if unlock, err := acquireAt(path, true, uid); err == nil {
		unlock()
		t.Fatal("maintenance bypassed active administrator")
	}
	first()
	second()
	exclusive, err := acquireAt(path, true, uid)
	if err != nil {
		t.Fatal(err)
	}
	defer exclusive()
	if unlock, err := acquireAt(path, false, uid); err == nil {
		unlock()
		t.Fatal("administrator bypassed maintenance")
	}
}
func TestMaintenanceLockRejectsUnsafeFilesAndInheritance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	_ = os.WriteFile(path, nil, 0666)
	_ = os.Chmod(path, 0666)
	if unlock, err := acquireAt(path, true, uint32(os.Geteuid())); err == nil {
		unlock()
		t.Fatal("writable lock accepted")
	}
	_ = os.Chmod(path, 0644)
	link := path + "-symlink"
	_ = os.Symlink(path, link)
	if unlock, err := acquireAt(link, true, uint32(os.Geteuid())); err == nil {
		unlock()
		t.Fatal("symlink lock accepted")
	}
	t.Setenv("SLURM_CONSOLE_LOCK_FD", "not-a-descriptor")
	if _, err := acquireAt(path, false, uint32(os.Geteuid())); err == nil {
		t.Fatal("invalid inherited descriptor")
	}
}
