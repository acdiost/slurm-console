//go:build linux

package pamauth

import (
	"os/exec"
	"syscall"
)

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Give sudo a chance to forward termination to the privileged helper.
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
}
