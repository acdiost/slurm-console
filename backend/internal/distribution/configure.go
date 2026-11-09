package distribution

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/term"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/clusterconfig"
	"slurm-console/backend/internal/maintenance"
	"slurm-console/backend/internal/platformsettings"
	"slurm-console/backend/internal/rbac"
)

type prompts struct {
	in       *bufio.Reader
	out      io.Writer
	terminal *os.File
}

func (p *prompts) read(label, fallback string, secret bool) (string, error) {
	if secret {
		fmt.Fprintf(p.out, "%s（回车保留已有值）: ", label)
	} else {
		fmt.Fprintf(p.out, "%s [%s]: ", label, fallback)
	}
	var raw string
	if secret && p.terminal != nil && term.IsTerminal(int(p.terminal.Fd())) {
		data, err := term.ReadPassword(int(p.terminal.Fd()))
		if err != nil {
			return "", err
		}
		defer clear(data)
		fmt.Fprintln(p.out)
		raw = string(data)
	} else {
		line, err := p.in.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("配置已取消，未收到完整输入: %w", err)
		}
		raw = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	}
	if len(raw) > 65536 {
		return "", fmt.Errorf("输入过长")
	}
	if !secret {
		raw = strings.TrimSpace(raw)
	}
	if raw == "" {
		return fallback, nil
	}
	if raw == "-" && !secret {
		return "", nil
	}
	return raw, nil
}

// Configure runs only inside the stopped-service installer and inherited lock.
func Configure(dir string) error {
	unlock, err := maintenance.Shared()
	if err != nil {
		return err
	}
	defer unlock()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: dir})
	if err = app.Bootstrap(); err != nil {
		return err
	}
	defer app.ResetBootstrapState()
	if err = app.RunAllMigrations(); err != nil {
		return err
	}
	p := &prompts{in: bufio.NewReader(os.Stdin), out: os.Stdout, terminal: os.Stdin}
	return configure(app, "/etc/slurm-console", p)
}

func configure(app core.App, configDir string, p *prompts) error {
	fmt.Fprintln(p.out, "Slurm Console 终端配置：回车使用默认/已有值，输入 - 清除可选值。服务和本地 socket 均使用 root。")
	settings, err := platformsettings.Read(app)
	if err != nil {
		return err
	}
	platformName, err := p.read("平台名称", settings.Name, false)
	if err != nil {
		return err
	}
	email := "admin@example.com"
	bindings, err := app.FindRecordsByFilter("user_platform_roles", "role.code = 'super_admin' && user.active = true", "id", 1, 0)
	if err != nil {
		return err
	}
	if len(bindings) > 0 {
		u, err := app.FindRecordById("users", bindings[0].GetString("user"))
		if err != nil {
			return err
		}
		email = u.Email()
	}
	email, err = p.read("管理员邮箱", email, false)
	if err != nil {
		return err
	}
	admin, err := app.FindAuthRecordByEmail("users", email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	newAdmin := admin == nil
	if newAdmin {
		collection, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		admin = core.NewRecord(collection)
		admin.SetEmail(email)
		admin.Set("auth_source", "local")
	} else if !admin.GetBool("active") {
		return fmt.Errorf("该邮箱属于已停用账户，请选择活动账户或新邮箱")
	}
	name := admin.GetString("name")
	if name == "" {
		name = "管理员"
	}
	name, err = p.read("管理员显示名称", name, false)
	if err != nil {
		return err
	}
	password, err := p.read("管理员密码（首次留空自动生成）", "", true)
	if err != nil {
		return err
	}
	generated := ""
	if !newAdmin && admin.GetString("auth_source") != "local" && password != "" {
		return fmt.Errorf("外部认证账户的密码由身份来源管理；请回车保留，不在终端向导中修改")
	}
	if password != "" {
		confirmation, err := p.read("再次输入密码", "", true)
		if err != nil {
			return err
		}
		if confirmation != password {
			return fmt.Errorf("两次密码不一致")
		}
	} else if newAdmin {
		generated = rand.Text()
		password = generated
	}
	if password != "" {
		admin.SetPassword(password)
	}
	admin.Set("name", name)
	admin.Set("active", true)
	admin.Set("management_revision", admin.GetInt("management_revision")+1)

	envPath := filepath.Join(configDir, "slurm-console.env")
	env, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	profilesPath := envValue(string(env), "SLURM_CONSOLE_CLUSTER_PROFILES")
	if profilesPath == "" {
		profilesPath = filepath.Join(configDir, "cluster-profiles.json")
	}
	registry := &clusterconfig.Registry{Profiles: map[string]clusterconfig.Profile{}}
	if _, err := os.Lstat(profilesPath); err == nil {
		registry, err = clusterconfig.LoadRegistry(profilesPath)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	choice, err := p.read("配置 Slurm 连接？y/n", "n", false)
	if err != nil {
		return err
	}
	if choice != "y" && choice != "n" {
		return fmt.Errorf("请输入 y 或 n")
	}
	var connection *clusterconfig.Input
	var existingID string
	var tokenPath, token string
	if choice == "y" {
		ref := "primary"
		if views := registry.Views(); len(views) > 0 {
			ref = views[0].Ref
		}
		ref, err = p.read("连接标识", ref, false)
		if err != nil {
			return err
		}
		profile, exists := registry.Profiles[ref]
		if !exists {
			profile = clusterconfig.Profile{Label: "默认集群", Endpoint: defaultSocket(), AuthType: "local", LocalUser: "root", Versions: []string{"v0.0.43"}}
		}
		existing, findErr := app.FindFirstRecordByFilter("clusters", "connection_ref = {:ref}", dbx.Params{"ref": ref})
		if findErr != nil && !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		defaultVersion := profile.Versions[0]
		if existing != nil {
			profile.Label = existing.GetString("name")
			defaultVersion = existing.GetString("api_version")
		}
		profile.Label, err = p.read("集群名称", profile.Label, false)
		if err != nil {
			return err
		}
		profile.Endpoint, err = p.read("Slurm REST 地址（unix:///绝对路径 或 https://主机）", profile.Endpoint, false)
		if err != nil {
			return err
		}
		version, err := p.read("Slurm API 版本", defaultVersion, false)
		if err != nil {
			return err
		}
		if !slices.Contains(profile.Versions, version) {
			profile.Versions = append(profile.Versions, version)
		}
		if strings.HasPrefix(profile.Endpoint, "unix:") {
			profile.AuthType, profile.LocalUser, profile.Username, profile.TokenFile, profile.CAFile = "local", "root", "", "", ""
		} else {
			profile.AuthType, profile.LocalUser = "jwt", ""
			profile.Username, err = p.read("Slurm JWT 用户名", profile.Username, false)
			if err != nil {
				return err
			}
			profile.CAFile, err = p.read("自定义 CA 文件（空值使用系统 CA）", profile.CAFile, false)
			if err != nil {
				return err
			}
			token, err = p.read("JWT（回车保留现有凭据）", "", true)
			if err != nil {
				return err
			}
			if token != "" {
				if strings.ContainsAny(token, "\r\n\x00") {
					return fmt.Errorf("JWT 格式不合法")
				}
				// Validate the reference before using it in a credential filename.
				if filepath.Base(ref) != ref || strings.ContainsAny(ref, "/\\") {
					return fmt.Errorf("连接标识不合法")
				}
				tokenPath = filepath.Join(configDir, ref+".jwt")
				profile.TokenFile = tokenPath
			} else if _, err := os.Stat(profile.TokenFile); err != nil {
				return fmt.Errorf("HTTPS 连接需要 JWT；首次配置不能留空")
			}
		}
		registry.Profiles[ref] = profile
		registry, err = clusterconfig.NewRegistry(registry.Profiles)
		if err != nil {
			return fmt.Errorf("连接配置无效: %w", err)
		}
		in := clusterconfig.Input{Name: profile.Label, Slug: ref, ConnectionRef: ref, APIVersion: version, Enabled: true, TimeoutSeconds: 5}
		if existing != nil {
			existingID = existing.Id
			in.Slug, in.Revision = existing.GetString("slug"), existing.GetInt("revision")
			in.Description, in.Enabled = existing.GetString("description"), existing.GetBool("enabled")
			if existing.GetInt("timeout_seconds") > 0 {
				in.TimeoutSeconds = existing.GetInt("timeout_seconds")
			}
		}
		in.Slug, err = p.read("集群标识（创建后保持不变）", in.Slug, false)
		if err != nil {
			return err
		}
		manager := &clusterconfig.Manager{App: app, Registry: registry}
		if _, err = manager.Preview(existingID, in); err != nil {
			return fmt.Errorf("集群配置校验失败: %w", err)
		}
		connection = &in
	}

	// Account, authorization, settings, cluster and audit commit together. The
	// surrounding installer snapshot also rolls back configuration-file failures.
	err = app.RunInTransaction(func(tx core.App) error {
		if err := tx.Save(admin); err != nil {
			return fmt.Errorf("管理员保存失败（密码须为 12–71 字符）: %w", err)
		}
		if _, err := rbac.Grant(tx, admin.Id, "super_admin", ""); err != nil {
			return err
		}
		r, err := tx.FindRecordById("platform_settings", platformsettings.ID)
		if err != nil {
			return err
		}
		if r.GetString("name") != platformName {
			r.Set("name", platformName)
			r.Set("revision", r.GetInt("revision")+1)
			if err = tx.Save(r); err != nil {
				return err
			}
		}
		if connection != nil {
			manager := &clusterconfig.Manager{App: tx, Registry: registry}
			view, err := manager.Save(existingID, *connection)
			if err != nil {
				return err
			}
			action := "clusters.update"
			if existingID == "" {
				action = "clusters.create"
			}
			if err = audit.Write(tx, audit.LocalActor(), audit.Event{Action: action, ClusterID: view.ID, TargetType: "cluster", TargetID: view.ID, Message: "root 终端向导保存连接配置。"}); err != nil {
				return err
			}
		}
		return audit.Write(tx, audit.LocalActor(), audit.Event{Action: "platform.configure", TargetType: "user", TargetID: admin.Id, Message: "root 终端向导配置平台及管理员；未记录密码或 JWT。"})
	})
	if err != nil {
		return err
	}
	if connection != nil {
		if token != "" {
			if err = writePrivate(tokenPath, []byte(token)); err != nil {
				return err
			}
		}
		data, err := json.MarshalIndent(registry, "", "  ")
		if err != nil {
			return err
		}
		if err = writePrivate(profilesPath, append(data, '\n')); err != nil {
			return err
		}
		env = []byte(setEnv(string(env), "SLURM_CONSOLE_CLUSTER_PROFILES", profilesPath))
		if err = writePrivate(envPath, env); err != nil {
			return err
		}
	}
	if generated != "" {
		fmt.Fprintf(p.out, "首次管理员密码（仅显示本次，请保存）: %s\n", generated)
	}
	fmt.Fprintf(p.out, "配置完成，管理员: %s。未修改其他集成；连接可在登录后检测。\n", email)
	return nil
}

func defaultSocket() string {
	for _, path := range []string{"/run/slurmrestd/slurmrestd.sock", "/run/slurmrestd.sock", "/var/run/slurmrestd/slurmrestd.socket"} {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return "unix://" + path
		}
	}
	return "unix:///run/slurmrestd/slurmrestd.sock"
}

func envValue(data, key string) string {
	value := ""
	for _, line := range strings.Split(data, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k == key {
			value = strings.TrimSpace(v)
			if strings.HasPrefix(value, "\"") {
				if unquoted, err := strconv.Unquote(value); err == nil {
					value = unquoted
				}
			} else {
				value = strings.Trim(value, "'")
			}
		}
	}
	return value
}

func setEnv(data, key, value string) string {
	lines := []string{}
	for _, line := range strings.Split(data, "\n") {
		k, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k == key {
			continue
		}
		lines = append(lines, line)
	}
	// JSON quoting is also compatible with systemd EnvironmentFile escaping.
	encoded, _ := json.Marshal(value)
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n" + key + "=" + string(encoded) + "\n"
}

func writePrivate(path string, data []byte) error {
	if strings.ContainsAny(path, "\r\n\x00") || !filepath.IsAbs(path) {
		return fmt.Errorf("配置路径必须是绝对路径")
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || !maintenance.OwnedBy(info, os.Geteuid())) {
		return fmt.Errorf("不安全的配置文件")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".configure-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
