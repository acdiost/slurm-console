//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package linuxusers

import "os/exec"

func configureProcess(*exec.Cmd) {}

func configureAdminProcess(*exec.Cmd) {}
