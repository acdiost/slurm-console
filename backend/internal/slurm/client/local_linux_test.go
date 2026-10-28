//go:build linux

package client

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == LocalCommand {
		if os.Geteuid() == 0 || os.Getegid() == 0 {
			os.Exit(2)
		}
		os.Exit(ServeLocal(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// This exercises a real credential change when the Linux test runner is root;
// ordinary CI also runs the portable IPC and transport regression tests.
func TestRootLocalSocketIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to exercise credential dropping")
	}
	name := "nobody"
	account, err := user.Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	expectedUID, _ := strconv.Atoi(account.Uid)
	expectedGID, _ := strconv.Atoi(account.Gid)
	dir, err := os.MkdirTemp("/tmp", "slurm-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "rest.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0666); err != nil {
		t.Fatal(err)
	}
	peer := make(chan *syscall.Ucred, 16)
	server := &http.Server{ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
		raw, err := conn.(*net.UnixConn).SyscallConn()
		if err != nil {
			t.Error(err)
			return ctx
		}
		var cred *syscall.Ucred
		if err := raw.Control(func(fd uintptr) {
			cred, err = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		}); err != nil {
			t.Error(err)
		}
		if cred != nil {
			status, err := os.ReadFile("/proc/" + strconv.Itoa(int(cred.Pid)) + "/status")
			if err != nil {
				t.Error(err)
			}
			for _, line := range strings.Split(string(status), "\n") {
				if strings.HasPrefix(line, "Groups:") {
					for _, group := range strings.Fields(strings.TrimPrefix(line, "Groups:")) {
						if group == "0" {
							t.Error("root group inherited")
						}
					}
				}
			}
		}
		peer <- cred
		return ctx
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slurm/v0.0.43/job/1":
			w.WriteHeader(http.StatusForbidden)
			return
		case "/slurm/v0.0.43/job/2":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"meta":{"plugin":{"data_parser":"v0.0.43"},"slurm":{"release":"25.11.8"}},"errors":[{"error_number":2002}]}`))
			return
		case "/slurm/v0.0.43/job/3":
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		case "/slurm/v0.0.43/job/4":
			<-r.Context().Done()
			return
		}
		w.Write([]byte(`{"meta":{"plugin":{"data_parser":"v0.0.43"},"slurm":{"release":"25.11.8"}},"pings":[]}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	c, err := New(Config{Endpoint: "unix://" + socket, AuthType: "local", Version: "v0.0.43", Timeout: 3 * time.Second, LocalUsername: name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), Ping, ""); err != nil {
		t.Fatal(err)
	}
	cred := <-peer
	if cred == nil || int(cred.Uid) != expectedUID || int(cred.Gid) != expectedGID {
		t.Fatalf("unexpected socket peer: %+v", cred)
	}
	for _, id := range []string{"1", "2"} {
		sent, err := c.Cancel(context.Background(), id)
		if sent || Code(err) != "auth_failed" {
			t.Fatalf("explicit rejection: sent=%v error=%v", sent, err)
		}
	}
	sent, err := c.Cancel(context.Background(), "3")
	if !sent || err == nil {
		t.Fatalf("lost write reply must remain unknown: sent=%v error=%v", sent, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	sent, err = c.Cancel(ctx, "4")
	if !sent || Code(err) != "timeout" {
		t.Fatalf("write timeout must remain unknown: sent=%v error=%v", sent, err)
	}
	sent, err = c.ControlNode(context.Background(), "node1", "drain", "isolated fixture")
	if !sent || err != nil {
		t.Fatalf("node request: sent=%v error=%v", sent, err)
	}
	for _, name := range []string{"root", "slurm-console-nonexistent"} {
		if configureLocalIdentity(&exec.Cmd{}, name) == nil {
			t.Fatalf("accepted identity %s", name)
		}
	}
}
