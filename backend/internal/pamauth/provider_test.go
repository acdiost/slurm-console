//go:build linux || darwin

package pamauth

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHelperProtocolAndTimeout(t *testing.T) {
	for _, sample := range []struct {
		name, script string
		timeout      time.Duration
		want         error
	}{
		{"success", `cat >/dev/null; printf '%s' '{"ok":true,"username":"fixture"}'`, 5 * time.Second, nil},
		{"denied", `cat >/dev/null; printf '%s' '{"ok":false,"code":"denied"}'`, 5 * time.Second, ErrDenied},
		{"malformed", `cat >/dev/null; printf 'invalid'`, 5 * time.Second, ErrUnavailable},
		{"failure", `exit 1`, 5 * time.Second, ErrUnavailable},
		{"timeout", `exec sleep 2`, 50 * time.Millisecond, ErrUnavailable},
	} {
		t.Run(sample.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+sample.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			process, err := NewProcess(path, false)
			if err != nil {
				t.Fatal(err)
			}
			process.Timeout = sample.timeout
			result, err := process.Run(context.Background(), Request{Mode: "authenticate", Username: "fixture", Password: rand.Text()})
			if !errors.Is(err, sample.want) {
				t.Fatalf("expected %v, got %v", sample.want, err)
			}
			if err == nil && (!result.OK || result.Username != "fixture") {
				t.Fatal("invalid result")
			}
		})
	}
}

func TestHelperRejectsRelativePathAndOversizedOutput(t *testing.T) {
	if _, err := NewProcess("helper", false); err == nil {
		t.Fatal("relative helper accepted")
	}
	var output cappedOutput
	_, _ = output.Write(make([]byte, 5000))
	if !output.exceeded || output.Len() != 4096 {
		t.Fatal("output bound not enforced")
	}
}
