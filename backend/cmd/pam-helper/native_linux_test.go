//go:build linux && (amd64 || arm64)

package main

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"slurm-console/backend/internal/pamauth"
)

func testPAMLibrary(t *testing.T) *pamLibrary {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	library, err := loadPAMLibrary()
	if err != nil {
		t.Skipf("Linux glibc/libpam runtime required: %v", err)
	}
	t.Cleanup(library.close)
	return library
}

func testMessages(t *testing.T, library *pamLibrary, styles ...int32) unsafe.Pointer {
	t.Helper()
	memory := library.calloc(uintptr(len(styles)), unsafe.Sizeof(pamMessage{}))
	pointers := library.calloc(uintptr(len(styles)), unsafe.Sizeof(uintptr(0)))
	if memory == nil || pointers == nil {
		t.Fatal("C message allocation failed")
	}
	t.Cleanup(func() {
		library.free(pointers)
		library.free(memory)
	})
	messages := unsafe.Slice((*pamMessage)(memory), len(styles))
	array := unsafe.Slice((**pamMessage)(pointers), len(styles))
	for index, style := range styles {
		messages[index].style = style
		array[index] = &messages[index]
	}
	return pointers
}

func TestLinuxNativeLayouts(t *testing.T) {
	if unsafe.Sizeof(pamMessage{}) != 16 || unsafe.Offsetof(pamMessage{}.text) != 8 ||
		unsafe.Sizeof(pamResponse{}) != 16 || unsafe.Offsetof(pamResponse{}.retcode) != 8 ||
		unsafe.Sizeof(pamConversation{}) != 16 || unsafe.Offsetof(pamConversation{}.data) != 8 ||
		unsafe.Sizeof(passwd{}) != 48 || unsafe.Offsetof(passwd{}.uid) != 16 ||
		unsafe.Offsetof(passwd{}.gid) != 20 || unsafe.Offsetof(passwd{}.gecos) != 24 {
		t.Fatal("Go structs do not match Linux-PAM/glibc LP64 ABI")
	}
}

func TestNativeConversationCallback(t *testing.T) {
	library := testPAMLibrary(t)
	state := pamConversationState{library: library, request: pamauth.Request{
		Mode: "authenticate", Username: "alice", Password: "secret",
	}}
	conversation := library.newConversation(&state)
	if conversation == nil {
		t.Fatal("C conversation allocation failed")
	}
	defer library.free(conversation)
	var callback func(int32, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) int32
	purego.RegisterFunc(&callback, (*pamConversation)(conversation).callback)
	messages := testMessages(t, library, pamTextInfo, pamPromptEchoOn, pamPromptEchoOff, pamErrorMsg)
	var output unsafe.Pointer
	if status := callback(4, messages, unsafe.Pointer(&output), nil); status != pamSuccess || output == nil {
		t.Fatalf("native callback status=%d output=%p", status, output)
	}
	responses := unsafe.Slice((*pamResponse)(output), 4)
	defer func() {
		for _, response := range responses {
			library.free(response.text)
		}
		library.free(output)
	}()
	if responses[0].text != nil || responses[3].text != nil {
		t.Fatal("informational messages must have NULL responses")
	}
	for index, expected := range map[int]string{1: "alice", 2: "secret"} {
		value, ok := boundedCString(responses[index].text, 255)
		if !ok || value != expected || responses[index].retcode != 0 {
			t.Fatalf("response %d = %q, valid=%t, retcode=%d", index, value, ok, responses[index].retcode)
		}
	}
	var repeated unsafe.Pointer
	if status := callback(1, testMessages(t, library, pamPromptEchoOff), unsafe.Pointer(&repeated), nil); status != pamConvErr || repeated != nil {
		t.Fatalf("repeated hidden prompt: status=%d output=%p", status, repeated)
	}
}

func TestConversationFailureReleasesPartialResponses(t *testing.T) {
	base := testPAMLibrary(t)
	for _, fixture := range []struct {
		name   string
		mode   string
		styles []int32
		failAt int
		status int32
	}{
		{"repeated-password", "authenticate", []int32{pamPromptEchoOff, pamPromptEchoOff}, 0, pamConvErr},
		{"unknown-style-after-password", "authenticate", []int32{pamPromptEchoOff, 99}, 0, pamConvErr},
		{"account-password", "account", []int32{pamPromptEchoOff}, 0, pamConvErr},
		{"probe-password", "probe", []int32{pamPromptEchoOff}, 0, pamConvErr},
		{"response-array-allocation", "authenticate", []int32{pamPromptEchoOff}, 1, pamBufErr},
		{"password-allocation-after-username", "authenticate", []int32{pamPromptEchoOn, pamPromptEchoOff}, 3, pamBufErr},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			library := *base
			live := make(map[unsafe.Pointer]uintptr)
			allocations := 0
			library.calloc = func(count, size uintptr) unsafe.Pointer {
				allocations++
				if allocations == fixture.failAt {
					return nil
				}
				memory := base.calloc(count, size)
				if memory != nil {
					live[memory] = count * size
				}
				return memory
			}
			library.free = func(memory unsafe.Pointer) {
				if _, ok := live[memory]; !ok {
					t.Errorf("free of unknown response allocation %p", memory)
					return
				}
				if live[memory] == uintptr(len("secret")+1) {
					for _, value := range unsafe.Slice((*byte)(memory), len("secret")+1) {
						if value != 0 {
							t.Error("partial password response was not wiped")
							break
						}
					}
				}
				delete(live, memory)
				base.free(memory)
			}
			state := pamConversationState{library: &library, request: pamauth.Request{
				Mode: fixture.mode, Username: "alice", Password: "secret",
			}}
			var output unsafe.Pointer
			status := state.converse(int32(len(fixture.styles)), testMessages(t, base, fixture.styles...), unsafe.Pointer(&output), nil)
			if status != fixture.status || output != nil {
				t.Errorf("status=%d output=%p", status, output)
			}
			if len(live) != 0 {
				t.Errorf("leaked %d C allocations", len(live))
				for memory := range live {
					base.free(memory)
				}
			}
		})
	}
}

func TestConversationRejectsInvalidMessages(t *testing.T) {
	library := testPAMLibrary(t)
	state := pamConversationState{library: library}
	for _, count := range []int32{-1, 0, 1, pamMaxMessages + 1} {
		var output unsafe.Pointer
		if status := state.converse(count, nil, unsafe.Pointer(&output), nil); status != pamConvErr || output != nil {
			t.Fatalf("count %d: status=%d output=%p", count, status, output)
		}
	}
	messages := testMessages(t, library, pamTextInfo)
	*(*unsafe.Pointer)(messages) = nil
	var output unsafe.Pointer
	if status := state.converse(1, messages, unsafe.Pointer(&output), nil); status != pamConvErr || output != nil {
		t.Fatalf("NULL message: status=%d output=%p", status, output)
	}
	if status := state.converse(1, messages, nil, nil); status != pamConvErr {
		t.Fatalf("NULL response pointer: status=%d", status)
	}
}

func TestNSSIdentityClassification(t *testing.T) {
	base := testPAMLibrary(t)
	for _, fixture := range []struct {
		name      string
		canonical string
		uid       uint32
		status    int32
		missing   bool
		grow      bool
		want      pamauth.Result
	}{
		{"canonical-nonroot", "alice", 1000, 0, false, false, pamauth.Result{OK: true, Username: "alice"}},
		{"buffer-growth", "alice", 1000, 0, false, true, pamauth.Result{OK: true, Username: "alice"}},
		{"root-alias", "alice", 0, 0, false, false, pamauth.Result{Code: "denied"}},
		{"canonical-mismatch", "Alice", 1000, 0, false, false, pamauth.Result{Code: "denied"}},
		{"missing", "", 0, 0, true, false, pamauth.Result{Code: "denied"}},
		{"nss-not-found", "", 0, int32(syscall.ENOENT), false, false, pamauth.Result{Code: "denied"}},
		{"nss-outage", "", 0, int32(syscall.EIO), false, false, pamauth.Result{Code: "unavailable"}},
		{"nss-buffer-limit", "", 0, int32(syscall.ERANGE), false, false, pamauth.Result{Code: "unavailable"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			library := *base
			calls := 0
			library.getpwnam = func(name unsafe.Pointer, identity *passwd, buffer unsafe.Pointer, size uintptr, found *unsafe.Pointer) int32 {
				calls++
				username, ok := boundedCString(name, 255)
				if !ok || username != "alice" {
					t.Fatalf("NSS input=%q valid=%t", username, ok)
				}
				if fixture.grow && calls == 1 {
					return int32(syscall.ERANGE)
				}
				if fixture.status != 0 || fixture.missing {
					return fixture.status
				}
				copy(unsafe.Slice((*byte)(buffer), int(size)), fixture.canonical+"\x00")
				identity.name = buffer
				identity.uid = fixture.uid
				*found = unsafe.Pointer(identity)
				return 0
			}
			if got := library.lookupIdentity("alice"); got != fixture.want {
				t.Fatalf("identity=%+v want=%+v", got, fixture.want)
			}
			if fixture.grow && calls != 2 {
				t.Fatalf("NSS calls=%d, want initial lookup plus retry", calls)
			}
		})
	}
}

func TestRealNSSRejectsRoot(t *testing.T) {
	library := testPAMLibrary(t)
	if result := library.lookupIdentity("root"); result.OK || result.Code != "denied" {
		t.Fatalf("NSS root identity=%+v", result)
	}
}

func TestCapabilities(t *testing.T) {
	_ = testPAMLibrary(t)
	result := run(pamauth.Request{Mode: "capabilities"})
	if os.Geteuid() == 0 {
		if !result.OK {
			t.Fatalf("root runtime capabilities=%+v", result)
		}
	} else if result.OK || result.Code != "unavailable" {
		t.Fatalf("unprivileged capabilities=%+v", result)
	}
}

func TestPAMFailureClassification(t *testing.T) {
	denied := map[int32]bool{6: true, 7: true, 10: true, 11: true, 12: true, 13: true, 16: true, 19: true, 27: true}
	for status := int32(1); status <= 31; status++ {
		want := "unavailable"
		if denied[status] {
			want = "denied"
		}
		if result := pamFailure(status); result.OK || result.Code != want {
			t.Errorf("status %d = %+v, want %s", status, result, want)
		}
	}
}
