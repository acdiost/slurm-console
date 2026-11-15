//go:build linux && (amd64 || arm64)

package main

import (
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"slurm-console/backend/internal/pamauth"
)

const service = "slurm-console"

func run(request pamauth.Request) (result pamauth.Result) {
	denied := pamauth.Result{Code: "denied"}
	unavailable := pamauth.Result{Code: "unavailable"}
	if os.Geteuid() != 0 {
		return unavailable
	}
	if request.Mode != "capabilities" && request.Mode != "probe" && request.Mode != "authenticate" && request.Mode != "account" {
		return unavailable
	}
	if request.Mode != "capabilities" && !validPolicy() {
		return unavailable
	}
	if request.Mode == "authenticate" || request.Mode == "account" {
		if request.Username == "" || len(request.Username) > 255 || strings.ContainsAny(request.Username, "\x00\r\n") {
			return denied
		}
		if request.Mode == "authenticate" && (request.Password == "" || len(request.Password) > 4096 || strings.ContainsRune(request.Password, 0)) {
			return denied
		}
		if request.RemoteHost != "" && net.ParseIP(request.RemoteHost) == nil {
			return denied
		}
	}

	// NSS, PAM and its callbacks stay on one OS thread. The process entrypoint's
	// eight-second deadline also bounds a hung dynamic loader, NSS or PAM module.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	library, err := loadPAMLibrary()
	if err != nil {
		return unavailable
	}
	defer library.close()
	if request.Mode == "capabilities" {
		return pamauth.Result{OK: true}
	}
	if request.Mode != "probe" {
		identity := library.lookupIdentity(request.Username)
		if !identity.OK {
			return identity
		}
	}

	conversation := pamConversationState{library: library, request: request}
	defer func() { conversation.request = pamauth.Request{} }()
	conv := library.newConversation(&conversation)
	if conv == nil {
		return unavailable
	}
	defer library.free(conv)
	serviceName := library.cString(service)
	if serviceName == nil {
		return unavailable
	}
	defer library.free(serviceName)
	var username unsafe.Pointer
	if request.Mode != "probe" {
		username = library.cString(request.Username)
		if username == nil {
			return unavailable
		}
		defer library.free(username)
	}
	var handle unsafe.Pointer
	status := library.start(serviceName, username, conv, &handle)
	if status != pamSuccess || handle == nil {
		if handle != nil {
			_ = library.end(handle, status)
		}
		return unavailable
	}
	defer func() {
		if library.end(handle, status) != pamSuccess && result.OK {
			result = unavailable
		}
	}()
	if request.Mode == "probe" {
		return pamauth.Result{OK: true}
	}
	if request.RemoteHost != "" {
		rhost := library.cString(request.RemoteHost)
		if rhost == nil {
			return unavailable
		}
		status = library.setItem(handle, pamRhost, rhost)
		library.free(rhost) // pam_set_item copies string items.
		if status != pamSuccess {
			return unavailable
		}
	}
	if request.Mode == "authenticate" {
		status = library.authenticate(handle, pamSilent|pamDisallowNullAuthtok)
		if status != pamSuccess {
			return pamFailure(status)
		}
	}
	status = library.account(handle, pamSilent|pamDisallowNullAuthtok)
	if status != pamSuccess {
		return pamFailure(status)
	}
	var user unsafe.Pointer
	status = library.getItem(handle, pamUser, &user)
	if status != pamSuccess {
		return denied
	}
	canonical, ok := boundedCString(user, 255)
	if !ok || canonical != request.Username {
		return denied
	}
	// Do not open sessions, establish credentials, or execute any user command.
	return pamauth.Result{OK: true, Username: canonical}
}

func validPolicy() bool {
	policy, err := os.Lstat("/etc/pam.d/" + service)
	if err != nil || !policy.Mode().IsRegular() || policy.Mode().Perm()&0022 != 0 {
		return false
	}
	stat, ok := policy.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func pamFailure(status int32) pamauth.Result {
	switch status {
	case pamPermDenied, pamAuthErr, pamUserUnknown, pamMaxtries,
		pamNewAuthtokReqd, pamAcctExpired, pamCredExpired,
		pamAuthtokExpired, pamConvErr:
		return pamauth.Result{Code: "denied"}
	default:
		// Module/configuration failures and unavailable identity services must
		// not invalidate a recoverable browser session as bad credentials.
		return pamauth.Result{Code: "unavailable"}
	}
}
