//go:build linux

package auth

// Privileged helpers and policies must not be replaceable by unprivileged users.
func pamFileReady(path string, executable bool) bool {
	return defaultPAMHost().ready(path, executable, false)
}
