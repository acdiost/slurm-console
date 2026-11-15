//go:build linux && (amd64 || arm64)

package main

import (
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"slurm-console/backend/internal/pamauth"
)

// Linux-PAM's public ABI uses 32-bit int/uid_t/gid_t and 64-bit pointers and
// size_t on these two architectures. The helper intentionally supports glibc
// hosts: purego's CGO=0 loader also needs libc.so.6, libdl.so.2 and
// libpthread.so.0. No native implementation is imported by the console.
// Constants and layouts: linux-pam/libpam/include/security/_pam_types.h.
const (
	pamSuccess             int32 = 0
	pamBufErr              int32 = 5
	pamPermDenied          int32 = 6
	pamAuthErr             int32 = 7
	pamUserUnknown         int32 = 10
	pamMaxtries            int32 = 11
	pamNewAuthtokReqd      int32 = 12
	pamAcctExpired         int32 = 13
	pamCredExpired         int32 = 16
	pamConvErr             int32 = 19
	pamAuthtokExpired      int32 = 27
	pamPromptEchoOff       int32 = 1
	pamPromptEchoOn        int32 = 2
	pamErrorMsg            int32 = 3
	pamTextInfo            int32 = 4
	pamUser                int32 = 2
	pamRhost               int32 = 4
	pamSilent              int32 = 0x8000
	pamDisallowNullAuthtok int32 = 0x0001
	pamMaxMessages               = 32
)

type pamMessage struct {
	style int32
	_     int32
	text  unsafe.Pointer
}

type pamResponse struct {
	text    unsafe.Pointer
	retcode int32
	_       int32
}

type pamConversation struct {
	callback uintptr
	data     unsafe.Pointer
}

type passwd struct {
	name     unsafe.Pointer
	password unsafe.Pointer
	uid      uint32
	gid      uint32
	gecos    unsafe.Pointer
	dir      unsafe.Pointer
	shell    unsafe.Pointer
}

type pamLibrary struct {
	handle       uintptr
	start        func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, *unsafe.Pointer) int32
	end          func(unsafe.Pointer, int32) int32
	authenticate func(unsafe.Pointer, int32) int32
	account      func(unsafe.Pointer, int32) int32
	setItem      func(unsafe.Pointer, int32, unsafe.Pointer) int32
	getItem      func(unsafe.Pointer, int32, *unsafe.Pointer) int32
	calloc       func(uintptr, uintptr) unsafe.Pointer
	free         func(unsafe.Pointer)
	getpwnam     func(unsafe.Pointer, *passwd, unsafe.Pointer, uintptr, *unsafe.Pointer) int32
}

func loadPAMLibrary() (*pamLibrary, error) {
	handle, err := purego.Dlopen("libpam.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	library := &pamLibrary{handle: handle}
	for _, symbol := range []struct {
		name string
		fn   any
	}{
		{"pam_start", &library.start},
		{"pam_end", &library.end},
		{"pam_authenticate", &library.authenticate},
		{"pam_acct_mgmt", &library.account},
		{"pam_set_item", &library.setItem},
		{"pam_get_item", &library.getItem},
		// Resolve against libpam and its dependencies so conversation responses
		// use the same C allocator as the PAM modules that take ownership.
		{"calloc", &library.calloc},
		{"free", &library.free},
		{"getpwnam_r", &library.getpwnam},
	} {
		address, err := purego.Dlsym(handle, symbol.name)
		if err != nil {
			library.close()
			return nil, err
		}
		purego.RegisterFunc(symbol.fn, address)
	}
	return library, nil
}

func (library *pamLibrary) close() {
	_ = purego.Dlclose(library.handle)
}

func (library *pamLibrary) cString(value string) unsafe.Pointer {
	memory := library.calloc(uintptr(len(value)+1), 1)
	if memory != nil {
		copy(unsafe.Slice((*byte)(memory), len(value)), value)
	}
	return memory
}

func boundedCString(memory unsafe.Pointer, limit int) (string, bool) {
	if memory == nil {
		return "", false
	}
	for size := 0; size <= limit; size++ {
		if *(*byte)(unsafe.Add(memory, size)) == 0 {
			return string(unsafe.Slice((*byte)(memory), size)), true
		}
	}
	return "", false
}

func (library *pamLibrary) lookupIdentity(username string) pamauth.Result {
	unavailable := pamauth.Result{Code: "unavailable"}
	denied := pamauth.Result{Code: "denied"}
	name := library.cString(username)
	if name == nil {
		return unavailable
	}
	defer library.free(name)
	memory := library.calloc(1, unsafe.Sizeof(passwd{}))
	if memory == nil {
		return unavailable
	}
	defer library.free(memory)
	identity := (*passwd)(memory)
	// getpwnam_r honors NSS (SSSD/LDAP/etc.); CGO=0 os/user only reads files.
	// Grow on ERANGE, with a finite cap for a malfunctioning identity service.
	for size := uintptr(16384); size <= 1048576; size *= 2 {
		buffer := library.calloc(size, 1)
		if buffer == nil {
			return unavailable
		}
		var found unsafe.Pointer
		status := library.getpwnam(name, identity, buffer, size, &found)
		if status == int32(syscall.ERANGE) {
			library.free(buffer)
			continue
		}
		if status != 0 {
			library.free(buffer)
			if status == int32(syscall.ENOENT) || status == int32(syscall.ESRCH) {
				return denied
			}
			return unavailable
		}
		if found == nil {
			library.free(buffer)
			return denied
		}
		canonical, ok := boundedCString(identity.name, 255)
		accepted := ok && canonical == username && identity.uid != 0
		library.free(buffer)
		if !accepted {
			return denied
		}
		return pamauth.Result{OK: true, Username: canonical}
	}
	return unavailable
}

type pamConversationState struct {
	library  *pamLibrary
	request  pamauth.Request
	prompted bool
}

func (library *pamLibrary) newConversation(state *pamConversationState) unsafe.Pointer {
	memory := library.calloc(1, unsafe.Sizeof(pamConversation{}))
	if memory == nil {
		return nil
	}
	// purego v0.11.3 NewCallback accepts fixed-width integer/pointer arguments
	// and a single integer result. Its callback registry retains the Go state;
	// appdata stays NULL, so no Go pointer is retained in C-owned memory.
	// There is exactly one transaction/callback per worker process.
	(*pamConversation)(memory).callback = purego.NewCallback(state.converse)
	return memory
}

func (state *pamConversationState) converse(count int32, messages unsafe.Pointer, output unsafe.Pointer, _ unsafe.Pointer) int32 {
	if output == nil {
		return pamConvErr
	}
	*(*unsafe.Pointer)(output) = nil
	if count <= 0 || count > pamMaxMessages || messages == nil {
		return pamConvErr
	}
	memory := state.library.calloc(uintptr(count), unsafe.Sizeof(pamResponse{}))
	if memory == nil {
		return pamBufErr
	}
	responses := unsafe.Slice((*pamResponse)(memory), int(count))
	completed := false
	defer func() {
		if !completed {
			for _, response := range responses {
				if response.text != nil {
					// Wipe partial password responses before releasing C memory.
					for offset := 0; offset <= 4096; offset++ {
						value := (*byte)(unsafe.Add(response.text, offset))
						if *value == 0 {
							break
						}
						*value = 0
					}
					state.library.free(response.text)
				}
			}
			state.library.free(memory)
		}
	}()
	for index, message := range unsafe.Slice((**pamMessage)(messages), int(count)) {
		if message == nil {
			return pamConvErr
		}
		var answer string
		switch message.style {
		case pamPromptEchoOff:
			if state.request.Mode != "authenticate" || state.prompted {
				return pamConvErr
			}
			state.prompted = true
			answer = state.request.Password
		case pamPromptEchoOn:
			answer = state.request.Username
		case pamTextInfo, pamErrorMsg:
			// Informational messages require a NULL response.
			continue
		default:
			return pamConvErr
		}
		responses[index].text = state.library.cString(answer)
		if responses[index].text == nil {
			return pamBufErr
		}
	}
	// On success PAM owns the response array and every response string.
	*(*unsafe.Pointer)(output) = memory
	completed = true
	return pamSuccess
}
