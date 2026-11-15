package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/pamauth"
)

const (
	pamSettingsCollection = "pam_settings"
	PAMSettingsID         = "pamsettings0001"
	pamStateKey           = "platform.pamSetup"
)

var (
	ErrPAMUnavailable = errors.New("PAM prerequisites unavailable")
	ErrPAMConflict    = errors.New("PAM settings revision conflict")
	pamInitMu         sync.Mutex
)

// PAMStatus reports runtime activation separately from installation readiness.
// It contains no executable paths or provider diagnostics.
type PAMStatus struct {
	Revision        int    `json:"revision"`
	Configured      bool   `json:"configured"`
	Available       bool   `json:"available"`
	Supported       bool   `json:"supported"`
	HelperInstalled bool   `json:"helperInstalled"`
	PolicyInstalled bool   `json:"policyInstalled"`
	Message         string `json:"message"`
}

type pamRuntime struct {
	mu        sync.Mutex
	hooks     sync.Once
	lifecycle sync.Once
	inspect   func(context.Context) (PAMStatus, pamauth.Provider)
}

func pamState(app core.App) *pamRuntime {
	pamInitMu.Lock()
	defer pamInitMu.Unlock()
	if state, ok := app.Store().Get(pamStateKey).(*pamRuntime); ok {
		return state
	}
	state := &pamRuntime{inspect: inspectPAM}
	app.Store().Set(pamStateKey, state)
	return state
}

func pamEnabled(record *core.Record) (bool, error) {
	if record.GetBool("has_override") {
		return record.GetBool("enabled"), nil
	}
	raw := os.Getenv("SLURM_CONSOLE_PAM_ENABLED")
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%w: 请修正服务环境中的 SLURM_CONSOLE_PAM_ENABLED 布尔值。", ErrPAMUnavailable)
	}
	return enabled, nil
}

func inspectPAM(ctx context.Context) (PAMStatus, pamauth.Provider) {
	status := PAMStatus{Supported: runtime.GOOS == "linux" && os.Geteuid() == 0}
	path := os.Getenv("SLURM_CONSOLE_PAM_HELPER")
	if path == "" {
		path = "/usr/local/libexec/slurm-console-pam-helper"
	}
	status.HelperInstalled = filepath.IsAbs(path) && filepath.Clean(path) == path && pamFileReady(path, true)
	status.PolicyInstalled = pamFileReady("/etc/pam.d/slurm-console", false)
	switch {
	case runtime.GOOS != "linux":
		status.Message = "PAM 需要 Linux 后端。请在 Linux 主机安装应用认证组件后重新检测；当前可继续使用本地管理员账户。"
	case !status.HelperInstalled:
		status.Message = "PAM helper 缺失或权限不安全。请在后端主机构建 make pam-helper，并以 root 运行 deploy/setup-pam.sh 安装后重新检测。"
	case !status.PolicyInstalled:
		status.Message = "应用专属 PAM 策略缺失或权限不安全。请以 root 运行 deploy/setup-pam.sh 安装并检查应用策略后重新检测。"
	default:
		sudo := os.Geteuid() != 0
		if raw := os.Getenv("SLURM_CONSOLE_PAM_SUDO"); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				status.Message = "请修正服务环境中的 SLURM_CONSOLE_PAM_SUDO 布尔值后重新检测。"
				return status, nil
			}
			sudo = value && os.Geteuid() != 0
		}
		provider, err := pamauth.NewProcess(path, sudo)
		if err == nil {
			result, probeErr := provider.Run(ctx, pamauth.Request{Mode: "probe"})
			if probeErr == nil && result.OK {
				status.Available = true
				status.Message = "PAM 认证组件检测通过。启用后请在用户管理中关联已有系统账户；本地管理员登录继续可用。"
				if !status.Supported {
					status.Message = "已检测到旧版 sudo PAM 集成。向导启用需要 Linux 后端以 root 运行；现有环境配置仍有效。"
				}
				return status, provider
			}
		}
		status.Message = "PAM helper 探测失败。请确认已安装 Linux CGO/PAM 构建、应用策略和执行权限，检查服务日志后重新检测。"
	}
	return status, nil
}

// CheckPAMSetup performs the same full, password-free helper probe used before
// activation. Missing prerequisites are represented by an actionable status.
func CheckPAMSetup(ctx context.Context, app core.App) (PAMStatus, error) {
	state := pamState(app)
	state.mu.Lock()
	defer state.mu.Unlock()
	record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
	if err != nil {
		return PAMStatus{}, err
	}
	status, _ := state.inspect(ctx)
	status.Revision = record.GetInt("revision")
	status.Configured = app.Store().Get(pamProviderKey) != nil
	if !status.Configured && status.Available {
		status.Message = "PAM 认证组件检测通过，当前未启用。启用后可在用户管理中关联已有系统账户；本地管理员登录继续可用。"
	}
	return status, nil
}

func PAMSetupStatus(app core.App) (PAMStatus, error) {
	return CheckPAMSetup(context.Background(), app)
}

// ApplyPAMSetup reloads the saved override, falling back to the administrator's
// environment only when no override exists. An unavailable provider fails closed
// while local authentication remains available to repair the installation.
func ApplyPAMSetup(app core.App) error {
	state := pamState(app)
	state.mu.Lock()
	defer state.mu.Unlock()
	record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
	if err != nil {
		return err
	}
	enabled, err := pamEnabled(record)
	if err != nil {
		RegisterPAM(app, nil)
		return err
	}
	if !enabled {
		RegisterPAM(app, nil)
		return nil
	}
	status, provider := state.inspect(context.Background())
	if !status.Available || provider == nil {
		RegisterPAM(app, nil)
		return fmt.Errorf("%w: %s", ErrPAMUnavailable, status.Message)
	}
	RegisterPAM(app, provider)
	return nil
}

// SavePAMSetup serializes probing, persistence and runtime activation. The fresh
// authorization callback runs inside the audited transaction; a failed probe,
// revocation or audit write leaves both the saved and running provider unchanged.
func SavePAMSetup(ctx context.Context, app core.App, enabled bool, revision int, actor audit.Actor, authorize func(core.App) error) (PAMStatus, error) {
	state := pamState(app)
	state.mu.Lock()
	defer state.mu.Unlock()
	record, err := app.FindRecordById(pamSettingsCollection, PAMSettingsID)
	if err != nil {
		return PAMStatus{}, err
	}
	status := PAMStatus{Revision: record.GetInt("revision"), Configured: app.Store().Get(pamProviderKey) != nil}
	if status.Revision != revision {
		return status, ErrPAMConflict
	}
	readiness, provider := state.inspect(ctx)
	readiness.Revision, readiness.Configured = status.Revision, status.Configured
	status = readiness
	if enabled && (!status.Supported || !status.Available || provider == nil) {
		if !status.Supported {
			status.Message = "向导启用 PAM 需要以 root 运行的 Linux 后端。请调整后端运行环境后重新检测；本地管理员登录继续可用。"
		}
		return status, fmt.Errorf("%w: %s", ErrPAMUnavailable, status.Message)
	}
	err = audit.Mutate(app, actor, func(tx core.App) (audit.Event, error) {
		if err := authorize(tx); err != nil {
			return audit.Event{}, err
		}
		current, err := tx.FindRecordById(pamSettingsCollection, PAMSettingsID)
		if err != nil {
			return audit.Event{}, err
		}
		if current.GetInt("revision") != revision {
			return audit.Event{}, ErrPAMConflict
		}
		current.Set("has_override", true)
		current.Set("enabled", enabled)
		current.Set("revision", revision+1)
		if err := tx.Save(current); err != nil {
			return audit.Event{}, err
		}
		if err := authorize(tx); err != nil {
			return audit.Event{}, err
		}
		return audit.Event{Action: "platform.pam.update", TargetType: "pam_settings", TargetID: PAMSettingsID, Message: "已保存系统认证启用状态；本地管理员认证方式保持可用。", Changes: []audit.Change{{Field: "enabled", Before: strconv.FormatBool(status.Configured), After: strconv.FormatBool(enabled)}, {Field: "revision", Before: strconv.Itoa(revision), After: strconv.Itoa(revision + 1)}}}, nil
	})
	if err != nil {
		return status, err
	}
	if !enabled {
		provider = nil
	}
	RegisterPAM(app, provider)
	status.Revision = revision + 1
	status.Configured = enabled
	if enabled {
		status.Message = "PAM 已启用。请在用户管理中关联已有系统账户；本地管理员登录继续可用。"
	} else {
		status.Message = "PAM 已停用。本地管理员登录继续可用；已关联 PAM 的账户暂时无法登录。"
	}
	return status, nil
}
