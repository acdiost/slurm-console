package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/audit"
	_ "slurm-console/backend/migrations"
)

func tempRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func fixture(t *testing.T) string {
	t.Helper()
	root := tempRoot(t)
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: filepath.Join(root, "var/lib/slurm-console/pb_data")})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	c, _ := app.FindCollectionByNameOrId("users")
	r := core.NewRecord(c)
	r.SetEmail("recovery@example.invalid")
	r.SetPassword("private-recovery-password-123")
	r.Set("active", true)
	r.Set("auth_source", "local")
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	if err := audit.Write(app, audit.LocalActor(), audit.Event{Action: "users.create", TargetType: "user", TargetID: r.Id, Message: "recovery fixture"}); err != nil {
		t.Fatal(err)
	}
	if err := app.ResetBootstrapState(); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"opt/slurm-console/slurm-console": "fixture executable", "opt/slurm-console/public/index.html": "fixture release", "etc/slurm-console/slurm-console.env": "PRIVATE-CONFIG=credential-value", "etc/slurm-console/credentials.txt": "PRIVATE-BIND-SECRET", "etc/systemd/system/slurm-console.service": "[Service]\nUser=slurm-console\n", "etc/slurm/slurm.conf": "EXTERNAL-SLURM-MUST-BE-UNTOUCHED"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func key(t *testing.T) *age.HybridIdentity {
	t.Helper()
	k, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func snapshotFixture(t *testing.T) (string, Manifest, *age.HybridIdentity) {
	t.Helper()
	root := fixture(t)
	identity := key(t)
	file := filepath.Join(tempRoot(t), "snapshot.tar.gz.age")
	m, err := Create(root, file, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	return file, m, identity
}
func TestEncryptedSnapshotRestoresAccountsAuditAndMatchingRelease(t *testing.T) {
	filename, m, identity := snapshotFixture(t)
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("PRIVATE-BIND-SECRET")) || bytes.Contains(raw, []byte("private-recovery-password")) {
		t.Fatal("unencrypted backup contents")
	}
	destination := filepath.Join(tempRoot(t), "restore")
	actual, err := Extract(filename, destination, identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual.Entries) != len(m.Entries) {
		t.Fatal("manifest changed")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(destination, "var/lib/slurm-console/pb_data/data.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var users, audits int
	if err := db.QueryRow("SELECT count(*) FROM users WHERE email='recovery@example.invalid'").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM operation_audits").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if users != 1 || audits != 1 {
		t.Fatal("account or audit evidence lost")
	}
	data, _ := os.ReadFile(filepath.Join(destination, "opt/slurm-console/public/index.html"))
	if string(data) != "fixture release" {
		t.Fatal("release not restored")
	}
	if _, err := os.Stat(filepath.Join(destination, "etc/slurm/slurm.conf")); !os.IsNotExist(err) {
		t.Fatal("external service included")
	}
	if _, err := Verify(filename, identity); err != nil {
		t.Fatal(err)
	}
}
func TestBackupRejectsWrongIdentityTruncationCorruptionAndExistingDestination(t *testing.T) {
	file, _, identity := snapshotFixture(t)
	if _, err := Verify(file, key(t)); err == nil {
		t.Fatal("wrong identity accepted")
	}
	data, _ := os.ReadFile(file)
	for label, raw := range map[string][]byte{"truncated": data[:len(data)-8], "corrupt": append([]byte(nil), data...)} {
		if label == "corrupt" {
			raw[len(raw)/2] ^= 1
		}
		path := filepath.Join(tempRoot(t), label)
		_ = os.WriteFile(path, raw, 0600)
		destination := filepath.Join(tempRoot(t), "stage")
		if _, err := Extract(path, destination, identity); err == nil {
			t.Fatal(label)
		}
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatal("failed extraction retained decrypted data")
		}
	}
	existing := tempRoot(t)
	if _, err := Extract(file, existing, identity); err == nil {
		t.Fatal("existing restore tree overwritten")
	}
	root := fixture(t)
	if _, err := Create(root, file, identity.Recipient()); err == nil {
		t.Fatal("existing backup overwritten")
	}
}
func TestBackupRejectsUnsafeSourceAndInvalidDatabase(t *testing.T) {
	root := fixture(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "etc/slurm-console/escape")); err != nil {
		t.Fatal(err)
	}
	identity := key(t)
	file := filepath.Join(tempRoot(t), "test.age")
	if _, err := Create(root, file, identity.Recipient()); err == nil {
		t.Fatal("symlink source accepted")
	}
	_ = os.Remove(filepath.Join(root, "etc/slurm-console/escape"))
	_ = os.WriteFile(filepath.Join(root, "var/lib/slurm-console/pb_data/data.db"), []byte("invalid SQLite"), 0600)
	if _, err := Create(root, file, identity.Recipient()); err == nil {
		t.Fatal("corrupt database accepted")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("invalid backup published")
	}
}
func writeArchive(t *testing.T, m Manifest, identity *age.HybridIdentity, headers []*tar.Header, payloads [][]byte) string {
	t.Helper()
	var out bytes.Buffer
	encrypted, err := age.Encrypt(&out, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(encrypted)
	tw := tar.NewWriter(gz)
	raw, _ := json.Marshal(m)
	_ = tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(raw))})
	_, _ = tw.Write(raw)
	for i, h := range headers {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if len(payloads) > i {
			_, _ = tw.Write(payloads[i])
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	_ = encrypted.Close()
	p := filepath.Join(tempRoot(t), "malicious.age")
	_ = os.WriteFile(p, out.Bytes(), 0600)
	return p
}
func TestRestoreRejectsTraversalLinksMissingFilesAndHashMismatch(t *testing.T) {
	_, m, identity := snapshotFixture(t)
	cases := []struct {
		name    string
		header  *tar.Header
		payload []byte
	}{
		{"traversal", &tar.Header{Name: "../../escape", Typeflag: tar.TypeReg, Size: 1}, []byte("x")},
		{"symlink", &tar.Header{Name: m.Entries[0].Path, Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"}, nil},
		{"wrong-hash", &tar.Header{Name: m.Entries[0].Path, Typeflag: tar.TypeReg, Size: m.Entries[0].Size}, bytes.Repeat([]byte("x"), int(m.Entries[0].Size))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := writeArchive(t, m, identity, []*tar.Header{c.header}, [][]byte{c.payload})
			if _, err := Verify(file, identity); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	file := writeArchive(t, m, identity, nil, nil)
	if _, err := Verify(file, identity); err == nil {
		t.Fatal("missing payload accepted")
	}
	duplicate := m
	duplicate.Entries = append(append([]Entry(nil), m.Entries...), m.Entries[0])
	if err := validateManifest(duplicate); err == nil {
		t.Fatal("duplicate manifest accepted")
	}
	for _, path := range []string{"../x", "/etc/shadow", "etc/slurm-console/../passwd", "etc/slurm-console\\escape"} {
		if allowed(path) {
			t.Fatal(path)
		}
	}
}
func TestApplyRollbackAndCommitPreserveOriginalsAndExternalServices(t *testing.T) {
	file, m, identity := snapshotFixture(t)
	stage := filepath.Join(tempRoot(t), "stage")
	if _, err := Extract(file, stage, identity); err != nil {
		t.Fatal(err)
	}
	target := fixture(t)
	old := []byte("previous release")
	binary := filepath.Join(target, "opt/slurm-console/slurm-console")
	_ = os.WriteFile(binary, old, 0600)
	helper := filepath.Join(target, "usr/local/libexec/slurm-console-pam-helper")
	_ = os.MkdirAll(filepath.Dir(helper), 0700)
	_ = os.WriteFile(helper, []byte("previous helper"), 0700)
	change, err := Apply(target, stage, m, os.Geteuid(), os.Getegid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(helper); !os.IsNotExist(err) {
		t.Fatal("absent optional helper retained")
	}
	if err := change.Rollback(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(binary)
	if string(data) != string(old) {
		t.Fatal("rollback lost executable")
	}
	if _, err := os.Stat(helper); err != nil {
		t.Fatal("rollback lost optional helper")
	}
	change, err = Apply(target, stage, m, os.Geteuid(), os.Getegid())
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Commit(); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(binary)
	if string(data) != "fixture executable" {
		t.Fatal("matching release not applied")
	}
	data, _ = os.ReadFile(filepath.Join(target, "etc/slurm/slurm.conf"))
	if string(data) != "EXTERNAL-SLURM-MUST-BE-UNTOUCHED" {
		t.Fatal("external service changed")
	}
	info, _ := os.Stat(filepath.Join(target, "var/lib/slurm-console/pb_data/data.db"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
}
func TestApplyPreparationFailureLeavesInstallationIntact(t *testing.T) {
	file, m, identity := snapshotFixture(t)
	stage := filepath.Join(tempRoot(t), "stage")
	_, err := Extract(file, stage, identity)
	if err != nil {
		t.Fatal(err)
	}
	target := fixture(t)
	_ = os.Remove(filepath.Join(stage, "opt/slurm-console/public/index.html"))
	if _, err := Apply(target, stage, m, os.Geteuid(), os.Getegid()); err == nil {
		t.Fatal("missing staged file accepted")
	}
	data, _ := os.ReadFile(filepath.Join(target, "opt/slurm-console/public/index.html"))
	if string(data) != "fixture release" {
		t.Fatal("preparation failure changed live files")
	}
}
func TestSecondaryCopyIsExactAndNeverOverwrites(t *testing.T) {
	file, _, _ := snapshotFixture(t)
	dir := tempRoot(t)
	if err := CopyArchive(file, dir); err != nil {
		t.Fatal(err)
	}
	a, _, _ := digestFile(file)
	b, _, _ := digestFile(filepath.Join(dir, filepath.Base(file)))
	if a != b {
		t.Fatal("copy changed")
	}
	if err := CopyArchive(file, dir); err == nil {
		t.Fatal("secondary archive overwritten")
	}
}
func TestReadinessRejectsWrongBodiesAndRedirects(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":{"status":"ok"}}`) }))
	defer good.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Health(ctx, good.URL); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"data":{"status":"failed"}}`, `<html>not the API</html>`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		err := Health(ctx, server.URL)
		cancel()
		server.Close()
		if err == nil {
			t.Fatal("false readiness")
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, good.URL, 302) }))
	defer redirect.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if err := Health(ctx2, redirect.URL); err == nil {
		t.Fatal("redirect accepted")
	}
}
func TestIdentityLocationAndPermissionsAreEnforced(t *testing.T) {
	path := filepath.Join(tempRoot(t), "identity")
	identity := key(t)
	_ = os.WriteFile(path, []byte(identity.String()), 0644)
	if _, _, err := readKey(path); err == nil {
		t.Fatal("world-readable key accepted")
	}
	_ = os.Chmod(path, 0600)
	if _, _, err := readKey(path); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/etc/slurm-console/identity.txt", "/var/lib/slurm-console/pb_data/key"} {
		if _, _, err := readKey(p); err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatal("key inside snapshot accepted")
		}
	}
}

func TestCorruptInstallationCanBePreservedWithoutBeingRestorable(t *testing.T) {
	root := fixture(t)
	_ = os.WriteFile(filepath.Join(root, "var/lib/slurm-console/pb_data/data.db"), []byte("corrupt original database"), 0600)
	identity := key(t)
	filename := filepath.Join(tempRoot(t), "before-restore.age")
	m, err := Preserve(root, filename, identity.Recipient())
	if err != nil || m.Purpose != "preservation" {
		t.Fatal(err)
	}
	verified, err := Verify(filename, identity)
	if err != nil || verified.Purpose != "preservation" {
		t.Fatal(err)
	}
	if _, err := Extract(filename, filepath.Join(tempRoot(t), "unsafe-restore"), identity); err == nil {
		t.Fatal("diagnostic preservation treated as valid recovery snapshot")
	}
}

func TestSnapshotIncludesUncheckpointedWALWithoutRootSideEffects(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, "var/lib/slurm-console/pb_data/data.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE wal_recovery_fixture(value TEXT); INSERT INTO wal_recovery_fixture VALUES ('wal-only-record')"); err != nil {
		t.Fatal(err)
	}
	identity := key(t)
	filename := filepath.Join(tempRoot(t), "wal.age")
	m, err := Create(root, filename, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	hasWAL := false
	for _, e := range m.Entries {
		if strings.HasSuffix(e.Path, "data.db-wal") {
			hasWAL = true
		}
		if strings.HasSuffix(e.Path, ".db-shm") {
			t.Fatal("transient shared memory included")
		}
	}
	if !hasWAL {
		t.Fatal("committed WAL omitted")
	}
	stage := filepath.Join(tempRoot(t), "stage")
	if _, err := Extract(filename, stage, identity); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", filepath.Join(stage, "var/lib/slurm-console/pb_data/data.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow("SELECT value FROM wal_recovery_fixture").Scan(&value); err != nil || value != "wal-only-record" {
		t.Fatalf("WAL data lost: %v %s", err, value)
	}
}
func TestDailyRetentionAuthenticatesAndPreservesOtherBackups(t *testing.T) {
	file, _, identity := snapshotFixture(t)
	dir := tempRoot(t)
	raw, _ := os.ReadFile(file)
	for _, name := range []string{"daily-20261020T000000Z-AAAAAAAAAA.tar.gz.age", "daily-20261021T000000Z-BBBBBBBBBB.tar.gz.age", "manual-20261019.tar.gz.age", "upgrade-20261019.tar.gz.age"} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := Prune(dir, 1, identity)
	if err != nil || removed != 1 {
		t.Fatalf("retention %d %v", removed, err)
	}
	for _, name := range []string{"daily-20261021T000000Z-BBBBBBBBBB.tar.gz.age", "manual-20261019.tar.gz.age", "upgrade-20261019.tar.gz.age"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("non-daily or newest backup removed")
		}
	}
	corrupt := filepath.Join(dir, "daily-20261018T000000Z-CCCCCCCCCC.tar.gz.age")
	_ = os.WriteFile(corrupt, []byte("corrupt archive"), 0600)
	if _, err := Prune(dir, 1, identity); err == nil {
		t.Fatal("unauthenticated archive pruned")
	}
	if _, err := os.Stat(corrupt); err != nil {
		t.Fatal("corrupt evidence removed")
	}
}
func TestCanceledSnapshotDoesNotPublish(t *testing.T) {
	root := fixture(t)
	identity := key(t)
	file := filepath.Join(tempRoot(t), "canceled.age")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CreateContext(ctx, root, file, identity.Recipient()); err == nil {
		t.Fatal("canceled snapshot completed")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("canceled backup published")
	}
}

func TestMaintenanceRoutingDoesNotBootstrapPocketBase(t *testing.T) {
	if FirstCommand([]string{"users", "create", "--name", "backup"}) != "users" {
		t.Fatal("flag value mistaken for command")
	}
	if FirstCommand([]string{"--dir", "/missing", "backup", "verify"}) != "backup" {
		t.Fatal("maintenance not recognized")
	}
	if handled, err := Dispatch([]string{"--dir", "/must-not-exist", "backup", "verify", "--file", "missing"}); !handled || err == nil {
		t.Fatal("PocketBase flags accepted by standalone recovery")
	}
	if handled, _ := Dispatch([]string{"serve"}); handled {
		t.Fatal("server routed through maintenance")
	}
}
