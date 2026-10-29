package client

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountingAmbiguousDeleteIsAnExplicitRejection(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "sc-db-reject-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	socket := filepath.Join(directory, "rest.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "42" || !r.URL.Query().Has("partition") {
			t.Error("incomplete target tuple")
		}
		w.WriteHeader(500)
		w.Write([]byte(`{"meta":{"plugin":{"type":"openapi/slurmdbd","data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.8"}},"errors":[{"error_number":9210}]}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	c, err := New(Config{Endpoint: "unix://" + socket, AuthType: "local", Version: "v0.0.43", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := c.MutateAccounting(context.Background(), AccountingMutation{Action: "association_remove", Cluster: "alpha", Account: "research", User: "alice", AssociationID: "42"})
	if sent || Code(err) != "operation_rejected" {
		t.Fatal(sent, err)
	}
}
