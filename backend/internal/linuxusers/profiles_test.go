package linuxusers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedSSHFilesAndPinnedArguments(t *testing.T) {
	profile := Profile{Label: "Identity host", Transport: "ssh", Host: "192.0.2.8", Port: 22, Username: "inventory", PrivateKey: "fixture-private-key\n", KnownHosts: "fixture-host-key\n"}
	if !validSource(profile) {
		t.Fatal("valid source rejected")
	}
	identity, hosts, cleanup, err := sshFiles(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	for path, content := range map[string]string{identity: profile.PrivateKey, hosts: profile.KnownHosts} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("SSH file is not protected", err)
		}
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil || dir.Mode().Perm() != 0700 {
			t.Fatal("SSH directory is not protected", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != content {
			t.Fatal("SSH content changed", err)
		}
	}
	args := strings.Join(SSHArgs(profile, identity, hosts), " ")
	for _, required := range []string{"StrictHostKeyChecking=yes", "BatchMode=yes", "IdentityAgent=none", "ClearAllForwardings=yes", "ProxyCommand=none", "GlobalKnownHostsFile=/dev/null", "UserKnownHostsFile=" + hosts, "-i " + identity, ReaderPath} {
		if !strings.Contains(args, required) {
			t.Fatal("SSH protection missing", required)
		}
	}
	if strings.Contains(args, profile.PrivateKey) || strings.Contains(args, profile.KnownHosts) {
		t.Fatal("credentials in arguments")
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(identity)); !os.IsNotExist(err) {
		t.Fatal("SSH files survived cleanup", err)
	}
}

func TestLegacyCredentialsRequireProtectedRegularFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(path, []byte("key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := restricted(path, 65536, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := restricted(path, 65536, true); err == nil {
		t.Fatal("public private key accepted")
	}
	if _, err := restricted("relative", 65536, true); err == nil {
		t.Fatal("relative path accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := restricted(link, 65536, false); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestSourceValidationRejectsUnsafeSSHAndLocalManagement(t *testing.T) {
	valid := Profile{Label: "Identity host", Transport: "ssh", Host: "192.0.2.8", Port: 22, Username: "inventory", PrivateKey: "key", KnownHosts: "host"}
	for _, change := range []func(*Profile){
		func(p *Profile) { p.Username = "root" },
		func(p *Profile) { p.Username = "-option" },
		func(p *Profile) { p.Host = "-oProxyCommand=evil" },
		func(p *Profile) { p.Port = 0 },
		func(p *Profile) { p.Port = 65536 },
		func(p *Profile) { p.ManageLocal = true },
		func(p *Profile) { p.PrivateKey = " " },
		func(p *Profile) { p.KnownHosts = "" },
		func(p *Profile) { p.PrivateKey = "key\x00" },
		func(p *Profile) { p.Label = "line\nlabel" },
		func(p *Profile) { p.Transport = "arbitrary" },
		func(p *Profile) { p.Transport = "local" },
	} {
		p := valid
		change(&p)
		if validSource(p) {
			t.Fatal("unsafe source accepted")
		}
	}
}
func TestOutputLimitDoesNotRetainExcess(t *testing.T) {
	b := new(cappedOutput)
	n, err := b.Write([]byte(strings.Repeat("x", MaxBytes+1024)))
	if err != nil || n != MaxBytes+1024 || !b.exceeded || b.Len() != MaxBytes {
		t.Fatal("output bound ineffective")
	}
}

func TestFailedSSHExecutionRemovesCredentialFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewProcess().Read(ctx, Profile{Transport: "ssh", Host: "127.0.0.1", Port: 22, Username: "inventory", PrivateKey: "private-content", KnownHosts: "host-content"}, Request{Version: 1})
	if err == nil {
		t.Fatal("cancelled SSH execution succeeded")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("failed execution left SSH credentials", err)
	}
}
