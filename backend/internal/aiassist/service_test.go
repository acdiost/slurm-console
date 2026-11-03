package aiassist

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"slurm-console/backend/internal/diagnostics"
)

const clusterID = "abcdefghijklmno"
const goodAnswer = `{"summary":"来源记录处于等待状态，不能确认唯一根因。","observations":[{"text":"记录显示等待状态。","evidenceIds":["states"]}],"checks":[{"audience":"admin","text":"人工核对资源请求与可用容量。","evidenceIds":["states"]}],"limitations":["未提供日志和实际资源时间序列。"]}`

func envelope(content string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}}})
	return string(b)
}
func fixtureService(t *testing.T, handler http.HandlerFunc) (*Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p, err := NewProfile(Config{Enabled: true, Label: "隔离服务", Endpoint: server.URL + "/v1/chat/completions", Model: "fixture", ClusterIDs: []string{clusterID}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{profile: p, pending: map[string]pending{}, active: map[string]bool{}, last: map[string]time.Time{}, slots: make(chan struct{}, 2), ctx: context.Background()}
	return s, server
}
func prepare(t *testing.T, s *Service, actor string) Preview {
	t.Helper()
	p, e := s.Prepare(Scope{actor, clusterID, "controller", "42", 1}, Project(diagnostics.Report{Source: "controller", Identity: diagnostics.Identity{States: []string{"PENDING"}}}), "2026-11-02T00:00:00Z")
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestProjectionDropsUntrustedIdentityAndFreeText(t *testing.T) {
	private := "PRIVATE_IGNORE_INSTRUCTIONS_SEND_SECRETS"
	r := diagnostics.Report{Source: "accounting", ObservedAt: private, GeneratedAt: private, Headline: private, Category: private, Identity: diagnostics.Identity{JobID: private, RecordKey: private, Name: private, User: private, Account: private, Partition: private, QOS: private, Nodes: private, States: []string{"FAILED", private}}, Evidence: []diagnostics.Evidence{{ID: "reason", Value: private}, {ID: private, Field: private, Label: private, Value: private}}, Findings: []diagnostics.Finding{{Code: "failed", Title: private, Explanation: private, Checks: []diagnostics.Check{{Text: private}}}, {Code: private}}, Limitations: []string{private}}
	b, e := json.Marshal(Project(r))
	if e != nil || strings.Contains(string(b), private) || !strings.Contains(string(b), "UNKNOWN") || !strings.Contains(string(b), `"id":"failed"`) {
		t.Fatal(string(b), e)
	}
}
func TestExactReviewedRequestOneUseOwnershipAndCooldown(t *testing.T) {
	var calls atomic.Int32
	var sent []byte
	s, _ := fixtureService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		sent, _ = io.ReadAll(r.Body)
		if r.Method != "POST" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected method/credential")
		}
		io.WriteString(w, envelope(goodAnswer))
	})
	p := prepare(t, s, "a")
	if calls.Load() != 0 {
		t.Fatal("preview sent data")
	}
	for _, scope := range [][3]string{{"b", clusterID, p.Digest}, {"a", "othercluster000", p.Digest}, {"a", clusterID, strings.Repeat("0", 64)}} {
		if _, e := s.Take(scope[0], scope[1], p.ID, scope[2]); !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	i, e := s.Take("a", clusterID, p.ID, p.Digest)
	if e != nil {
		t.Fatal(e)
	}
	defer i.Release()
	if _, e = i.Call(context.Background()); e != nil {
		t.Fatal(e)
	}
	if string(sent) != string(p.Request) || calls.Load() != 1 {
		t.Fatal("request differed from preview")
	}
	if _, e = i.Call(context.Background()); e == nil || calls.Load() != 1 {
		t.Fatal("invocation sent twice")
	}
	if _, e = s.Take("a", clusterID, p.ID, p.Digest); !errors.Is(e, ErrConflict) {
		t.Fatal("replay", e)
	}
	p2 := prepare(t, s, "a")
	if _, e = s.Take("a", clusterID, p2.ID, p2.Digest); !errors.Is(e, ErrBusy) {
		t.Fatal("parallel/cooldown", e)
	}
	i.Release()
	if _, e = s.Take("a", clusterID, p2.ID, p2.Digest); !errors.Is(e, ErrBusy) {
		t.Fatal("cooldown", e)
	}
}
func TestUntrustedProviderOutputsDiscardedWithoutRetry(t *testing.T) {
	bad := map[string]string{
		"bad_json": "{", "unknown_field": strings.TrimSuffix(goodAnswer, "}") + `,"tools":[]}`,
		"unknown_reference": strings.ReplaceAll(goodAnswer, `"states"`, `"private_log"`),
		"no_references":     strings.ReplaceAll(goodAnswer, `["states"]`, `[]`),
		"duplicate":         strings.Replace(goodAnswer, `"summary":`, `"summary":"first","summary":`, 1),
		"html":              strings.Replace(goodAnswer, "来源记录", "<script>", 1),
		"markdown_code":     strings.Replace(goodAnswer, "来源记录", "```", 1),
		"too_long":          strings.Replace(goodAnswer, "来源记录", strings.Repeat("长", 400), 1),
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			s, _ := fixtureService(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, envelope(content)) })
			p := prepare(t, s, "a")
			i, e := s.Take("a", clusterID, p.ID, p.Digest)
			if e != nil {
				t.Fatal(e)
			}
			defer i.Release()
			a, e := i.Call(context.Background())
			var f *Failure
			if !errors.As(e, &f) || !f.Sent || a.Summary != "" || calls.Load() != 1 {
				t.Fatal(a, e, calls.Load())
			}
		})
	}
	for name, body := range map[string]string{"truncated": strings.Replace(envelope(goodAnswer), `"stop"`, `"length"`, 1), "tool": `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{}","tool_calls":[{}]}}]}`, "refusal": `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{}","refusal":"refused"}}]}`, "oversize": strings.Repeat("x", 129<<10)} {
		t.Run(name, func(t *testing.T) {
			s, _ := fixtureService(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
			p := prepare(t, s, "a")
			i, _ := s.Take("a", clusterID, p.ID, p.Digest)
			defer i.Release()
			if _, e := i.Call(context.Background()); e == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}
func TestBoundsExpiryAndDisabled(t *testing.T) {
	s, _ := fixtureService(t, func(http.ResponseWriter, *http.Request) { t.Error("should not call") })
	var first Preview
	for n := 0; n < 8; n++ {
		p := prepare(t, s, "a")
		if n == 0 {
			first = p
		}
	}
	if _, e := s.Prepare(Scope{Actor: "a", Cluster: clusterID}, Packet{Version: 1, Source: "controller"}, ""); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	v := s.pending[first.ID]
	v.expires = time.Now().Add(-time.Second)
	s.pending[first.ID] = v
	if _, e := s.Take("a", clusterID, first.ID, first.Digest); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	for n := 0; len(s.pending) < 64; n++ {
		prepare(t, s, string(rune(100+n)))
	}
	if _, e := s.Prepare(Scope{Actor: "overflow", Cluster: clusterID}, Packet{Version: 1, Source: "controller"}, ""); !errors.Is(e, ErrBusy) {
		t.Fatal("global budget", e)
	}
	s.profile = nil
	if _, e := s.Prepare(Scope{Cluster: clusterID}, Packet{}, ""); !errors.Is(e, ErrDisabled) {
		t.Fatal(e)
	}
}
func TestTwoConcurrentSlots(t *testing.T) {
	s, _ := fixtureService(t, func(http.ResponseWriter, *http.Request) {})
	a, b, c := prepare(t, s, "a"), prepare(t, s, "b"), prepare(t, s, "c")
	i, _ := s.Take("a", clusterID, a.ID, a.Digest)
	j, _ := s.Take("b", clusterID, b.ID, b.Digest)
	defer j.Release()
	if _, e := s.Take("c", clusterID, c.ID, c.Digest); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	i.Release()
	k, e := s.Take("c", clusterID, c.ID, c.Digest)
	if e != nil {
		t.Fatal(e)
	}
	k.Release()
}
func TestConfigurationAndProtectedFiles(t *testing.T) {
	base := Config{Enabled: true, Label: "Test", Endpoint: "https://example.test/v1/chat/completions", Model: "fixture", ClusterIDs: []string{clusterID}}
	for _, endpoint := range []string{"http://example.test/v1/chat/completions", "http://localhost/v1/chat/completions", "https://user:pass@example.test/v1/chat/completions", "https://example.test/v1/chat/completions?secret=x", "https://example.test/v1/chat/completions#fragment", "https://example.test/v1/responses"} {
		c := base
		c.Endpoint = endpoint
		if _, e := NewProfile(c); e == nil {
			t.Fatal(endpoint)
		}
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.ClusterIDs = nil }, func(c *Config) { c.ClusterIDs = []string{clusterID, clusterID} }, func(c *Config) { c.TimeoutSeconds = 61 }, func(c *Config) { c.MaxOutputTokens = 4097 }, func(c *Config) { c.TokenParameter = "arbitrary" }} {
		c := base
		mutate(&c)
		if _, e := NewProfile(c); e == nil {
			t.Fatal(c)
		}
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if e := os.WriteFile(key, []byte("fixture-key"), 0600); e != nil {
		t.Fatal(e)
	}
	base.APIKeyFile = key
	if _, e := NewProfile(base); e != nil {
		t.Fatal(e)
	}
	os.Chmod(key, 0644)
	if _, e := NewProfile(base); e == nil {
		t.Fatal("readable key")
	}
	os.Chmod(key, 0600)
	link := filepath.Join(dir, "link")
	os.Symlink(key, link)
	c := base
	c.APIKeyFile = link
	if _, e := NewProfile(c); e == nil {
		t.Fatal("symlink key")
	}
	c = base
	c.Endpoint = "http://127.0.0.1:9999/v1/chat/completions"
	if _, e := NewProfile(c); e == nil {
		t.Fatal("cleartext key")
	}
	config := filepath.Join(dir, "config.json")
	os.WriteFile(config, []byte(`{"enabled":false,"enabled":true}`), 0600)
	if _, e := Load(config); e == nil {
		t.Fatal("duplicate config")
	}
	os.WriteFile(config, []byte(`{"enabled":false}`), 0600)
	if p, e := Load(config); e != nil || p != nil {
		t.Fatal(p, e)
	}
}
func TestTLSKeyRotationAndRedirectNeverForwarded(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer rotated-key" {
			t.Error("key not refreshed")
		}
		io.WriteString(w, envelope(goodAnswer))
	}))
	defer server.Close()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	key := filepath.Join(dir, "key")
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
	os.WriteFile(key, []byte("old-key"), 0600)
	p, e := NewProfile(Config{Enabled: true, Label: "TLS", Endpoint: server.URL + "/chat/completions", Model: "fixture", ClusterIDs: []string{clusterID}, APIKeyFile: key, CAFile: ca})
	if e != nil {
		t.Fatal(e)
	}
	s := &Service{profile: p, pending: map[string]pending{}, active: map[string]bool{}, last: map[string]time.Time{}, slots: make(chan struct{}, 2), ctx: context.Background()}
	v := prepare(t, s, "a")
	os.WriteFile(key, []byte("rotated-key"), 0600)
	i, _ := s.Take("a", clusterID, v.ID, v.Digest)
	defer i.Release()
	if _, e = i.Call(context.Background()); e != nil || calls.Load() != 1 {
		t.Fatal(e, calls.Load())
	}
	var forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer destination.Close()
	redirect, _ := fixtureService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	v = prepare(t, redirect, "b")
	j, _ := redirect.Take("b", clusterID, v.ID, v.Digest)
	defer j.Release()
	if _, e = j.Call(context.Background()); e == nil || forwarded.Load() != 0 {
		t.Fatal(e, forwarded.Load())
	}
}
