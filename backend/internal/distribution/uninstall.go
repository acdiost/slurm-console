package distribution

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"slurm-console/backend/internal/backup"
	"slurm-console/backend/internal/maintenance"
)

type uninstallData struct {
	label string
	paths []string
	keep  bool
}

var uninstallUnits = []string{"slurm-console-backup.timer", "slurm-console-backup.service", "slurm-console.service"}

func uninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use: "uninstall", Short: "Uninstall on Linux, asking which application data to retain", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if runtime.GOOS != "linux" || os.Geteuid() != 0 {
				return fmt.Errorf("uninstallation requires root on Linux")
			}
			p := &prompts{in: bufio.NewReader(command.InOrStdin()), out: command.OutOrStdout()}
			data, confirmed, err := promptUninstall(p)
			if err != nil || !confirmed {
				return err
			}
			unlock, err := maintenance.Exclusive()
			if err != nil {
				return err
			}
			defer unlock()
			run := func(args ...string) (string, error) {
				ctx, cancel := context.WithTimeout(command.Context(), 45*time.Second)
				defer cancel()
				output, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
				if err != nil {
					return "", fmt.Errorf("systemctl %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
				}
				return strings.TrimSpace(string(output)), nil
			}
			return uninstallAt("/", data, run, p.out)
		},
	}
}

func promptUninstall(p *prompts) ([]uninstallData, bool, error) {
	data := []uninstallData{
		{label: "平台数据（账户、授权、集群记录、审计和数据库文件）", paths: []string{"/var/lib/slurm-console"}},
		{label: "应用配置与凭据（环境文件、集成配置、证书和令牌）", paths: []string{"/etc/slurm-console"}},
		{label: "加密备份及其解密密钥（删除后无法从这些备份恢复）", paths: []string{backup.DefaultDirectory, filepath.Dir(backup.DefaultKey)}},
	}
	fmt.Fprintln(p.out, "卸载将停止并禁用 Slurm Console 和备份任务，删除程序、专属 systemd 配置、PAM helper/策略及旧版专属 sudo 规则。")
	fmt.Fprintln(p.out, "Slurm、外部身份服务、系统账户及安装目录以外的文件由管理员维护。以下关键数据默认保留在原路径。")
	for i := range data {
		for {
			answer, err := p.read("是否保留"+data[i].label+"（"+strings.Join(data[i].paths, "、")+"）? y/n", "y", false)
			if err != nil {
				return nil, false, err
			}
			switch strings.ToLower(answer) {
			case "y", "yes", "是":
				data[i].keep = true
			case "n", "no", "否":
				data[i].keep = false
			default:
				fmt.Fprintln(p.out, "请输入 y 或 n。")
				continue
			}
			break
		}
	}
	fmt.Fprintln(p.out, "卸载选择：")
	for _, group := range data {
		action := "删除"
		if group.keep {
			action = "保留"
		}
		fmt.Fprintf(p.out, "  %s：%s\n", action, strings.Join(group.paths, "、"))
	}
	answer, err := p.read("确认卸载请输入 UNINSTALL，其他输入取消", "取消", false)
	if err != nil {
		return nil, false, err
	}
	if answer != "UNINSTALL" {
		fmt.Fprintln(p.out, "已取消卸载，未停止服务或删除文件。")
		return nil, false, nil
	}
	return data, true, nil
}

// uninstallAt operates only on this application's fixed installation paths.
// root permits exercising actual removal in an isolated filesystem fixture.
func uninstallAt(root string, data []uninstallData, systemctl func(...string) (string, error), out io.Writer) error {
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer rootFS.Close()
	paths := []string{
		"/usr/local/libexec/slurm-console-pam-helper",
		"/etc/pam.d/slurm-console",
		"/etc/sudoers.d/slurm-console-pam",
		"/etc/sudoers.d/slurm-console-linux-users",
	}
	for _, base := range []string{"/etc/systemd/system", "/run/systemd/system"} {
		for _, unit := range uninstallUnits {
			paths = append(paths, base+"/"+unit, base+"/"+unit+".d")
		}
		entries, err := os.ReadDir(filepath.Join(root, base))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !(strings.HasSuffix(entry.Name(), ".wants") || strings.HasSuffix(entry.Name(), ".requires") || strings.HasSuffix(entry.Name(), ".upholds")) {
				continue
			}
			for _, unit := range uninstallUnits {
				paths = append(paths, base+"/"+entry.Name()+"/"+unit)
			}
		}
	}
	for _, group := range data {
		if !group.keep {
			paths = append(paths, group.paths...)
		}
	}
	paths = append(paths, "/opt/slurm-console")
	// Validate traversal before stopping services. Root allows ordinary relative
	// directory links (e.g. /opt -> var/opt) while preventing sandbox escapes.
	for _, path := range paths {
		if _, err := rootFS.Lstat(strings.TrimPrefix(path, "/")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("unsafe uninstall path %s: %w", path, err)
		}
	}
	// Stop the timer before its worker and the application. Any failure aborts
	// removal so a live service never loses its configuration or database.
	for _, unit := range uninstallUnits {
		state, err := systemctl("show", unit, "--property=LoadState", "--value")
		if err != nil {
			return err
		}
		if state == "not-found" {
			continue
		}
		if state == "" {
			return fmt.Errorf("cannot determine systemd state for %s", unit)
		}
		if _, err := systemctl("stop", unit); err != nil {
			return err
		}
		if _, err := systemctl("disable", unit); err != nil {
			return err
		}
	}
	for _, path := range paths {
		if err := rootFS.RemoveAll(strings.TrimPrefix(path, "/")); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if _, err := systemctl("daemon-reload"); err != nil {
		return err
	}
	fmt.Fprintln(out, "Slurm Console 已卸载。")
	for _, group := range data {
		if group.keep {
			fmt.Fprintf(out, "保留数据（如存在）：%s\n", strings.Join(group.paths, "、"))
		}
	}
	return nil
}
