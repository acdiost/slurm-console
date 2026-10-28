package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeAccount(t *testing.T) {
	for _, tc := range []struct{ unit, want string }{
		{"[Service]\nUser=root\nGroup=root\n", "root"},
		{"[Unit]\nUser=ignored\n[Service]\nUser=slurm-console\nGroup=slurm-console\n", "slurm-console"},
		{"[Service]\nUser=root\nGroup=slurm-console\n", ""},
		{"[Service]\nUser=someone\nGroup=someone\n", ""},
		{"[Service]\nUser=root\nUser=slurm-console\nGroup=slurm-console\n", ""},
		{"[Service]\nExecStart=/bin/true\n", ""},
	} {
		got, err := runtimeAccount(tc.unit)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Errorf("%q: %q %v", tc.unit, got, err)
		}
	}
}
func TestRootRuntimeOwnership(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "etc/systemd/system")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "slurm-console.service"), []byte("[Service]\nUser=root\nGroup=root\n"), 0600); err != nil {
		t.Fatal(err)
	}
	uid, gid, err := runtimeOwnership(root)
	if err != nil || uid != 0 || gid != 0 {
		t.Fatal(uid, gid, err)
	}
}
