package client

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestClientRotatesCredentialsAndClassifiesErrorsWithoutEcho(t *testing.T) {
	expected := rand.Text()
	var mu sync.Mutex
	code := 200
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-SLURM-USER-TOKEN") != expected || r.Header.Get("X-SLURM-USER-NAME") != "reader" {
			t.Error("wrong request credentials")
		}
		w.WriteHeader(code)
		if code == 200 {
			fmt.Fprint(w, `{"jobs":[],"meta":{"plugin":{"data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.8"}},"errors":[]}`)
		} else {
			fmt.Fprint(w, "private upstream diagnostic")
		}
	}))
	defer server.Close()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(expected), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := New(Config{Endpoint: server.URL, AuthType: "jwt", Username: "reader", TokenFile: tokenPath, Version: "v0.0.43", TLS: &tls.Config{RootCAs: pool}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), Jobs, ""); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	expected = rand.Text()
	err = os.WriteFile(tokenPath, []byte(expected), 0600)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), Jobs, ""); err != nil {
		t.Fatal("token rotation did not take effect")
	}
	mu.Lock()
	code = 403
	mu.Unlock()
	if _, err := reader.Read(context.Background(), Jobs, ""); Code(err) != "auth_failed" || err.Error() == "private upstream diagnostic" {
		t.Fatal("unclassified or exposed upstream failure")
	}
}
func TestClientRejectsUnsafePathsVersionsAndTLSBypass(t *testing.T) {
	for _, id := range []string{"0", "01", "1_2", "1/../nodes", "4294967296", "123;delete"} {
		if ValidJobID(id) {
			t.Fatalf("invalid job selector: %s", id)
		}
	}
	for _, name := range []string{"../host", "node/1", "node?token=x", "a\nheader"} {
		if ValidNodeName(name) {
			t.Fatal("unsafe node name accepted")
		}
	}
	config := Config{Endpoint: "https://rest.example.invalid", AuthType: "jwt", Username: "reader", TokenFile: "/etc/slurm-console/test.jwt", Version: "v0.0.43", Timeout: time.Second}
	config.TLS = &tls.Config{InsecureSkipVerify: true}
	if _, err := New(config); err == nil {
		t.Fatal("TLS bypass accepted")
	}
	config.TLS = nil
	config.Version = "v0.0.99"
	if _, err := New(config); err == nil {
		t.Fatal("unknown parser accepted")
	}
	config.Version = "v0.0.43"
	reader, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), Resource("reconfigure"), ""); Code(err) != "invalid_request" {
		t.Fatal("arbitrary resource accepted")
	}
}
