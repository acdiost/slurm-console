//go:build !linux

package client

import (
	"fmt"
	"os/exec"
)

func configureLocalIdentity(*exec.Cmd, string) error {
	return fmt.Errorf("local socket identity requires Linux")
}
