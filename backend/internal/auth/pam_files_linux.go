//go:build linux

package auth

import (
	"os"
	"path/filepath"
	"syscall"
)

// Privileged helpers and policies must not be replaceable by unprivileged users.
func pamFileReady(path string, executable bool) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || executable && info.Mode().Perm()&0100 == 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return false
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		info, err = os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return false
		}
		stat, ok = info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return false
		}
		if dir == "/" {
			return true
		}
	}
}
