package pamauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

type Provider interface {
	Run(context.Context, Request) (Result, error)
}
type Process struct {
	Path    string
	Sudo    bool
	Timeout time.Duration
	slots   chan struct{}
}

func NewProcess(path string, sudo bool) (*Process, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("PAM helper must use an absolute clean path")
	}
	return &Process{Path: path, Sudo: sudo, Timeout: 9 * time.Second, slots: make(chan struct{}, 4)}, nil
}

func (p *Process) Run(ctx context.Context, request Request) (Result, error) {
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		return Result{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	input, err := json.Marshal(request)
	if err != nil {
		return Result{}, ErrUnavailable
	}
	defer clear(input)
	command := exec.CommandContext(ctx, p.Path)
	if p.Sudo {
		command = exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "--", p.Path)
	}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdin = bytes.NewReader(input)
	command.Stderr = io.Discard
	output := &cappedOutput{}
	command.Stdout = output
	command.WaitDelay = time.Second
	configureProcess(command)
	if err := command.Run(); err != nil {
		return Result{}, ErrUnavailable
	}
	var result Result
	if output.exceeded || json.Unmarshal(output.Bytes(), &result) != nil {
		return Result{}, ErrUnavailable
	}
	if !result.OK {
		if result.Code == "denied" {
			return Result{}, ErrDenied
		}
		return Result{}, ErrUnavailable
	}
	return result, nil
}

type cappedOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *cappedOutput) Write(data []byte) (int, error) {
	size := len(data)
	if b.Len()+size > 4096 {
		b.exceeded = true
	}
	if b.Len() < 4096 {
		remaining := 4096 - b.Len()
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.Buffer.Write(data)
	}
	return size, nil
}
