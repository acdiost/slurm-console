package linuxusers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

const ReaderPath = "/opt/slurm-console/slurm-console"
const ReaderCommand = "linux-users-reader"

// Profile contains database-backed source settings. Credentials are never serialized.
type Profile struct {
	ManageLocal bool   `json:"manageLocal"`
	Label       string `json:"label"`
	Transport   string `json:"transport"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PrivateKey  string `json:"-"`
	KnownHosts  string `json:"knownHosts"`
}

// Legacy paths are accepted only by the one-time import, never by the API or runtime.
type legacyProfile struct {
	ManageLocal    bool   `json:"manageLocal,omitempty"`
	Label          string `json:"label"`
	Transport      string `json:"transport"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
	Username       string `json:"username,omitempty"`
	IdentityFile   string `json:"identityFile,omitempty"`
	KnownHostsFile string `json:"knownHostsFile,omitempty"`
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

type Process struct{ slots chan struct{} }

func NewProcess() *Process { return &Process{slots: make(chan struct{}, 4)} }
func SSHArgs(p Profile, identityFile, knownHostsFile string) []string {
	return []string{"-F", "/dev/null", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ProxyCommand=none", "-o", "ProxyJump=none", "-o", "ConnectionAttempts=1", "-o", "ConnectTimeout=5", "-o", "GlobalKnownHostsFile=/dev/null", "-o", "UserKnownHostsFile=" + knownHostsFile, "-i", identityFile, "-l", p.Username, "-p", strconv.Itoa(p.Port), "--", p.Host, ReaderPath + " " + ReaderCommand}
}

func writeSSHFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, content)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func sshFiles(profile Profile) (identity, hosts string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "slurm-console-linux-source-")
	if err != nil {
		return "", "", nil, fail("unavailable")
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	identity, hosts = filepath.Join(dir, "identity"), filepath.Join(dir, "known_hosts")
	if err = writeSSHFile(identity, profile.PrivateKey); err == nil {
		err = writeSSHFile(hosts, profile.KnownHosts)
	}
	if err != nil {
		cleanup()
		return "", "", nil, fail("unavailable")
	}
	return identity, hosts, cleanup, nil
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
		identity, hosts, cleanup, err := sshFiles(profile)
		if err != nil {
			return Snapshot{}, err
		}
		defer cleanup()
		cmd = exec.CommandContext(ctx, "/usr/bin/ssh", SSHArgs(profile, identity, hosts)...)
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
