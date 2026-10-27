package linuxusers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilesRequireProtectedFilesAndPinSSH(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "identity")
	hosts := filepath.Join(dir, "known_hosts")
	config := filepath.Join(dir, "profiles.json")
	if err := os.WriteFile(key, []byte("fixture-private-key"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(hosts, []byte("fixture-host-key"), 0644)
	raw := `{"clusters":{"cluster1":{"label":"Identity host","transport":"ssh","host":"192.0.2.8","port":22,"username":"inventory","identityFile":"` + key + `","knownHostsFile":"` + hosts + `"}}}`
	_ = os.WriteFile(config, []byte(raw), 0640)
	r, err := LoadRegistry(config)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(SSHArgs(r.Clusters["cluster1"]), " ")
	for _, required := range []string{"StrictHostKeyChecking=yes", "BatchMode=yes", "IdentityAgent=none", "ClearAllForwardings=yes", "ProxyCommand=none", ReaderPath} {
		if !strings.Contains(args, required) {
			t.Fatal(args)
		}
	}
	_ = os.Chmod(key, 0644)
	if _, err = LoadRegistry(config); err == nil {
		t.Fatal("public private key permitted")
	}
	_ = os.Chmod(key, 0600)
	for _, bad := range []string{strings.Replace(raw, `"inventory"`, `"root"`, 1), strings.Replace(raw, `"192.0.2.8"`, `"-oProxyCommand=evil"`, 1), strings.Replace(raw, `"port":22`, `"port":0`, 1), strings.Replace(raw, `"transport":"ssh"`, `"transport":"local"`, 1), strings.Replace(raw, `"transport":"ssh"`, `"transport":"arbitrary"`, 1), strings.Replace(raw, `"label":"Identity host"`, `"label":"Identity host","command":"id"`, 1)} {
		_ = os.WriteFile(config, []byte(bad), 0640)
		if _, err = LoadRegistry(config); err == nil {
			t.Fatal("unsafe source permitted")
		}
	}
	_ = os.WriteFile(config, []byte(raw), 0666)
	_ = os.Chmod(config, 0666)
	if _, err = LoadRegistry(config); err == nil {
		t.Fatal("writable profiles accepted")
	}
}
func TestOutputLimitDoesNotRetainExcess(t *testing.T) {
	b := new(cappedOutput)
	n, err := b.Write([]byte(strings.Repeat("x", MaxBytes+1024)))
	if err != nil || n != MaxBytes+1024 || !b.exceeded || b.Len() != MaxBytes {
		t.Fatal("output bound ineffective")
	}
}
