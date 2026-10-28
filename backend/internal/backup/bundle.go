// Package backup stores version-matched encrypted application snapshots.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	_ "modernc.org/sqlite"
)

const DefaultKey = "/var/lib/slurm-console-backup/identity.txt"
const DefaultDirectory = "/var/backups/slurm-console"
const maxBytes = int64(16 << 30)
const maxFiles = 100000
const manifestName = "manifest.json"

type Group struct {
	Path     string
	Required bool
	Dir      bool
}

var Groups = []Group{{"var/lib/slurm-console/pb_data", true, true}, {"opt/slurm-console", true, true}, {"etc/slurm-console", true, true}, {"etc/systemd/system/slurm-console.service", true, false}, {"etc/systemd/system/slurm-console-backup.service", false, false}, {"etc/systemd/system/slurm-console-backup.timer", false, false}, {"usr/local/libexec/slurm-console-pam-helper", false, false}, {"etc/pam.d/slurm-console", false, false}, {"etc/sudoers.d/slurm-console-pam", false, false}, {"etc/sudoers.d/slurm-console-linux-users", false, false}}

type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   int64  `json:"mode"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Format    int     `json:"format"`
	Purpose   string  `json:"purpose"`
	CreatedAt string  `json:"createdAt"`
	OS        string  `json:"os"`
	Arch      string  `json:"arch"`
	Entries   []Entry `json:"entries"`
}

func allowed(name string) bool {
	if name == "" || path.Clean(name) != name || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return false
	}
	for _, g := range Groups {
		if name == g.Path || g.Dir && strings.HasPrefix(name, g.Path+"/") {
			return true
		}
	}
	return false
}
func checkParents(filename string) error {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	for p := absolute; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func digestFile(filename string) (string, int64, error) {
	return digestFileContext(context.Background(), filename)
}
func digestFileContext(ctx context.Context, filename string) (string, int64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, f})
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func checkSQLite(root string) error {
	for _, name := range []string{"data.db", "auxiliary.db"} {
		filename := filepath.Join(root, "var/lib/slurm-console/pb_data", name)
		if _, err := os.Stat(filename); os.IsNotExist(err) && name == "auxiliary.db" {
			continue
		} else if err != nil {
			return err
		}
		db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filename}).String()+"?mode=ro")
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var result string
		err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
		cancel()
		_ = db.Close()
		if err != nil {
			return err
		}
		if result != "ok" {
			return fmt.Errorf("SQLite integrity check failed for %s", name)
		}
	}
	return nil
}
func Create(root, filename string, recipient age.Recipient) (Manifest, error) {
	return CreateContext(context.Background(), root, filename, recipient)
}
func Preserve(root, filename string, recipient age.Recipient) (Manifest, error) {
	return create(context.Background(), root, filename, recipient, "preservation")
}
func CreateContext(ctx context.Context, root, filename string, recipient age.Recipient) (Manifest, error) {
	return create(ctx, root, filename, recipient, "snapshot")
}
func create(ctx context.Context, root, filename string, recipient age.Recipient, purpose string) (Manifest, error) {
	var m Manifest
	if purpose == "snapshot" {
		if err := checkSQLiteCopy(ctx, root); err != nil {
			return m, err
		}
	}
	m = Manifest{Purpose: purpose, Format: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), OS: "linux", Arch: runtime.GOARCH, Entries: []Entry{}}
	var total int64
	for _, group := range Groups {
		base := filepath.Join(root, group.Path)
		if _, err := os.Lstat(base); os.IsNotExist(err) && (!group.Required || purpose == "preservation") {
			continue
		} else if err != nil {
			return m, err
		}
		if err := checkParents(base); err != nil {
			return m, err
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode().IsDir() {
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported non-regular entry: %s", p)
			}
			relative, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(relative)
			if name == "var/lib/slurm-console/pb_data/data.db-shm" || name == "var/lib/slurm-console/pb_data/auxiliary.db-shm" {
				return nil
			}
			if !allowed(name) {
				return fmt.Errorf("invalid snapshot path")
			}
			if info.Size() < 0 || info.Size() > maxBytes-total || len(m.Entries) >= maxFiles {
				return fmt.Errorf("snapshot exceeds supported limits")
			}
			sum, n, err := digestFileContext(ctx, p)
			if err != nil {
				return err
			}
			total += n
			m.Entries = append(m.Entries, Entry{name, n, int64(info.Mode().Perm()), sum})
			return nil
		})
		if err != nil {
			return m, err
		}
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	if err := validateManifest(m); err != nil {
		return m, err
	}
	parent := filepath.Dir(filename)
	if err := checkParents(parent); err != nil {
		return m, err
	}
	if _, err := os.Lstat(filename); !os.IsNotExist(err) {
		return m, fmt.Errorf("backup destination must not exist")
	}
	output, err := os.CreateTemp(parent, ".backup-*.partial")
	if err != nil {
		return m, err
	}
	temporary := output.Name()
	defer os.Remove(temporary)
	defer output.Close()
	if err := output.Chmod(0600); err != nil {
		return m, err
	}
	encrypted, err := age.Encrypt(output, recipient)
	if err != nil {
		return m, err
	}
	compressed := gzip.NewWriter(encrypted)
	archive := tar.NewWriter(compressed)
	raw, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	if len(raw) > 16<<20 {
		return m, fmt.Errorf("manifest too large")
	}
	if err := archive.WriteHeader(&tar.Header{Name: manifestName, Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
		return m, err
	}
	if _, err := archive.Write(raw); err != nil {
		return m, err
	}
	for _, entry := range m.Entries {
		if err := archive.WriteHeader(&tar.Header{Name: entry.Path, Mode: entry.Mode, Size: entry.Size, Typeflag: tar.TypeReg}); err != nil {
			return m, err
		}
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			return m, err
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(archive, h), contextReader{ctx, f})
		_ = f.Close()
		if copyErr != nil {
			return m, copyErr
		}
		if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
			return m, fmt.Errorf("snapshot source changed during backup")
		}
	}
	for _, close := range []func() error{archive.Close, compressed.Close, encrypted.Close, output.Sync, output.Close} {
		if err := close(); err != nil {
			return m, err
		}
	}
	// A hard link publishes without overwriting an existing archive, even in a race.
	if err := os.Link(temporary, filename); err != nil {
		return m, err
	}
	return m, nil
}
func validateManifest(m Manifest) error {
	if m.Format != 1 || (m.Purpose != "snapshot" && m.Purpose != "preservation") || m.OS != "linux" || m.Arch == "" || len(m.Entries) == 0 || len(m.Entries) > maxFiles {
		return fmt.Errorf("unsupported backup manifest")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.CreatedAt); err != nil {
		return fmt.Errorf("invalid backup creation time")
	}
	seen := map[string]bool{}
	var total int64
	for _, e := range m.Entries {
		if !allowed(e.Path) || seen[e.Path] || e.Size < 0 || e.Size > maxBytes-total || len(e.SHA256) != 64 || e.Mode < 0 || e.Mode > 0777 {
			return fmt.Errorf("invalid manifest entry")
		}
		if _, err := hex.DecodeString(e.SHA256); err != nil {
			return err
		}
		seen[e.Path] = true
		total += e.Size
	}
	for _, required := range []string{"var/lib/slurm-console/pb_data/data.db", "opt/slurm-console/slurm-console", "opt/slurm-console/public/index.html", "etc/slurm-console/slurm-console.env", "etc/systemd/system/slurm-console.service"} {
		if m.Purpose == "snapshot" && !seen[required] {
			return fmt.Errorf("required file absent: %s", required)
		}
	}
	return nil
}

// Extract writes only into a new private staging tree and authenticates every
// payload byte before the caller can apply it to the installation.
func Extract(filename, destination string, identity age.Identity) (Manifest, error) {
	return extract(filename, destination, identity, false)
}
func extract(filename, destination string, identity age.Identity, allowPreservation bool) (Manifest, error) {
	var m Manifest
	if err := checkParents(filepath.Dir(destination)); err != nil {
		return m, err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return m, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(destination)
		}
	}()
	f, err := os.Open(filename)
	if err != nil {
		return m, err
	}
	defer f.Close()
	reader, err := age.Decrypt(f, identity)
	if err != nil {
		return m, fmt.Errorf("cannot decrypt backup: %w", err)
	}
	compressed, err := gzip.NewReader(reader)
	if err != nil {
		return m, err
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	header, err := archive.Next()
	if err != nil {
		return m, err
	}
	if header.Name != manifestName || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > 16<<20 {
		return m, fmt.Errorf("missing backup manifest")
	}
	decoder := json.NewDecoder(io.LimitReader(archive, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return m, fmt.Errorf("invalid manifest trailer")
	}
	if err := validateManifest(m); err != nil {
		return m, err
	}
	if m.Purpose != "snapshot" && !allowPreservation {
		return m, fmt.Errorf("preservation archive is diagnostic evidence, not a restorable snapshot")
	}
	expected := map[string]Entry{}
	for _, e := range m.Entries {
		expected[e.Path] = e
	}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, err
		}
		entry, ok := expected[header.Name]
		if !ok || !allowed(header.Name) || header.Typeflag != tar.TypeReg || header.Size != entry.Size || header.Linkname != "" {
			return m, fmt.Errorf("unexpected archive entry")
		}
		full := filepath.Join(destination, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			return m, err
		}
		out, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return m, err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(out, h), archive)
		closeErr := out.Close()
		if err != nil {
			return m, err
		}
		if closeErr != nil {
			return m, closeErr
		}
		if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
			return m, fmt.Errorf("backup checksum mismatch")
		}
		delete(expected, entry.Path)
	}
	if len(expected) != 0 {
		return m, fmt.Errorf("backup is incomplete")
	}
	n, err := io.Copy(io.Discard, compressed)
	if err != nil {
		return m, err
	}
	if n != 0 {
		return m, fmt.Errorf("unexpected data after archive")
	}
	// Drain the age stream to authenticate its final chunk and reject truncation.
	n, err = io.Copy(io.Discard, reader)
	if err != nil {
		return m, err
	}
	if n != 0 {
		return m, fmt.Errorf("unexpected data after compressed backup")
	}
	if m.Purpose == "snapshot" {
		if err := checkSQLite(destination); err != nil {
			return m, err
		}
	}
	success = true
	return m, nil
}
func Verify(filename string, identity age.Identity) (Manifest, error) {
	parent, err := os.MkdirTemp("", "slurm-backup-verify-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(parent)
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return Manifest{}, err
	}
	return extract(filename, filepath.Join(parent, "contents"), identity, true)
}
func CopyArchive(source, directory string) error {
	if err := checkParents(directory); err != nil {
		return err
	}
	target := filepath.Join(directory, filepath.Base(source))
	sum, _, err := digestFile(source)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".backup-copy-*.partial")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	defer temp.Close()
	_ = temp.Chmod(0600)
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := io.Copy(temp, in); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	copied, _, err := digestFile(name)
	if err != nil {
		return err
	}
	if copied != sum {
		return errors.New("backup copy checksum mismatch")
	}
	return os.Link(name, target)
}

// SQLite checks run on a private copy: read-only SQLite may create WAL shared
// memory files, which must never become root-owned in the live service directory.
func checkSQLiteCopy(ctx context.Context, root string) error {
	temp, err := os.MkdirTemp("", "slurm-sqlite-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	target := filepath.Join(temp, "var/lib/slurm-console/pb_data")
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	for _, name := range []string{"data.db", "data.db-wal", "auxiliary.db", "auxiliary.db-wal"} {
		source := filepath.Join(root, "var/lib/slurm-console/pb_data", name)
		info, err := os.Lstat(source)
		if os.IsNotExist(err) && name != "data.db" {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxBytes {
			return fmt.Errorf("invalid SQLite snapshot source")
		}
		if err := checkParents(source); err != nil {
			return err
		}
		in, err := os.Open(source)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(target, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(out, contextReader{ctx, in})
		in.Close()
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return checkSQLite(temp)
}
