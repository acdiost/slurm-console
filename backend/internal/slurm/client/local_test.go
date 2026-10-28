package client

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalRequestProtocol(t *testing.T) {
	// A short path also works under the macOS Unix socket length limit.
	dir, err := os.MkdirTemp("/tmp", "slurm-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "rest.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slurm/v0.0.43/ping/" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"meta":{"plugin":{"data_parser":"v0.0.43"},"slurm":{"release":"25.11.8"}},"pings":[{"pinged":"UP"}]}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	request := localRequest{Endpoint: "unix://" + socket, Version: "v0.0.43", Method: "GET", Path: "/slurm/v0.0.43/ping/", Timeout: time.Second}
	raw, _ := json.Marshal(request)
	var out bytes.Buffer
	if code := ServeLocal(bytes.NewReader(raw), &out); code != 0 {
		t.Fatal(code)
	}
	var result localResponse
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Code != "" || len(result.Envelope.Pings) == 0 {
		t.Fatal(out.String(), err)
	}
	for _, bad := range []string{`{}`, string(raw) + `{}`, `{"Endpoint":"https://unapproved","Timeout":1000000000}`, string(bytes.Repeat([]byte("x"), 16385))} {
		if ServeLocal(bytes.NewBufferString(bad), &out) == 0 {
			t.Fatal("accepted invalid IPC")
		}
	}
}
func TestLocalRequestBoundaries(t *testing.T) {
	base := localRequest{Version: "v0.0.43", Method: "GET", Path: "/slurm/v0.0.43/jobs/"}
	if !validLocalRequest(base) {
		t.Fatal("read denied")
	}
	for _, path := range []string{"/etc/passwd", "/slurm/v0.0.43/node/a?x=y", "/slurm/v0.0.43/job/1?signal=KILL", "/slurm/v0.0.43/nodes/../"} {
		base.Path = path
		if validLocalRequest(base) {
			t.Fatal(path)
		}
	}
	base.Method = "DELETE"
	base.Path = "/slurm/v0.0.43/job/123?signal=KILL"
	base.Write = true
	if !validLocalRequest(base) {
		t.Fatal("cancel denied")
	}
	base.Write = false
	if validLocalRequest(base) {
		t.Fatal("unmarked write")
	}
	base.Method = "POST"
	base.Path = "/slurm/v0.0.43/node/ALL"
	base.Write = true
	base.Body = []byte(`{"state":["RESUME"]}`)
	if validLocalRequest(base) {
		t.Fatal("ALL allowed")
	}
	base.Path = "/slurm/v0.0.43/node/n1"
	if !validLocalRequest(base) {
		t.Fatal("resume denied")
	}
	base.Body = []byte(`{"state":["DOWN"]}`)
	if validLocalRequest(base) {
		t.Fatal("unapproved action")
	}
	var out localOutput
	out.Write(make([]byte, (16<<20)+1))
	if !out.exceeded || out.Len() != 0 {
		t.Fatal("unbounded reply")
	}
}
