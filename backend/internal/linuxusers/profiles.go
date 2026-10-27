package linuxusers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const ReaderPath = "/opt/slurm-console/slurm-console"
const ReaderCommand = "linux-users-reader"

type Profile struct {
	Label          string `json:"label"`
	Transport      string `json:"transport"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
	Username       string `json:"username,omitempty"`
	IdentityFile   string `json:"identityFile,omitempty"`
	KnownHostsFile string `json:"knownHostsFile,omitempty"`
}
type Registry struct {
	Clusters map[string]Profile `json:"clusters"`
}

func restricted(path string, limit int64, secret bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fail("unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || secret && info.Mode().Perm()&0077 != 0 {
		return nil, fail("unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, fail("invalid_response")
	}
	return raw, nil
}
func LoadRegistry(path string) (*Registry, error) {
	registry := &Registry{Clusters: map[string]Profile{}}
	if path == "" {
		return registry, nil
	}
	raw, err := restricted(path, 65536, false)
	if err != nil {
		return nil, fail("unavailable")
	}
	if StrictJSON(raw, registry) != nil || registry.Clusters == nil || len(registry.Clusters) > 100 {
		return nil, fail("invalid_response")
	}
	for id, p := range registry.Clusters {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(id) || p.Label == "" || len(p.Label) > 160 || strings.ContainsAny(p.Label, "\r\n\t") {
			return nil, fail("invalid_response")
		}
		switch p.Transport {
		case "local":
			if p.Host != "" || p.Port != 0 || p.Username != "" || p.IdentityFile != "" || p.KnownHostsFile != "" {
				return nil, fail("invalid_response")
			}
		case "ssh":
			if p.Username == "root" || !regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`).MatchString(p.Username) || p.Port < 1 || p.Port > 65535 || (net.ParseIP(p.Host) == nil && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`).MatchString(p.Host)) {
				return nil, fail("invalid_response")
			}
			if key, err := restricted(p.IdentityFile, 65536, true); err != nil {
				return nil, fail("unavailable")
			} else {
				clear(key)
			}
			if _, err := restricted(p.KnownHostsFile, 1048576, false); err != nil {
				return nil, fail("unavailable")
			}
		default:
			return nil, fail("invalid_response")
		}
	}
	return registry, nil
}

type Process struct{ slots chan struct{} }

func NewProcess() *Process { return &Process{slots: make(chan struct{}, 4)} }
func SSHArgs(p Profile) []string {
	return []string{"-F", "/dev/null", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ProxyCommand=none", "-o", "ProxyJump=none", "-o", "ConnectionAttempts=1", "-o", "ConnectTimeout=5", "-o", "GlobalKnownHostsFile=/dev/null", "-o", "UserKnownHostsFile=" + p.KnownHostsFile, "-i", p.IdentityFile, "-l", p.Username, "-p", strconv.Itoa(p.Port), "--", p.Host, ReaderPath + " " + ReaderCommand}
}
func (p *Process) Read(ctx context.Context, profile Profile, request Request) (Snapshot, error) {
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		return Snapshot{}, fail("busy")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	input, _ := json.Marshal(request)
	var cmd *exec.Cmd
	if profile.Transport == "local" {
		if runtime.GOOS != "linux" {
			return Snapshot{}, fail("unavailable")
		}
		cmd = exec.CommandContext(ctx, ReaderPath, ReaderCommand)
	} else if profile.Transport == "ssh" {
		if key, err := restricted(profile.IdentityFile, 65536, true); err != nil {
			return Snapshot{}, fail("unavailable")
		} else {
			clear(key)
		}
		if _, err := restricted(profile.KnownHostsFile, 1048576, false); err != nil {
			return Snapshot{}, fail("unavailable")
		}
		cmd = exec.CommandContext(ctx, "/usr/bin/ssh", SSHArgs(profile)...)
	} else {
		return Snapshot{}, fail("unavailable")
	}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	out := new(cappedOutput)
	configureProcess(cmd)
	cmd.Stdout = out
	if err := cmd.Run(); err != nil || out.exceeded {
		return Snapshot{}, fail("unavailable")
	}
	var result Snapshot
	if StrictJSON(out.Bytes(), &result) != nil {
		return Snapshot{}, fail("invalid_response")
	}
	if err := ValidateSnapshot(result, request); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

type cappedOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *cappedOutput) Write(data []byte) (int, error) {
	size := len(data)
	if b.Len()+size > MaxBytes {
		b.exceeded = true
	}
	if remaining := MaxBytes - b.Len(); remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.Buffer.Write(data)
	}
	return size, nil
}
