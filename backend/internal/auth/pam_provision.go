package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/maintenance"
	"slurm-console/backend/internal/pamauth"
)

const pamManagedHelper = "/usr/local/libexec/slurm-console-pam-helper"
const pamAppPolicy = "/etc/pam.d/slurm-console"

// RegisterPAMBundle supplies the worker shipped with the release. Registration
// does not extract files or change the host's authentication configuration.
func RegisterPAMBundle(app core.App, bundle fs.FS) {
	state := pamState(app)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.bundle = bundle
}

// pamHost keeps host paths and process execution together so provisioning can be
// exercised in an isolated filesystem without changing the real host policy.
type pamHost struct {
	root      string
	uid       int
	supported func() bool
	exclusive func() (func(), error)
	process   func(string) (pamauth.Provider, error)
	labels    func(context.Context, []string) error
}

func defaultPAMHost() *pamHost {
	return &pamHost{root: "/", uid: 0,
		supported: func() bool {
			return runtime.GOOS == "linux" && os.Geteuid() == 0 && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
		},
		exclusive: maintenance.Exclusive,
		process:   func(path string) (pamauth.Provider, error) { return pamauth.NewProcess(path, false) },
	}
}

func (h *pamHost) path(path string) string {
	return filepath.Join(h.root, strings.TrimPrefix(path, "/"))
}

// trusted resolves system-directory links (for example /usr/local on CoreOS)
// and authselect shared-policy links, validating every link and target parent.
// The application policy and worker themselves must remain regular files.
func (h *pamHost) trusted(path string, followFinal bool) (os.FileInfo, error) {
	return h.trustedAt(filepath.Clean(path), followFinal, 0)
}

func (h *pamHost) trustedAt(path string, followFinal bool, depth int) (os.FileInfo, error) {
	if depth > 40 || !filepath.IsAbs(path) {
		return nil, errors.New("unsafe PAM path")
	}
	rel, err := filepath.Rel(h.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("PAM path escapes host root")
	}
	current := h.root
	parts := []string{}
	if rel != "." {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	for i := -1; i < len(parts); i++ {
		if i >= 0 {
			current = filepath.Join(current, parts[i])
		}
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if !maintenance.OwnedBy(info, h.uid) {
			return nil, errors.New("unsafe PAM path owner")
		}
		last := i == len(parts)-1
		if info.Mode()&os.ModeSymlink != 0 {
			if last && !followFinal {
				return nil, errors.New("PAM file must not be a symlink")
			}
			target, err := os.Readlink(current)
			if err != nil {
				return nil, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			info, err = h.trustedAt(target, true, depth+1)
			if err != nil {
				return nil, err
			}
		}
		if info.Mode().Perm()&0022 != 0 || (!last && !info.IsDir()) {
			return nil, errors.New("unsafe writable PAM path")
		}
		if last {
			return info, nil
		}
	}
	return nil, errors.New("invalid PAM path")
}

func (h *pamHost) ready(path string, executable, shared bool) bool {
	info, err := h.trusted(path, shared)
	return err == nil && info.Mode().IsRegular() && (!executable || info.Mode().Perm()&0100 != 0)
}

func (h *pamHost) restoreLabels(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if h.labels != nil {
		return h.labels(ctx, paths)
	}
	for _, name := range []string{"/usr/sbin/restorecon", "/sbin/restorecon"} {
		binary := h.path(name)
		if _, err := os.Lstat(binary); os.IsNotExist(err) {
			continue
		}
		if !h.ready(binary, true, true) {
			return errors.New("unsafe restorecon executable")
		}
		command := exec.CommandContext(ctx, binary, append([]string{"--"}, paths...)...)
		command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
		return command.Run()
	}
	return nil
}

func (h *pamHost) policy() ([]byte, error) {
	path := h.path(pamAppPolicy)
	if _, err := os.Lstat(path); err == nil {
		if !h.ready(path, false, false) {
			return nil, errors.New("existing application policy is unsafe")
		}
		return nil, nil // Preserve all existing application policy bytes.
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if info, err := h.trusted(filepath.Dir(path), true); err != nil || !info.IsDir() {
		return nil, errors.New("PAM policy directory is unavailable or unsafe")
	}
	for _, stack := range []string{"password-auth", "common", "system-auth"} {
		auth, account := stack, stack
		if stack == "common" {
			auth, account = "common-auth", "common-account"
		}
		if h.ready(h.path("/etc/pam.d/"+auth), false, true) && h.ready(h.path("/etc/pam.d/"+account), false, true) {
			return []byte("# Application authentication only; shared host policies are unchanged.\nauth include " + auth + "\naccount required pam_nologin.so\naccount include " + account + "\n"), nil
		}
	}
	return nil, errors.New("no trusted system authentication stack")
}

func pamUnavailable(status PAMStatus, message string) (PAMStatus, error) {
	status.Message = message
	return status, fmt.Errorf("%w: %s", ErrPAMUnavailable, message)
}

// ConfigurePAMSetup prepares the fixed application files, verifies the actual
// worker, and atomically commits enabled state and its audit before activation.
func ConfigurePAMSetup(ctx context.Context, app core.App, revision int, actor audit.Actor, authorize func(core.App) error) (PAMStatus, error) {
	state := pamState(app)
	state.mu.Lock()
	host := state.host
	if host == nil {
		host = defaultPAMHost()
	}
	record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
	status := PAMStatus{Configured: app.Store().Get(pamProviderKey) != nil}
	if err == nil {
		status.Revision = record.GetInt("revision")
		if revision != status.Revision {
			err = ErrPAMConflict
		} else {
			err = authorize(app)
		}
	}
	state.mu.Unlock()
	if err != nil {
		return status, err
	}
	status.Supported = host.supported()
	if !status.Supported {
		return pamUnavailable(status, "一键配置 PAM 需要以 root 运行的 Linux amd64/arm64 后端。")
	}
	unlock, err := host.exclusive()
	if err != nil {
		return pamUnavailable(status, "主机正在维护或维护锁不可用，请在部署或备份结束后重试。")
	}
	defer unlock()
	return configurePAMSetupLocked(ctx, app, host, revision, actor, authorize)
}

func configurePAMSetupLocked(ctx context.Context, app core.App, host *pamHost, revision int, actor audit.Actor, authorize func(core.App) error) (status PAMStatus, err error) {
	state := pamState(app)
	state.mu.Lock()
	defer state.mu.Unlock()
	record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
	if err != nil {
		return status, err
	}
	status.Revision, status.Configured = record.GetInt("revision"), app.Store().Get(pamProviderKey) != nil
	if revision != status.Revision {
		return status, ErrPAMConflict
	}
	if err := authorize(app); err != nil {
		return status, err
	}
	status.Supported = host.supported()
	if !status.Supported {
		return pamUnavailable(status, "一键配置 PAM 需要以 root 运行的 Linux amd64/arm64 后端。")
	}
	policy, err := host.policy()
	if err != nil {
		return pamUnavailable(status, "请主机管理员提供安全的应用 PAM 策略，或现有 password-auth、common-auth/common-account、system-auth 认证栈；不会修改共享策略。")
	}
	helper := os.Getenv("SLURM_CONSOLE_PAM_HELPER")
	custom := helper != ""
	var payload []byte
	if custom {
		if !filepath.IsAbs(helper) || filepath.Clean(helper) != helper || !host.ready(helper, true, false) {
			return pamUnavailable(status, "自定义 PAM 认证组件缺失或权限不安全，请主机管理员修复；配置会保留自定义组件。")
		}
	} else {
		helper = host.path(pamManagedHelper)
		if state.bundle == nil {
			return pamUnavailable(status, "发行版缺少内置 PAM 认证组件，请更新为包含认证组件的完整发行版后重试。")
		}
		payload, err = fs.ReadFile(state.bundle, "slurm-console-pam-helper")
		if err != nil || len(payload) == 0 {
			return pamUnavailable(status, "发行版缺少内置 PAM 认证组件，请更新为包含认证组件的完整发行版后重试。")
		}
	}
	changes := &pamHostChanges{host: host}
	if policy == nil {
		if err := changes.observe(host.path(pamAppPolicy)); err != nil {
			return pamUnavailable(status, "应用 PAM 策略在配置期间发生变化，请检查权限后重试。")
		}
	}
	if custom {
		if err := changes.observe(helper); err != nil {
			return pamUnavailable(status, "自定义 PAM 组件在配置期间发生变化，请检查权限后重试。")
		}
	}
	// Stage privately outside the managed paths so missing runtime libraries are
	// detected before either application file is changed.
	staged := helper
	if !custom {
		dir, stageErr := os.MkdirTemp("", "slurm-console-pam-preflight-")
		if stageErr != nil {
			return pamUnavailable(status, "无法准备 PAM 组件，请检查主机临时目录和磁盘权限。")
		}
		defer os.RemoveAll(dir)
		staged = filepath.Join(dir, "slurm-console-pam-helper")
		if err := os.WriteFile(staged, payload, 0700); err != nil {
			return pamUnavailable(status, "无法准备 PAM 组件，请检查主机临时目录和磁盘权限。")
		}
	}
	candidate, err := host.process(staged)
	if err == nil {
		var result pamauth.Result
		result, err = candidate.Run(ctx, pamauth.Request{Mode: "capabilities"})
		if !result.OK && err == nil {
			err = pamauth.ErrUnavailable
		}
	}
	if err != nil {
		return pamUnavailable(status, "PAM 运行环境不可用。请确认主机提供 glibc 和 libpam.so.0，且完整发行版与主机架构匹配；无需安装开发工具。")
	}
	// Refresh permission immediately before the first host write.
	if err := authorize(app); err != nil {
		return status, err
	}
	defer func() {
		if err != nil {
			if rollbackErr := changes.rollback(); rollbackErr != nil {
				status, err = pamUnavailable(status, "PAM 配置未保存，且主机文件恢复失败。请主机管理员检查固定组件和应用策略目录后重试；已保留恢复备份。")
			}
		}
		changes.cleanup()
	}()
	if !custom {
		if err := changes.install(helper, payload, 0755, true); err != nil {
			return pamUnavailable(status, "无法安全安装 PAM 组件，请检查固定安装目录的权限和可用空间。")
		}
	}
	if policy != nil {
		if err := changes.install(host.path(pamAppPolicy), policy, 0644, false); err != nil {
			return pamUnavailable(status, "无法安全创建应用 PAM 策略，请检查策略目录权限后重试。")
		}
	}
	paths := make([]string, len(changes.files))
	for i := range changes.files {
		paths[i] = changes.files[i].path
	}
	if err := host.restoreLabels(ctx, paths); err != nil {
		return pamUnavailable(status, "无法恢复 PAM 应用文件的 SELinux 标签，请主机管理员检查安全策略后重试。")
	}
	status.HelperInstalled = host.ready(helper, true, false)
	status.PolicyInstalled = host.ready(host.path(pamAppPolicy), false, false)
	candidate, err = host.process(helper)
	if err == nil && status.HelperInstalled && status.PolicyInstalled {
		var result pamauth.Result
		result, err = candidate.Run(ctx, pamauth.Request{Mode: "probe"})
		status.Available = err == nil && result.OK
	}
	if !status.Available {
		return pamUnavailable(status, "PAM 组件验证失败。请检查主机运行库、应用策略及其引用的认证模块；配置已回滚。")
	}
	if !changes.unchanged() {
		return pamUnavailable(status, "配置期间主机文件已被管理员更改，请检查后重试；已保留管理员的文件。")
	}
	if err := ctx.Err(); err != nil {
		return pamUnavailable(status, "PAM 配置请求已取消，请重新检测后重试。")
	}
	commitAuthorize := func(tx core.App) error {
		if err := authorize(tx); err != nil {
			return err
		}
		if !changes.unchanged() || !host.ready(helper, true, false) || !host.ready(host.path(pamAppPolicy), false, false) {
			status.Message = "配置期间主机 PAM 文件或权限已变化，请检查后重试；已保留管理员的文件。"
			return fmt.Errorf("%w: %s", ErrPAMUnavailable, status.Message)
		}
		return nil
	}
	committed, saveErr := persistPAMSetupLocked(app, true, revision, actor, commitAuthorize, status, candidate)
	if saveErr != nil {
		return status, saveErr
	}
	return committed, nil
}

// PAMSetupCommand shares provisioning with the web setup and uses a trusted
// local audit actor. The application's inherited --dir flag selects its data.
func PAMSetupCommand(app core.App) *cobra.Command {
	root := &cobra.Command{Use: "pam", Short: "Configure application PAM authentication"}
	root.AddCommand(&cobra.Command{Use: "configure", Args: cobra.NoArgs, Short: "Install, verify and enable the bundled PAM integration", RunE: func(cmd *cobra.Command, _ []string) error {
		host := defaultPAMHost()
		if !host.supported() {
			return errors.New("PAM configuration requires root on Linux amd64/arm64")
		}
		unlock, err := host.exclusive()
		if err != nil {
			return err
		}
		defer unlock()
		if !app.IsBootstrapped() {
			defer app.ResetBootstrapState()
			if err := app.Bootstrap(); err != nil {
				return err
			}
		}
		if err := app.RunAllMigrations(); err != nil {
			return err
		}
		record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
		if err != nil {
			return err
		}
		status, err := configurePAMSetupLocked(cmd.Context(), app, host, record.GetInt("revision"), audit.LocalActor(), func(core.App) error { return nil })
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), status.Message)
		return nil
	}})
	return root
}

// A backup is a hard link to the old inode: replacing a helper never creates a
// missing/partial executable window, and rollback can restore exactly that inode.
type pamHostChange struct {
	path, backup string
	installed    os.FileInfo
	data         []byte
	mode         fs.FileMode
	uid          int
	backupInfo   os.FileInfo
	keepBackup   bool
}
type pamHostDir struct {
	path string
	info os.FileInfo
}
type pamHostChanges struct {
	host            *pamHost
	files, observed []pamHostChange
	dirs            []pamHostDir
}

func (c *pamHostChanges) observe(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c.observed = append(c.observed, pamHostChange{path: path, installed: info, data: data, mode: info.Mode(), uid: c.host.uid})
	return nil
}

func (c *pamHostChanges) ensureDir(path string) error {
	if info, err := c.host.trusted(path, true); err == nil && info.IsDir() {
		return nil
	} else if !os.IsNotExist(err) {
		return errors.New("unsafe PAM installation directory")
	}
	if err := c.ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0755); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	c.dirs = append(c.dirs, pamHostDir{path: path, info: info})
	return nil
}

func (c *pamHostChanges) install(path string, data []byte, mode fs.FileMode, replace bool) error {
	if err := c.ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	old, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if old != nil && (!replace || !c.host.ready(path, true, false)) {
		return errors.New("existing PAM file cannot be safely replaced")
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".slurm-console-pam-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	defer temp.Close()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	info, err := temp.Stat()
	if err != nil || !maintenance.OwnedBy(info, c.host.uid) {
		return errors.New("unsafe installed PAM file owner")
	}
	if err := temp.Close(); err != nil {
		return err
	}
	change := pamHostChange{path: path, installed: info, data: data, mode: mode, uid: c.host.uid}
	if old != nil {
		backupFile, err := os.CreateTemp(filepath.Dir(path), ".slurm-console-pam-backup-")
		if err != nil {
			return err
		}
		backup := backupFile.Name()
		backupFile.Close()
		if err := os.Remove(backup); err != nil {
			return err
		}
		if err := os.Link(path, backup); err != nil {
			return err
		}
		change.backup = backup
		current, err := os.Lstat(path)
		linked, linkErr := os.Lstat(backup)
		if err != nil || linkErr != nil || !os.SameFile(old, current) || !os.SameFile(old, linked) || old.Mode() != current.Mode() || old.Size() != current.Size() || !old.ModTime().Equal(current.ModTime()) {
			os.Remove(backup)
			return errors.New("PAM helper changed during installation")
		}
		change.backupInfo = linked
		if err := os.Rename(name, path); err != nil {
			os.Remove(backup)
			return err
		}
	} else {
		if err := os.Link(name, path); err != nil {
			return err
		}
	}
	c.files = append(c.files, change)
	return nil
}

func (f *pamHostChange) unchanged() bool {
	info, err := os.Lstat(f.path)
	if err != nil || !os.SameFile(info, f.installed) || info.Mode() != f.mode || !maintenance.OwnedBy(info, f.uid) {
		return false
	}
	data, err := os.ReadFile(f.path)
	return err == nil && bytes.Equal(data, f.data)
}
func (c *pamHostChanges) unchanged() bool {
	for i := range c.files {
		if !c.files[i].unchanged() {
			return false
		}
	}
	for i := range c.observed {
		if !c.observed[i].unchanged() {
			return false
		}
	}
	return true
}
func (c *pamHostChanges) rollback() error {
	var result error
	for i := len(c.files) - 1; i >= 0; i-- {
		f := &c.files[i]
		if !f.unchanged() {
			continue
		} // An administrator's replacement wins.
		var err error
		if f.backup != "" {
			info, statErr := os.Lstat(f.backup)
			if statErr != nil || !os.SameFile(info, f.backupInfo) {
				f.keepBackup = true
				result = errors.Join(result, errors.New("PAM restore backup changed"))
				continue
			}
			err = os.Rename(f.backup, f.path)
		} else {
			err = os.Remove(f.path)
		}
		if err != nil {
			f.keepBackup = true
			result = errors.Join(result, err)
		}
	}
	return result
}
func (c *pamHostChanges) cleanup() {
	for i := range c.files {
		f := &c.files[i]
		if f.backup != "" && !f.keepBackup {
			if info, err := os.Lstat(f.backup); err == nil && os.SameFile(info, f.backupInfo) {
				_ = os.Remove(f.backup)
			}
		}
	}
	for i := len(c.dirs) - 1; i >= 0; i-- {
		dir := c.dirs[i]
		if info, err := os.Lstat(dir.path); err == nil && os.SameFile(info, dir.info) {
			_ = os.Remove(dir.path)
		}
	} // Only empty directories created here.
}
