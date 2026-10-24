package clusterconfig

import (
	"context"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const validPing = `{"pings":[{"hostname":"controller-a","pinged":"UP","mode":"primary"}],"meta":{"plugin":{"data_parser":"data_parser/v0.0.43"},"slurm":{"release":"25.11.4"}},"errors":[]}`

func tlsProfile(t *testing.T, handler http.HandlerFunc) (Profile, *atomic.Int32) {
	t.Helper()
	secret := rand.Text()
	calls := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != "/slurm/v0.0.43/ping/" || r.Header.Get("X-SLURM-USER-NAME") != "monitor" || r.Header.Get("X-SLURM-USER-TOKEN") != secret {
			t.Error("unexpected Slurm request or credentials")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	path := t.TempDir()
	token := filepath.Join(path, "token")
	ca := filepath.Join(path, "ca.pem")
	if err := os.WriteFile(token, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0644); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(map[string]Profile{"fixture": {Label: "Fixture", Endpoint: server.URL, AuthType: "jwt", Username: "monitor", TokenFile: token, CAFile: ca, Versions: []string{"v0.0.43"}}})
	if err != nil {
		t.Fatal(err)
	}
	return registry.Profiles["fixture"], calls
}

func TestProbeVerifiesTLSAndNormalizesOnlySafeFields(t *testing.T) {
	profile, _ := tlsProfile(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, validPing) })
	result := Probe(context.Background(), profile, "v0.0.43", time.Second)
	if result.Status != "connected" || result.ServerVersion != "25.11.4" || len(result.Controllers) != 1 || result.CheckedAt == "" {
		t.Fatalf("invalid probe: %+v", result)
	}
}

func TestProbeClassifiesFailuresWithoutEchoingUpstreamBody(t *testing.T) {
	for _, sample := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"unauthorized", 401, "sensitive upstream response", "auth_failed"},
		{"api-version", 404, "sensitive upstream response", "api_unsupported"},
		{"server-error", 500, "sensitive upstream response", "unreachable"},
		{"malformed", 200, "sensitive upstream response", "invalid_response"},
		{"wrong-parser", 200, strings.Replace(validPing, "data_parser/v0.0.43", "data_parser/v0.0.42", 1), "api_unsupported"},
		{"down-controller", 200, strings.Replace(validPing, `"UP"`, `"DOWN"`, 1), "controller_down"},
		{"oversized", 200, strings.Repeat("x", (8<<20)+1), "invalid_response"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			profile, _ := tlsProfile(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(sample.status); fmt.Fprint(w, sample.body) })
			result := Probe(context.Background(), profile, "v0.0.43", 2*time.Second)
			if result.Status != sample.want || strings.Contains(result.Message, "sensitive") {
				t.Fatalf("unexpected safe result: %+v", result)
			}
		})
	}
}

func TestProbeDoesNotFollowRedirectsOrReadUnsafeCredentials(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	profile, calls := tlsProfile(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if result := Probe(context.Background(), profile, "v0.0.43", time.Second); result.Status == "connected" || redirected.Load() != 0 {
		t.Fatal("followed redirect with credentials")
	}
	if err := os.Chmod(profile.TokenFile, 0644); err != nil {
		t.Fatal(err)
	}
	if result := Probe(context.Background(), profile, "v0.0.43", time.Second); result.Status != "credentials_unavailable" || calls.Load() != 1 {
		t.Fatal("unsafe credentials caused an outbound request")
	}
}

func TestProbeTimesOutAndRejectsUntrustedTLS(t *testing.T) {
	profile, _ := tlsProfile(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	if result := Probe(context.Background(), profile, "v0.0.43", 100*time.Millisecond); result.Status != "timeout" {
		t.Fatalf("ignored timeout: %s", result.Status)
	}
	profile.tls = profile.tls.Clone()
	profile.tls.RootCAs = nil
	if result := Probe(context.Background(), profile, "v0.0.43", time.Second); result.Status != "tls_failed" {
		t.Fatalf("untrusted TLS accepted: %s", result.Status)
	}
}

func TestProbeBoundsSlowResponseBody(t *testing.T) {
	profile, _ := tlsProfile(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	if result := Probe(context.Background(), profile, "v0.0.43", 100*time.Millisecond); result.Status != "timeout" {
		t.Fatalf("slow response body ignored deadline: %s", result.Status)
	}
}

func TestLoadRegistryRejectsUnsafeFilesAndTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	valid := `{"profiles":{"local":{"label":"Local","endpoint":"unix:///run/rest.sock","authType":"local","apiVersions":["v0.0.43"]}}}`
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if registry, err := LoadRegistry(path); err != nil || len(registry.Profiles) != 1 {
		t.Fatalf("load valid registry: %v", err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(path); err == nil {
		t.Fatal("accepted writable registry")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(valid+` {}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(path); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	if _, err := LoadRegistry("relative.json"); err == nil {
		t.Fatal("accepted relative registry")
	}
}

func TestRegistryRejectsUnapprovedTransportsAndDoesNotExportSecrets(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:6820", "https://example.com/path", "https://user:fixture@example.com", "unix:///tmp/../root.sock"} {
		_, err := NewRegistry(map[string]Profile{"test": {Label: "Test", Endpoint: endpoint, AuthType: "jwt", Username: "monitor", TokenFile: "/private/token", Versions: []string{"v0.0.43"}}})
		if err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	profile, _ := tlsProfile(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, validPing) })
	registry, err := NewRegistry(map[string]Profile{"test": profile})
	if err != nil {
		t.Fatal(err)
	}
	view := fmt.Sprintf("%+v", registry.Views())
	if strings.Contains(view, profile.TokenFile) || strings.Contains(view, profile.CAFile) || strings.Contains(view, profile.Username) {
		t.Fatal("credential metadata leaked")
	}
}
