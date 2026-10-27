package backup

import (
	"filippo.io/age"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slurm-console/backend/internal/maintenance"
	"sort"
)

var dailyName = regexp.MustCompile(`^daily-[0-9]{8}T[0-9]{6}Z-[A-Z2-7]{10}\.tar\.gz\.age$`)

// Only authenticated snapshots in the explicitly managed daily namespace qualify.
func Prune(directory string, keep int, identity age.Identity) (int, error) {
	if keep < 1 {
		return 0, fmt.Errorf("daily retention must keep at least one snapshot")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	names := []string{}
	for _, e := range entries {
		if !dailyName.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return 0, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !maintenance.OwnedBy(info, os.Geteuid()) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	removed := 0
	for _, name := range names[min(keep, len(names)):] {
		file := filepath.Join(directory, name)
		m, err := Verify(file, identity)
		if err != nil || m.Purpose != "snapshot" {
			return removed, fmt.Errorf("daily backup could not be authenticated; retained: %s", name)
		}
		if err := os.Remove(file); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
