package freeipa

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureEntry() map[string]any {
	return map[string]any{"uid": []string{"alice"}, "ipauniqueid": []string{"uuid-user"}, "givenname": []string{"Alice"}, "sn": []string{"Example"}, "nsaccountlock": false, "memberof_group": []string{"research"}, "krbpasswordexpiration": []any{map[string]string{"__datetime__": time.Now().Add(time.Hour).UTC().Format("20060102150405Z")}}}
}
func TestVerifiedHTTPSCredentialsIdentityAndFixedCommands(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.Header.Get("Referer"), "/ipa") {
			t.Error("missing referer")
		}
		if r.URL.Path == "/ipa/session/login_password" {
			_ = r.ParseForm()
			if r.Form.Get("password") != "service-secret" && r.Form.Get("password") != "user-secret" {
				w.WriteHeader(401)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "ipa_session", Value: "opaque", Secure: true, HttpOnly: true})
			return
		}
		if cookie, e := r.Cookie("ipa_session"); e != nil || cookie.Value != "opaque" {
			t.Error("missing service cookie")
		}
		var in struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Fatal("bad rpc")
		}
		var result any
		switch in.Method {
		case "user_show":
			result = map[string]any{"result": fixtureEntry()}
		case "group_find":
			result = map[string]any{"result": []any{map[string]any{"cn": []string{"trust admins"}, "ipauniqueid": []string{"group-uuid"}}}, "truncated": false}
		case "user_disable":
			writes.Add(1)
			result = map[string]any{"result": true}
		default:
			t.Errorf("unexpected method %s", in.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "error": nil, "id": 1})
	}))
	defer server.Close()
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	ca := filepath.Join(dir, "ca.pem")
	_ = os.WriteFile(secret, []byte("service-secret\n"), 0600)
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
	c, e := New(Config{URL: server.URL, Domain: "example.test", Realm: "EXAMPLE.TEST", Username: "service", PasswordFile: secret, CAFile: ca})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	u, e := c.User(ctx, "alice")
	if e != nil || u.ID != "uuid-user" || u.PasswordExpired || len(u.Version) != 64 {
		t.Fatalf("user: %#v %v", u, e)
	}
	b, _ := json.Marshal(u)
	if strings.Contains(string(b), "uuid-user") {
		t.Fatal("directory immutable identity leaked")
	}
	if e = c.Authenticate(ctx, "alice", u.ID, "user-secret"); e != nil {
		t.Fatal(e)
	}
	if e = c.Authenticate(ctx, "alice", "recreated-user", "user-secret"); !errors.Is(e, ErrDenied) {
		t.Fatal("recreated identity accepted")
	}
	groups, e := c.Groups(ctx, "")
	if e != nil || len(groups) != 1 || groups[0].Name != "trust admins" {
		t.Fatalf("groups: %#v %v", groups, e)
	}
	if e = c.Change(ctx, Change{Action: "user.disable", Target: "alice"}); e != nil || writes.Load() != 1 {
		t.Fatal("fixed write failed", e)
	}
	for _, change := range []Change{{Action: "user.disable", Target: "admin"}, {Action: "user.disable", Target: "service"}, {Action: "arbitrary", Target: "alice"}, {Action: "group.add-member", Target: "research", Member: "admin"}} {
		if e = c.Change(ctx, change); !errors.Is(e, ErrDenied) {
			t.Fatal("unsafe operation accepted")
		}
	}
	if writes.Load() != 1 {
		t.Fatal("unexpected external writes")
	}
	// A root CA is required; an unrelated trusted pool must not verify this server.
	c.http.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
	c.http.CloseIdleConnections()
	if _, e = c.User(ctx, "alice"); e == nil {
		t.Fatal("untrusted TLS accepted")
	}
}
func TestExpiryAndVersionsFailClosed(t *testing.T) {
	for _, raw := range []string{`["20000101000000Z"]`, `[{"__datetime__":"20000101000000Z"}]`} {
		var e entry
		_ = json.Unmarshal([]byte(`{"krbpasswordexpiration":`+raw+`}`), &e)
		expired, err := expired(e, "krbpasswordexpiration")
		if err != nil || !expired {
			t.Fatal("expired password not recognized", err)
		}
	}
	for _, raw := range []string{`["bad-date"]`, `[{"wrong":"date"}]`, `true`} {
		var e entry
		_ = json.Unmarshal([]byte(`{"krbpasswordexpiration":`+raw+`}`), &e)
		if _, err := expired(e, "krbpasswordexpiration"); err == nil {
			t.Fatal("malformed expiry accepted")
		}
	}
	var e entry
	b, _ := json.Marshal(fixtureEntry())
	_ = json.Unmarshal(b, &e)
	a, _ := userView(e)
	e["ipauniqueid"] = json.RawMessage(`["replacement"]`)
	bUser, _ := userView(e)
	if a.Version == bUser.Version {
		t.Fatal("recreated user has same version")
	}
}
func TestRejectsUnsafeConfiguration(t *testing.T) {
	for _, origin := range []string{"http://ipa.example.test", "https://user:secret@ipa.example.test", "https://ipa.example.test/path", "https://ipa.example.test?secret=x"} {
		if _, e := New(Config{URL: origin, Username: "service", Domain: "example.test", Realm: "EXAMPLE.TEST"}); e == nil {
			t.Fatal("unsafe origin accepted")
		}
	}
	d := t.TempDir()
	p := filepath.Join(d, "secret")
	_ = os.WriteFile(p, []byte("secret"), 0644)
	if _, e := restrictedFile(p, 4096, true); e == nil {
		t.Fatal("public service secret accepted")
	}
}

func TestMissingOrMalformedLockStateNeverBecomesEnabled(t *testing.T) {
	for _, raw := range []string{`{}`, `{"nsaccountlock":"unknown"}`, `{"nsaccountlock":[]}`, `{"nsaccountlock":null}`} {
		var e entry
		_ = json.Unmarshal([]byte(raw), &e)
		if _, err := accountLocked(e); err == nil {
			t.Fatal("unknown eligibility accepted", raw)
		}
	}
}
