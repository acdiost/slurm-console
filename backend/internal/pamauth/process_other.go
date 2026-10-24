//go:build !linux

package pamauth

import "os/exec"

func configureProcess(command *exec.Cmd) {}
