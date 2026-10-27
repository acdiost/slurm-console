package backup

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"filippo.io/age"
	"github.com/spf13/cobra"
	"slurm-console/backend/internal/maintenance"
)

func rootLinux() error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return fmt.Errorf("run as root on the Linux application host")
	}
	return nil
}
func readKey(filename string) (age.Identity, age.Recipient, error) {
	if err := outsideInstallation(filename, ""); err != nil {
		return nil, nil, fmt.Errorf("keep backup identity outside application snapshot paths: %w", err)
	}
	if err := checkParents(filename); err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 || !maintenance.OwnedBy(info, os.Geteuid()) {
		return nil, nil, fmt.Errorf("identity must be a private regular file (0600)")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, err
	}
	defer clear(data)
	identities, err := age.ParseIdentities(strings.NewReader(string(data)))
	if err != nil || len(identities) != 1 {
		return nil, nil, fmt.Errorf("expected one native age backup identity")
	}
	switch key := identities[0].(type) {
	case *age.HybridIdentity:
		return key, key.Recipient(), nil
	case *age.X25519Identity:
		return key, key.Recipient(), nil
	default:
		return nil, nil, fmt.Errorf("unsupported backup identity")
	}
}
func secureDirectory(directory string) error {
	if err := safeParent(filepath.Dir(directory)); err != nil {
		return err
	}
	if _, err := os.Lstat(directory); os.IsNotExist(err) {
		if err := os.Mkdir(directory, 0700); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := checkParents(directory); err != nil {
		return err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || !maintenance.OwnedBy(info, os.Geteuid()) {
		return fmt.Errorf("backup directory must be private (0700): %s", directory)
	}
	return nil
}
func service(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
func active() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "show", "slurm-console", "--property=ActiveState", "--value").Output()
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(out)) {
	case "active":
		return true, nil
	case "inactive", "failed":
		return false, nil
	default:
		return false, fmt.Errorf("application service is changing state; retry later")
	}
}
func Health(ctx context.Context, url string) error {
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			var body struct{ Data struct{ Status string } }
			err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&body)
			response.Body.Close()
			if err == nil && response.StatusCode == 200 && body.Data.Status == "ok" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("application readiness check failed: %w", ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func ready() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Health(ctx, "http://127.0.0.1:18090/api/v1/health"); err != nil {
		return err
	}
	running, err := active()
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("application service is not active")
	}
	return nil
}
func restartIf(wasActive bool) error {
	if !wasActive {
		return nil
	}
	if err := service("start", "slurm-console"); err != nil {
		return err
	}
	return ready()
}
func archiveName(kind string) string {
	return filepath.Join(DefaultDirectory, kind+"-"+time.Now().UTC().Format("20060102T150405Z")+"-"+rand.Text()[:10]+".tar.gz.age")
}
func snapshot(ctx context.Context, filename string, recipient age.Recipient) (m Manifest, resultErr error) {
	wasActive, err := active()
	if err != nil {
		return m, err
	}
	if err := service("stop", "slurm-console"); err != nil {
		return m, err
	}
	defer func() { resultErr = errors.Join(resultErr, restartIf(wasActive)) }()
	return CreateContext(ctx, "/", filename, recipient)
}
func output(cmd *cobra.Command, filename string, m Manifest, extra ...map[string]any) error {
	report := map[string]any{"file": filename, "format": m.Format, "createdAt": m.CreatedAt, "files": len(m.Entries), "os": m.OS, "arch": m.Arch, "verified": true, "purpose": m.Purpose, "databaseVerified": m.Purpose == "snapshot"}
	for _, fields := range extra {
		for key, value := range fields {
			report[key] = value
		}
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
}

func Commands() *cobra.Command {
	root := &cobra.Command{Use: "backup", Short: "Encrypted application backup, verification and recovery (no third-party service configuration)"}
	var keyPath string
	root.PersistentFlags().StringVar(&keyPath, "key", DefaultKey, "Private age identity file; keep a separate offline copy")
	init := &cobra.Command{Use: "init-key", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rootLinux(); err != nil {
			return err
		}
		unlock, err := maintenance.Exclusive()
		if err != nil {
			return err
		}
		defer unlock()
		if _, err := os.Lstat(keyPath); err == nil {
			_, _, err = readKey(keyPath)
			return err
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := outsideInstallation(keyPath, ""); err != nil {
			return err
		}
		if err := secureDirectory(filepath.Dir(keyPath)); err != nil {
			return err
		}
		key, err := age.GenerateHybridIdentity()
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(keyPath), ".identity-*.partial")
		if err != nil {
			return err
		}
		temporary := file.Name()
		defer os.Remove(temporary)
		defer file.Close()
		if err := file.Chmod(0600); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(file, key.String()); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := os.Link(temporary, keyPath); err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Backup identity initialized:", keyPath, "(store a separate offline copy)")
		return nil
	}}
	var file, copyDir, kind string
	var keep int
	create := &cobra.Command{Use: "create", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rootLinux(); err != nil {
			return err
		}
		unlock, err := maintenance.Exclusive()
		if err != nil {
			return err
		}
		defer unlock()
		identity, recipient, err := readKey(keyPath)
		if err != nil {
			return err
		}
		if err := secureDirectory(DefaultDirectory); err != nil {
			return err
		}
		if keep < 0 || keep > 365 || keep > 0 && kind != "daily" {
			return fmt.Errorf("--keep must be 1..365 for daily backups only (0 disables pruning)")
		}
		if kind != "manual" && kind != "daily" && kind != "upgrade" {
			return fmt.Errorf("kind must be manual, daily or upgrade")
		}
		if file == "" {
			file = archiveName(kind)
		}
		if err := secureDirectory(filepath.Dir(file)); err != nil {
			return err
		}
		if err := outsideInstallation(file, keyPath); err != nil {
			return err
		}
		if copyDir != "" {
			if _, err := os.Stat(copyDir); err != nil {
				return err
			}
			if err := secureDirectory(copyDir); err != nil {
				return err
			}
		}
		ctx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stopSignals()
		_, err = snapshot(ctx, file, recipient)
		if err != nil {
			return err
		}
		m, err := Verify(file, identity)
		if err != nil {
			return fmt.Errorf("backup created but verification failed: %w", err)
		}
		if copyDir != "" {
			if err := CopyArchive(file, copyDir); err != nil {
				return fmt.Errorf("local backup verified, secondary copy failed: %w", err)
			}
		}
		pruned := 0
		if keep > 0 {
			var err error
			pruned, err = Prune(filepath.Dir(file), keep, identity)
			if err != nil {
				return err
			}
		}
		copyStatus := "not_configured"
		if copyDir != "" {
			copyStatus = "verified"
		}
		return output(cmd, file, m, map[string]any{"secondaryCopy": copyStatus, "prunedDaily": pruned})
	}}
	create.Flags().IntVar(&keep, "keep", 0, "Retain this many authenticated daily snapshots; manual/upgrade snapshots are never pruned")
	create.Flags().StringVar(&file, "file", "", "New destination file (default /var/backups/slurm-console)")
	create.Flags().StringVar(&kind, "kind", "manual", "Filename class: manual, daily, upgrade")
	create.Flags().StringVar(&copyDir, "copy-dir", os.Getenv("SLURM_CONSOLE_BACKUP_COPY_DIR"), "Existing private directory on administrator-managed independent storage")
	var verifyFile string
	verify := &cobra.Command{Use: "verify", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		identity, _, err := readKey(keyPath)
		if err != nil {
			return err
		}
		m, err := Verify(verifyFile, identity)
		if err != nil {
			return err
		}
		return output(cmd, verifyFile, m)
	}}
	verify.Flags().StringVar(&verifyFile, "file", "", "Encrypted backup to authenticate and check")
	_ = verify.MarkFlagRequired("file")
	var restoreFile, to, confirmation string
	var apply bool
	restore := &cobra.Command{Use: "restore", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := rootLinux(); err != nil {
			return err
		}
		_, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stopSignals()
		if apply == (to != "") {
			return fmt.Errorf("choose exactly one of --to NEW_DIRECTORY or --apply --confirm RESTORE")
		}
		if apply && confirmation != "RESTORE" {
			return fmt.Errorf("overwriting the application requires --confirm RESTORE")
		}
		identity, recipient, err := readKey(keyPath)
		if err != nil {
			return err
		}
		if apply {
			if err := outsideInstallation(restoreFile, keyPath); err != nil {
				return err
			}
		}
		if !apply {
			if err := outsideInstallation(to, keyPath); err != nil {
				return err
			}
			m, err := Extract(restoreFile, to, identity)
			if err != nil {
				return err
			}
			return output(cmd, to, m)
		}
		unlock, err := maintenance.Exclusive()
		if err != nil {
			return err
		}
		defer unlock()
		parent, err := os.MkdirTemp("/var/lib", "slurm-console-restore-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(parent)
		stage := filepath.Join(parent, "verified")
		m, err := Extract(restoreFile, stage, identity)
		if err != nil {
			return err
		}
		if m.Arch != runtime.GOARCH {
			return fmt.Errorf("backup architecture does not match this host")
		}
		if err := validatePAM(stage, m); err != nil {
			return err
		}
		account, err := user.Lookup("slurm-console")
		if err != nil {
			return err
		}
		uid, err := strconv.Atoi(account.Uid)
		if err != nil || uid == 0 {
			return fmt.Errorf("a non-root slurm-console service account is required")
		}
		gid, err := strconv.Atoi(account.Gid)
		if err != nil {
			return err
		}
		if err := secureDirectory(DefaultDirectory); err != nil {
			return err
		}
		wasActive, err := active()
		if err != nil {
			return err
		}
		if err := service("stop", "slurm-console"); err != nil {
			return err
		}
		guard := archiveName("before-restore")
		if _, err := Preserve("/", guard, recipient); err != nil {
			_ = restartIf(wasActive)
			return fmt.Errorf("restore not applied: cannot preserve current installation: %w", err)
		}
		if _, err := Verify(guard, identity); err != nil {
			_ = restartIf(wasActive)
			return err
		}
		change, err := Apply("/", stage, m, uid, gid)
		if err != nil {
			_ = service("daemon-reload")
			_ = restartIf(wasActive)
			return err
		}
		err = relabel()
		if err == nil {
			err = service("daemon-reload")
		}
		if err == nil {
			err = restartIf(wasActive)
		}
		if err != nil {
			_ = service("stop", "slurm-console")
			rollbackErr := change.Rollback()
			_ = service("daemon-reload")
			startErr := restartIf(wasActive)
			return errors.Join(fmt.Errorf("restored service failed; previous installation restored: %w", err), rollbackErr, startErr)
		}
		if err := change.Commit(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Pre-restore encrypted snapshot:", guard)
		return output(cmd, restoreFile, m)
	}}
	restore.Flags().StringVar(&restoreFile, "file", "", "Encrypted backup")
	restore.Flags().StringVar(&to, "to", "", "Extract and verify into a new private directory without applying")
	restore.Flags().BoolVar(&apply, "apply", false, "Replace only application-managed paths; preserve current installation first")
	restore.Flags().StringVar(&confirmation, "confirm", "", "Explicit overwrite acknowledgement: RESTORE")
	_ = restore.MarkFlagRequired("file")
	root.AddCommand(init, create, verify, restore)
	return root
}
func outsideInstallation(filename, key string) error {
	p, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	k, err := filepath.Abs(key)
	if err != nil {
		return err
	}
	if key != "" && p == k {
		return fmt.Errorf("destination cannot overwrite the backup identity")
	}
	for _, g := range Groups {
		base := "/" + g.Path
		if p == base || g.Dir && strings.HasPrefix(p, base+"/") {
			return fmt.Errorf("backup/staging destination must be outside application paths")
		}
	}
	return nil
}
func validatePAM(stage string, m Manifest) error {
	for _, e := range m.Entries {
		if e.Path == "etc/sudoers.d/slurm-console-pam" {
			cmd := exec.Command("visudo", "-cf", filepath.Join(stage, e.Path))
			if data, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("backup sudoers policy rejected: %s", strings.TrimSpace(string(data)))
			}
		}
	}
	return nil
}
func HealthCommand() *cobra.Command {
	var url string
	cmd := &cobra.Command{Use: "healthcheck", Args: cobra.NoArgs, Short: "Wait up to 20 seconds for application/database readiness", RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
		defer cancel()
		return Health(ctx, url)
	}}
	cmd.Flags().StringVar(&url, "url", "http://127.0.0.1:18090/api/v1/health", "Application health endpoint")
	return cmd
}

func relabel() error {
	if _, err := exec.LookPath("restorecon"); err != nil {
		return nil
	}
	args := []string{"-RF", "--"}
	for _, g := range Groups {
		p := "/" + g.Path
		if _, err := os.Lstat(p); err == nil {
			args = append(args, p)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if data, err := exec.CommandContext(ctx, "restorecon", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("restore SELinux labels: %s", strings.TrimSpace(string(data)))
	}
	return nil
}
