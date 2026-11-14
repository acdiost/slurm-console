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
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func discoveryClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	path := filepath.Join(t.TempDir(), "jwt")
	if err := os.WriteFile(path, []byte("test-service-token"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Endpoint: server.URL, AuthType: "jwt", Username: "reader", TokenFile: path, Version: "0.0.40", TLS: &tls.Config{RootCAs: pool}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDiscoveryFindsCompleteControllerContractsAndFallsBackOnlyOnMissingDocument(t *testing.T) {
	var calls atomic.Int32
	c := discoveryClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.Header.Get("X-SLURM-USER-TOKEN") != "test-service-token" {
			t.Error("discovery did not use normal authenticated GET")
		}
		if r.URL.Path == "/openapi/v3" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Path != "/openapi.json" {
			t.Error("unexpected discovery path", r.URL.Path)
		}
		paths := map[string]any{}
		for _, version := range []string{"v0.0.40", "v0.0.43", "v0.0.44", "v0.0.99"} {
			for _, resource := range []string{"ping", "jobs", "nodes", "partitions"} {
				paths["/slurm/"+version+"/"+resource+"/"] = map[string]any{"get": map[string]any{}}
			}
		}
		delete(paths, "/slurm/v0.0.44/nodes/")
		json.NewEncoder(w).Encode(map[string]any{"openapi": "3.0.2", "paths": paths})
	})
	versions, err := c.Versions(context.Background())
	if err != nil || !reflect.DeepEqual(versions, []string{"v0.0.43", "v0.0.40"}) || calls.Load() != 2 {
		t.Fatal(versions, err, calls.Load())
	}
}

func TestDiscoveryDoesNotRetryAuthErrorsRedirectsOrInvalidDocuments(t *testing.T) {
	for _, sample := range []struct {
		status     int
		body, code string
	}{{401, "private diagnostic", "auth_failed"}, {403, "private diagnostic", "auth_failed"}, {302, "", "unreachable"}, {200, `{"openapi":"3.0.2","paths":[]}`, "invalid_response"}, {200, `{"paths":{}}`, "invalid_response"}} {
		t.Run(sample.code+strconv.Itoa(sample.status), func(t *testing.T) {
			var calls atomic.Int32
			c := discoveryClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/private-redirect")
				w.WriteHeader(sample.status)
				w.Write([]byte(sample.body))
			})
			_, err := c.Versions(context.Background())
			if Code(err) != sample.code || calls.Load() != 1 || strings.Contains(err.Error(), "private diagnostic") {
				t.Fatal(err, calls.Load())
			}
		})
	}
}

func TestVersionedControllerRequestsStayOnConfiguredVersion(t *testing.T) {
	for _, version := range []string{"v0.0.40", "v0.0.41", "v0.0.42", "v0.0.43", "v0.0.44"} {
		t.Run(version, func(t *testing.T) {
			var calls atomic.Int32
			c := discoveryClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !strings.HasPrefix(r.URL.Path, "/slurm/"+version+"/") {
					t.Error("request switched versions", r.URL.Path)
				}
				json.NewEncoder(w).Encode(map[string]any{"meta": map[string]any{"plugin": map[string]any{"data_parser": "data_parser/" + version}, "slurm": map[string]any{"release": "fixture"}}, "errors": []any{}})
			})
			c.config.Version = version
			for _, resource := range []Resource{Ping, Jobs, Nodes, Partitions} {
				if _, err := c.Read(context.Background(), resource, ""); err != nil {
					t.Fatal(err)
				}
			}
			if sent, err := c.Cancel(context.Background(), "42"); !sent || err != nil {
				t.Fatal(sent, err)
			}
			if sent, err := c.ControlNode(context.Background(), "node1", "drain", "maintenance"); !sent || err != nil {
				t.Fatal(sent, err)
			}
			if calls.Load() != 6 {
				t.Fatal("unexpected retries", calls.Load())
			}
		})
	}
}

func TestUnsupportedLegacyWritesNeverReachSlurm(t *testing.T) {
	var calls atomic.Int32
	c := discoveryClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("unsupported write reached upstream")
	})
	for _, m := range []AccountingMutation{
		{Action: "reservation_delete", Cluster: "alpha", Reservation: &ReservationSpec{Name: "maintenance"}},
		{Action: "qos_tres_update", TRES: &TRESSpec{Kind: "qos", Bucket: "group_minutes"}},
	} {
		if sent, err := c.MutateAccounting(context.Background(), m); sent || Code(err) != "api_unsupported" {
			t.Fatal(sent, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unexpected upstream request")
	}
}
