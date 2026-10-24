//go:build linux && cgo

package main

import (
	"errors"
	"net"
	"os"
	"os/user"
	"runtime"
	"strings"
	"syscall"

	"github.com/msteinert/pam/v2"
	"slurm-console/backend/internal/pamauth"
)

const service = "slurm-console"

func run(request pamauth.Request) pamauth.Result {
	denied := pamauth.Result{Code: "denied"}
	unavailable := pamauth.Result{Code: "unavailable"}
	if os.Geteuid() != 0 {
		return unavailable
	}
	policy, err := os.Lstat("/etc/pam.d/" + service)
	if err != nil || !policy.Mode().IsRegular() || policy.Mode().Perm()&0022 != 0 {
		return unavailable
	}
	if stat, ok := policy.Sys().(*syscall.Stat_t); !ok || stat.Uid != 0 {
		return unavailable
	}
	if request.Mode == "probe" {
		return pamauth.Result{OK: true}
	}
	if request.Mode != "authenticate" && request.Mode != "account" {
		return unavailable
	}
	if request.Username == "" || len(request.Username) > 255 || strings.ContainsAny(request.Username, "\x00\r\n") {
		return denied
	}
	if request.Mode == "authenticate" && (request.Password == "" || len(request.Password) > 4096 || strings.ContainsRune(request.Password, 0)) {
		return denied
	}
	identity, err := user.Lookup(request.Username)
	if err != nil || identity.Uid == "0" || identity.Username != request.Username {
		return denied
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	prompted := false
	transaction, err := pam.StartFunc(service, request.Username, func(style pam.Style, _ string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			if request.Mode == "authenticate" && !prompted {
				prompted = true
				return request.Password, nil
			}
		case pam.PromptEchoOn:
			return request.Username, nil
		case pam.TextInfo, pam.ErrorMsg:
			return "", nil
		}
		return "", errors.New("unsupported PAM conversation")
	})
	if err != nil {
		return unavailable
	}
	defer transaction.End()
	if request.RemoteHost != "" {
		if net.ParseIP(request.RemoteHost) == nil {
			return denied
		}
		if transaction.SetItem(pam.Rhost, request.RemoteHost) != nil {
			return unavailable
		}
	}
	if request.Mode == "authenticate" {
		if err := transaction.Authenticate(pam.Silent | pam.DisallowNullAuthtok); err != nil {
			return pamFailure(err)
		}
	}
	if err := transaction.AcctMgmt(pam.Silent | pam.DisallowNullAuthtok); err != nil {
		return pamFailure(err)
	}
	canonical, err := transaction.GetItem(pam.User)
	if err != nil || canonical != identity.Username {
		return denied
	}
	// Do not open sessions, establish credentials, or execute any user command.
	return pamauth.Result{OK: true, Username: canonical}
}

func pamFailure(err error) pamauth.Result {
	switch err {
	case pam.ErrAuth, pam.ErrPermDenied, pam.ErrUserUnknown, pam.ErrMaxtries,
		pam.ErrNewAuthtokReqd, pam.ErrAcctExpired, pam.ErrCredExpired,
		pam.ErrAuthtokExpired, pam.ErrConv:
		return pamauth.Result{Code: "denied"}
	default:
		// Module/configuration failures and unavailable identity services must
		// not invalidate a recoverable browser session as bad credentials.
		return pamauth.Result{Code: "unavailable"}
	}
}
