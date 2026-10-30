package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryQueryWireIsBoundedAndCanonical(t *testing.T) {
	q := HistoryQuery{Cluster: "alpha", Start: 1700000000, End: 1700086400, User: "alice", JobID: "42"}
	path, ok := historyPath("v0.0.43", q)
	if !ok {
		t.Fatal("valid query rejected")
	}
	req := localRequest{Version: "v0.0.43", Method: "GET", Path: path}
	if !validLocalRequest(req) {
		t.Fatal("approved query not allowed as dropped identity")
	}
	for _, suffix := range []string{"&show_batch_script=true", "&cluster=other", "&start_time=0", "&show_job_environment=true", "&format=script"} {
		r := req
		r.Path += suffix
		if validLocalRequest(r) {
			t.Fatal("expanded query accepted", suffix)
		}
	}
	for _, change := range []func(*HistoryQuery){func(q *HistoryQuery) { q.Start = 0 }, func(q *HistoryQuery) { q.End = q.Start + 32*86400 }, func(q *HistoryQuery) { q.User = "alice,bob" }, func(q *HistoryQuery) { q.Cluster = "ALL" }, func(q *HistoryQuery) { q.JobID = "42.batch" }, func(q *HistoryQuery) { q.State = "any" }} {
		bad := q
		change(&bad)
		if bad.Valid() {
			t.Fatalf("bad selector accepted: %+v", bad)
		}
	}
	r := req
	r.Method = "POST"
	if validLocalRequest(r) {
		t.Fatal("history write accepted")
	}
}
func TestHistoryRejectsWarningsAndUsesOnlyApprovedCondition(t *testing.T) {
	var received string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}, "errors": []any{}, "warnings": []any{map[string]any{"description": "private warning"}}, "meta": map[string]any{"plugin": map[string]any{"type": "openapi/slurmdbd", "data_parser": "data_parser/v0.0.43"}, "slurm": map[string]any{"release": "25.11.8"}}})
	}))
	defer server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Endpoint: server.URL, AuthType: "jwt", Username: "reader", TokenFile: token, Version: "v0.0.43", TLS: &tls.Config{RootCAs: pool}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Read(context.Background(), DBJobs, HistoryQuery{Cluster: "alpha", Start: 1700000000, End: 1700086400}.Key())
	if Code(err) != "invalid_response" || strings.Contains(err.Error(), "private") || !strings.Contains(received, "usage_time_as_submit_time=true") {
		t.Fatalf("warning or scope failure %s %v", received, err)
	}
}

func TestHistoryEmptyResultWarningIsNotAServiceFailure(t *testing.T) {
	e := Envelope{Jobs: json.RawMessage(`[]`), Warnings: []json.RawMessage{json.RawMessage(`{"description":"slurmdb_jobs_get() found nothing","source":"_dump_jobs"}`)}}
	if !validHistoryWarnings(e) {
		t.Fatal("standard empty result rejected")
	}
	e.Jobs = json.RawMessage(`[{}]`)
	if validHistoryWarnings(e) {
		t.Fatal("warning with partial nonempty data ignored")
	}
	e.Jobs = json.RawMessage(`null`)
	if validHistoryWarnings(e) {
		t.Fatal("missing jobs interpreted as empty")
	}
}
