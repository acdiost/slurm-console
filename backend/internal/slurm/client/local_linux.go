//go:build linux

package client

import (
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

func configureLocalIdentity(cmd *exec.Cmd, name string) error {
	account, err := user.Lookup(name)
	if err != nil {
		return err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid == 0 {
		return fmt.Errorf("non-root socket identity required")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil || gid == 0 {
		return fmt.Errorf("non-root socket group required")
	}
	// Use only the peer's primary group. In particular, never inherit root groups.
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{uint32(gid)}}}
	return nil
}
