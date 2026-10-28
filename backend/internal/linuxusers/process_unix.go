//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package linuxusers

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// The local reader and its getent child share a process group. A canceled HTTP
// request must not leave a blocked NSS child consuming resources indefinitely.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

func configureAdminProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
}
