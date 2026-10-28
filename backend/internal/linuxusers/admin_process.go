package linuxusers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"
)

type AdminProvider interface {
	Run(context.Context, AdminRequest) (AdminResult, error)
}
type AdminProcess struct{ slots chan struct{} }

func NewAdminProcess() *AdminProcess { return &AdminProcess{slots: make(chan struct{}, 2)} }
func (p *AdminProcess) Run(ctx context.Context, r AdminRequest) (AdminResult, error) {
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		return AdminResult{}, fail("busy")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, _ := json.Marshal(r)
	cmd := exec.CommandContext(ctx, ReaderPath, AdminCommand)
	if os.Geteuid() != 0 {
		// Compatibility for existing installations until their systemd unit is upgraded.
		cmd = exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "--", "/usr/bin/nsenter", "--mount=/proc/1/ns/mnt", "--", ReaderPath, AdminCommand)
	}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	// Termination reaches the account helper, whose own deadline
	// cancels useradd/usermod; browser cancellation never triggers a retry.
	configureAdminProcess(cmd)
	out := new(cappedOutput)
	cmd.Stdout = out
	if err := cmd.Run(); err != nil || out.exceeded || out.Len() > 16384 {
		return AdminResult{}, fail("unavailable")
	}
	var value AdminResult
	if StrictJSON(out.Bytes(), &value) != nil {
		return AdminResult{}, fail("invalid_response")
	}
	return value, nil
}
