//go:build !linux

package auth

func pamFileReady(string, bool) bool { return false }
