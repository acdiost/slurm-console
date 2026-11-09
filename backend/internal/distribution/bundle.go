// Package distribution embeds the frontend and existing verified installer.
package distribution

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

//go:embed all:assets
var payload embed.FS

func Public() fs.FS {
	public, err := fs.Sub(payload, "assets/public")
	if err != nil {
		return nil
	}
	return public
}

// Dispatch runs before PocketBase opens a database or reads integration config.
func Dispatch(args []string, configure func(string) error) (bool, error) {
	if len(args) == 0 || (args[0] != "install" && args[0] != "configure" && args[0] != "configure-data") {
		return false, nil
	}
	root := Command(configure)
	root.SetArgs(args)
	return true, root.Execute()
}

func Command(configure func(string) error) *cobra.Command {
	root := &cobra.Command{Use: "slurm-console", SilenceErrors: true, SilenceUsage: true}
	for _, name := range []string{"install", "configure"} {
		root.AddCommand(&cobra.Command{Use: name, Short: "Install and configure interactively as root on Linux", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			if runtime.GOOS != "linux" || os.Geteuid() != 0 {
				return fmt.Errorf("installation and configuration require root on Linux")
			}
			return install()
		}})
	}
	var dir string
	internal := &cobra.Command{Use: "configure-data", Hidden: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		if runtime.GOOS != "linux" || os.Geteuid() != 0 || os.Getenv("SLURM_CONSOLE_LOCK_FD") == "" {
			return fmt.Errorf("configuration must run through the root installer")
		}
		return configure(dir)
	}}
	internal.Flags().StringVar(&dir, "dir", "/var/lib/slurm-console/pb_data", "Application database directory")
	root.AddCommand(internal)
	return root
}

func install() error {
	if _, err := fs.Stat(payload, "assets/public/index.html"); err != nil {
		return fmt.Errorf("installer payload missing: build with make release")
	}
	stage, err := os.MkdirTemp("", "slurm-console-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	err = fs.WalkDir(payload, "assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() == ".keep" {
			return nil
		}
		rel, err := filepath.Rel("assets", path)
		if err != nil {
			return err
		}
		if len(rel) > 7 && rel[:7] == "public/" {
			rel = filepath.Join("frontend/dist", rel[7:])
		}
		target := filepath.Join(stage, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		data, err := payload.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(stage, "backend"), 0700); err != nil {
		return err
	}
	// A private staging copy lets the installer atomically replace a running binary.
	source, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(filepath.Join(stage, "backend/slurm-console"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	cmd := exec.Command("bash", filepath.Join(stage, "deploy/install.sh"), "--configure")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		return err
	}
	return nil
}
