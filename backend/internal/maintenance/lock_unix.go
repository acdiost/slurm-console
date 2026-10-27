//go:build linux || darwin

// Package maintenance coordinates deployment and trusted administration commands.
package maintenance

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

const LockPath = "/run/lock/slurm-console.lock"

func acquire(exclusive bool) (func(), error) { return acquireAt(LockPath, exclusive, 0) }
func acquireAt(lockPath string, exclusive bool, expectedUID uint32) (func(), error) {
	if fdText := os.Getenv("SLURM_CONSOLE_LOCK_FD"); fdText != "" {
		fd, err := strconv.Atoi(fdText)
		if err != nil || fd < 3 {
			return nil, fmt.Errorf("invalid inherited maintenance lock")
		}
		file := os.NewFile(uintptr(fd), "inherited-lock")
		info, err := file.Stat()
		expected, e := os.Stat(lockPath)
		if err != nil || e != nil || !os.SameFile(info, expected) {
			return nil, fmt.Errorf("invalid inherited maintenance lock")
		}
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return nil, fmt.Errorf("maintenance lock unavailable: %w", err)
		}
		return func() { _ = file.Close() }, nil
	}
	flags := os.O_RDONLY
	if exclusive {
		flags = os.O_CREATE | os.O_RDWR
	}
	file, err := os.OpenFile(lockPath, flags|syscall.O_NOFOLLOW, 0644)
	if os.IsNotExist(err) && !exclusive {
		return func() {}, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		file.Close()
		return nil, fmt.Errorf("unsafe maintenance lock")
	}
	operation := syscall.LOCK_SH
	if exclusive {
		operation = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(file.Fd()), operation|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("maintenance in progress; retry after backup/deployment finishes")
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}
func Shared() (func(), error)    { return acquire(false) }
func Exclusive() (func(), error) { return acquire(true) }

func OwnedBy(info os.FileInfo, uid int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid
}
