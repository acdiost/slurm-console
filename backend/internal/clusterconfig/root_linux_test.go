//go:build linux

package clusterconfig

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"slurm-console/backend/internal/slurm/client"
)

func TestRootDefaultSocketPeerUsesRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify socket peer credentials")
	}
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "rest.sock"))
	if err != nil {
		t.Fatal(err)
	}
	peer := make(chan *syscall.Ucred, 3)
	server := &http.Server{ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
		raw, err := conn.(*net.UnixConn).SyscallConn()
		if err != nil {
			t.Error(err)
			return ctx
		}
		var cred *syscall.Ucred
		if err = raw.Control(func(fd uintptr) {
			cred, err = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		}); err != nil {
			t.Error(err)
		}
		peer <- cred
		return ctx
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"meta":{"plugin":{"data_parser":"v0.0.43"},"slurm":{"release":"25.11.8"}},"pings":[]}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	for _, localUser := range []string{"", "root", "legacy-service-user"} {
		p := Profile{Endpoint: "unix://" + listener.Addr().String(), AuthType: "local", LocalUser: localUser}
		c, err := p.Client("v0.0.43", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.Read(context.Background(), client.Ping, ""); err != nil {
			t.Fatal(err)
		}
		select {
		case cred := <-peer:
			if cred == nil || cred.Uid != 0 || cred.Gid != 0 || int(cred.Pid) != os.Getpid() {
				t.Fatalf("request did not use root parent: %+v", cred)
			}
		case <-time.After(time.Second):
			t.Fatal("missing peer credentials")
		}
	}
}
