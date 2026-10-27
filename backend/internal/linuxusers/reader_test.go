package linuxusers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReaderSeparatesLocalNSSAndDiscardsPasswordFields(t *testing.T) {
	r := Reader{Run: func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "passwd" {
			t.Fatal(args)
		}
		return []byte("root:root-password-must-not-return:0:0:root:/root:/bin/bash\nworker:x:1001:1001:Local:/home/worker:/bin/bash\ndirectory@example.test:x:2001:2001:Directory:/home/directory:/bin/bash\nshadowed:x:3001:3001:Remote identity:/remote/home:/bin/bash\n"), nil
	}, File: func(path string) ([]byte, error) {
		if path == "/etc/login.defs" {
			return []byte("# ignored\nUID_MIN 1000\n"), nil
		}
		return []byte("root:x:0:0:root:/root:/bin/bash\nworker:x:1001:1001:Local:/home/worker:/bin/bash\nshadowed:x:3001:3001:Different identity:/home/shadowed:/bin/bash\n"), nil
	}}
	result, err := r.Read(context.Background(), Request{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 4 || result.Items[0].Kind != "privileged" || result.Items[1].Source != "local" || result.Items[2].Source != "nss" || result.Items[3].Source != "nss" {
		t.Fatal(result)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "root-password") || strings.Contains(string(raw), "shadowPassword") {
		t.Fatal("password retained")
	}
}
func TestReaderUnknownMetadataAndExactNonEnumeratedUser(t *testing.T) {
	r := Reader{Run: func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "passwd worker@realm" {
			t.Fatal(args)
		}
		return []byte("worker@realm:x:2500:2500::/home/worker:/bin/bash\n"), nil
	}, File: func(string) ([]byte, error) { return nil, errors.New("unreadable") }}
	result, err := r.Read(context.Background(), Request{Version: 1, Name: "worker@realm"})
	if err != nil || result.Enumeration != "lookup" || result.UIDMin != nil || result.Items[0].Source != "unknown" || result.Items[0].Kind != "unknown" {
		t.Fatal(result, err)
	}
	r.Run = func(context.Context, ...string) ([]byte, error) {
		return []byte("different:x:2500:2500::/home/different:/bin/bash\n"), nil
	}
	if _, err = r.Read(context.Background(), Request{Version: 1, Name: "worker@realm"}); !IsFailure(err, "invalid_response") {
		t.Fatal(err)
	}
}
func TestPasswdParserRejectsDuplicatesInvalidValuesAndBounds(t *testing.T) {
	for _, raw := range []string{"bad", "worker:x:-1:1::/:/bin/bash\n", "worker:x:4294967296:1::/:/bin/bash\n", "worker:x:1:1::/:/bin/bash\nworker:x:2:2::/:/bin/bash\n", strings.Repeat("x", MaxBytes+1)} {
		if _, err := ParsePasswd([]byte(raw)); err == nil {
			t.Fatal("accepted invalid NSS response")
		}
	}
	valid := Snapshot{Version: 1, Items: []User{{Name: "worker", UID: 1, GID: 1, Source: "unknown", Kind: "unknown"}}, Enumeration: "nss", FetchedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	valid.Items[0].Home = "/home/worker\x00"
	if ValidateSnapshot(valid, Request{Version: 1}) == nil {
		t.Fatal("control bytes accepted")
	}
	for _, name := range []string{"-option", "../root", "root;id", "worker\nroot", "", "1"} {
		if ValidName(name) {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"worker", "worker@domain", "DOMAIN\\worker", "_system$", "用户@目录", "2worker", "first+tag"} {
		if !ValidName(name) {
			t.Fatal(name)
		}
	}
}
func TestReaderProtocolRejectsUnknownFieldsAndTrailingInput(t *testing.T) {
	for _, raw := range []string{`{"version":1,"command":"id"}`, `{"version":1} {"version":1}`, strings.Repeat(" ", 4097)} {
		var out strings.Builder
		if ServeReader(strings.NewReader(raw), &out) == 0 || out.Len() != 0 {
			t.Fatal("invalid protocol accepted")
		}
	}
}
