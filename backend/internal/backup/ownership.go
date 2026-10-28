package backup

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// A rollback must use the account in the archived unit, including pre-root
// releases. Never infer database ownership from the currently installed unit.
func runtimeOwnership(stage string) (int, int, error) {
	raw, err := os.ReadFile(filepath.Join(stage, "etc/systemd/system/slurm-console.service"))
	if err != nil {
		return 0, 0, err
	}
	name, err := runtimeAccount(string(raw))
	if err != nil {
		return 0, 0, err
	}
	if name == "root" {
		return 0, 0, nil
	}
	account, err := user.Lookup(name)
	if err != nil {
		return 0, 0, err
	}
	uid, e1 := strconv.Atoi(account.Uid)
	gid, e2 := strconv.Atoi(account.Gid)
	if e1 != nil || e2 != nil || uid <= 0 || gid <= 0 {
		return 0, 0, fmt.Errorf("legacy non-root service account is required")
	}
	return uid, gid, nil
}

func runtimeAccount(unit string) (string, error) {
	section, name, group := "", "", ""
	valid := true
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[Service]" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "User":
			if name != "" {
				valid = false
			}
			name = strings.TrimSpace(value)
		case "Group":
			if group != "" {
				valid = false
			}
			group = strings.TrimSpace(value)
		}
	}
	if !valid || name != group || (name != "root" && name != "slurm-console") {
		return "", fmt.Errorf("backup service runtime account is unsupported")
	}
	return name, nil
}
