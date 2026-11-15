package distribution

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uninstallFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := []string{
		"/opt/slurm-console/slurm-console", "/opt/slurm-console/public/index.html",
		"/var/lib/slurm-console/pb_data/data.db", "/etc/slurm-console/slurm-console.env",
		"/var/backups/slurm-console/daily.tar.gz.age", "/var/lib/slurm-console-backup/identity.txt",
		"/usr/local/libexec/slurm-console-pam-helper", "/etc/pam.d/slurm-console",
		"/etc/sudoers.d/slurm-console-pam", "/etc/sudoers.d/slurm-console-linux-users",
		"/etc/pam.d/password-auth", "/etc/slurm/slurm.conf", "/etc/passwd",
	}
	for _, base := range []string{"/etc/systemd/system", "/run/systemd/system"} {
		for _, unit := range uninstallUnits {
			files = append(files, base+"/"+unit, base+"/"+unit+".d/override.conf")
		}
	}
	for _, path := range files {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("preserve this content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	wants := filepath.Join(root, "etc/systemd/system/custom.target.wants")
	if err := os.MkdirAll(wants, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../slurm-console.service", filepath.Join(wants, "slurm-console.service")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wants, "unrelated.service"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func uninstallChoices(t *testing.T, answers string) []uninstallData {
	t.Helper()
	data, confirmed, err := promptUninstall(&prompts{in: bufio.NewReader(strings.NewReader(answers)), out: io.Discard})
	if err != nil || !confirmed {
		t.Fatalf("uninstall choices: confirmed=%v err=%v", confirmed, err)
	}
	return data
}

func TestUninstallRetainsOnlySelectedData(t *testing.T) {
	for mask := range 8 {
		t.Run(fmt.Sprintf("keep-%03b", mask), func(t *testing.T) {
			root := uninstallFixture(t)
			var answers strings.Builder
			for i := range 3 {
				if mask&(1<<i) != 0 {
					answers.WriteString("y\n")
				} else {
					answers.WriteString("n\n")
				}
			}
			answers.WriteString("UNINSTALL\n")
			data := uninstallChoices(t, answers.String())
			service := func(args ...string) (string, error) { return "loaded", nil }
			if err := uninstallAt(root, data, service, io.Discard); err != nil {
				t.Fatal(err)
			}
			for i, group := range data {
				for _, path := range group.paths {
					_, err := os.Stat(filepath.Join(root, path))
					if mask&(1<<i) != 0 && err != nil || mask&(1<<i) == 0 && !os.IsNotExist(err) {
						t.Fatalf("retention mismatch for %s: %v", path, err)
					}
				}
			}
			for _, path := range []string{
				"/opt/slurm-console", "/usr/local/libexec/slurm-console-pam-helper", "/etc/pam.d/slurm-console",
				"/etc/sudoers.d/slurm-console-pam", "/etc/sudoers.d/slurm-console-linux-users",
				"/etc/systemd/system/custom.target.wants/slurm-console.service",
			} {
				if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
					t.Fatalf("application artifact remains at %s: %v", path, err)
				}
			}
			for _, base := range []string{"/etc/systemd/system", "/run/systemd/system"} {
				for _, unit := range uninstallUnits {
					for _, suffix := range []string{"", ".d"} {
						if _, err := os.Lstat(filepath.Join(root, base, unit+suffix)); !os.IsNotExist(err) {
							t.Fatalf("unit artifact remains: %s/%s%s: %v", base, unit, suffix, err)
						}
					}
				}
			}
			for _, path := range []string{"/etc/pam.d/password-auth", "/etc/slurm/slurm.conf", "/etc/passwd"} {
				content, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(content) != "preserve this content" {
					t.Fatalf("unrelated file changed: %s: %v", path, err)
				}
			}
			if content, err := os.ReadFile(filepath.Join(root, "etc/systemd/system/custom.target.wants/unrelated.service")); err != nil || string(content) != "keep" {
				t.Fatal("unrelated unit changed", err)
			}
			// A second uninstall tolerates absent units and already removed paths.
			missing := func(args ...string) (string, error) { return "not-found", nil }
			if err := uninstallAt(root, data, missing, io.Discard); err != nil {
				t.Fatal("repeat uninstall", err)
			}
		})
	}
}

func TestUninstallDefaultsAndCancellation(t *testing.T) {
	data := uninstallChoices(t, "invalid\n\n\n\nUNINSTALL\n")
	for _, group := range data {
		if !group.keep {
			t.Fatal("default did not retain", group.label)
		}
	}
	for _, input := range []string{"", "n\nn\nn\n", "n\nn\nn\n\n", "n\nn\nn\nyes\n"} {
		_, confirmed, _ := promptUninstall(&prompts{in: bufio.NewReader(strings.NewReader(input)), out: io.Discard})
		if confirmed {
			t.Fatalf("incomplete or unconfirmed input accepted: %q", input)
		}
	}
}

func TestUninstallAbortsBeforeDeletionIfSystemdFails(t *testing.T) {
	for _, failure := range []string{"show", "stop", "disable"} {
		t.Run(failure, func(t *testing.T) {
			root := uninstallFixture(t)
			data := uninstallChoices(t, "n\nn\nn\nUNINSTALL\n")
			service := func(args ...string) (string, error) {
				if args[0] == failure {
					return "", errors.New("service manager failure")
				}
				return "loaded", nil
			}
			if err := uninstallAt(root, data, service, io.Discard); err == nil {
				t.Fatal("systemd failure ignored")
			}
			for _, path := range []string{"/opt/slurm-console/slurm-console", "/var/lib/slurm-console/pb_data/data.db", "/etc/slurm-console/slurm-console.env", "/var/lib/slurm-console-backup/identity.txt"} {
				content, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(content) != "preserve this content" {
					t.Fatalf("file removed after failed service operation: %s: %v", path, err)
				}
			}
		})
	}
}

func TestUninstallDoesNotFollowSymlinks(t *testing.T) {
	for _, parentLink := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent-%v", parentLink), func(t *testing.T) {
			root := uninstallFixture(t)
			outside := t.TempDir()
			protected := filepath.Join(outside, "slurm-console")
			if err := os.WriteFile(protected, []byte("external file"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "etc/pam.d/slurm-console")
			target := protected
			if parentLink {
				link = filepath.Dir(link)
				target = outside
			}
			if err := os.RemoveAll(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			serviceCalls := 0
			service := func(args ...string) (string, error) { serviceCalls++; return "loaded", nil }
			err := uninstallAt(root, uninstallChoices(t, "n\nn\nn\nUNINSTALL\n"), service, io.Discard)
			if parentLink && (err == nil || serviceCalls != 0) || !parentLink && err != nil {
				t.Fatal("unexpected symlink handling", err, serviceCalls)
			}
			content, err := os.ReadFile(protected)
			if err != nil || string(content) != "external file" {
				t.Fatal("external symlink target changed", err)
			}
		})
	}
}

func TestUninstallSupportsRelativeSystemDirectoryLinks(t *testing.T) {
	root := uninstallFixture(t)
	for _, pair := range [][2]string{{"opt", "var/opt"}, {"usr/local", "var/usrlocal"}} {
		if err := os.Rename(filepath.Join(root, pair[0]), filepath.Join(root, pair[1])); err != nil {
			t.Fatal(err)
		}
		target, err := filepath.Rel(filepath.Dir(pair[0]), pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, pair[0])); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := filepath.Join(root, "var/opt/unrelated")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	service := func(args ...string) (string, error) { return "loaded", nil }
	if err := uninstallAt(root, uninstallChoices(t, "n\nn\nn\nUNINSTALL\n"), service, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"var/opt/slurm-console", "var/usrlocal/libexec/slurm-console-pam-helper"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatal("artifact behind relative directory link remains", path, err)
		}
	}
	if content, err := os.ReadFile(unrelated); err != nil || string(content) != "keep" {
		t.Fatal("unrelated directory content changed", err)
	}
}
