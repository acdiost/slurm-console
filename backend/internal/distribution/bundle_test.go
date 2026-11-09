package distribution

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestInstallationRequiresLinuxRootAndInternalCommandRequiresLock(t *testing.T) {
	called := false
	configure := func(string) error { called = true; return nil }
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		for _, command := range []string{"install", "configure"} {
			handled, err := Dispatch([]string{command}, configure)
			if !handled || err == nil || !strings.Contains(err.Error(), "root") {
				t.Fatal("unprivileged installation accepted", command, err)
			}
		}
	}
	t.Setenv("SLURM_CONSOLE_LOCK_FD", "")
	if handled, err := Dispatch([]string{"configure-data"}, configure); !handled || err == nil || called {
		t.Fatal("internal setup ran outside installer", err)
	}
}
