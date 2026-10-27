package linuxusers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Reader uses only public NSS passwd metadata; password fields are discarded.
// It never reads shadow, authenticates users, or changes Linux accounts.
type Reader struct {
	Run  func(context.Context, ...string) ([]byte, error)
	File func(string) ([]byte, error)
}

func NewReader() Reader {
	return Reader{Run: func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "/usr/bin/getent", args...)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		out := new(cappedOutput)
		cmd.Stdout = out
		err := cmd.Run()
		if out.exceeded {
			return nil, fail("invalid_response")
		}
		return out.Bytes(), err
	}, File: func(path string) ([]byte, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
		if len(b) > MaxBytes {
			return nil, fail("invalid_response")
		}
		return b, err
	}}
}
func ParsePasswd(raw []byte) ([]User, error) {
	if len(raw) > MaxBytes {
		return nil, fail("invalid_response")
	}
	values := []User{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) != 7 || !ValidName(fields[0]) || seen[fields[0]] || len(values) >= MaxUsers {
			return nil, fail("invalid_response")
		}
		uid, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			return nil, fail("invalid_response")
		}
		gid, err := strconv.ParseUint(fields[3], 10, 32)
		if err != nil {
			return nil, fail("invalid_response")
		}
		seen[fields[0]] = true
		values = append(values, User{Name: fields[0], UID: uint32(uid), GID: uint32(gid), Comment: fields[4], Home: fields[5], Shell: fields[6], Source: "unknown", Kind: "unknown"})
	}
	return values, nil
}
func uidMinimum(raw []byte) *uint32 {
	for _, line := range strings.Split(string(raw), "\n") {
		v := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(v) == 2 && v[0] == "UID_MIN" {
			n, e := strconv.ParseUint(v[1], 10, 32)
			if e == nil && n > 0 {
				value := uint32(n)
				return &value
			}
		}
	}
	return nil
}
func (r Reader) Read(ctx context.Context, request Request) (Snapshot, error) {
	result := Snapshot{Version: 1, Items: []User{}, Enumeration: "nss", FetchedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if request.Version != 1 || request.Name != "" && !ValidName(request.Name) {
		return result, fail("invalid_response")
	}
	args := []string{"passwd"}
	if request.Name != "" {
		args = append(args, request.Name)
		result.Enumeration = "lookup"
	}
	raw, err := r.Run(ctx, args...)
	if err != nil {
		var exit *exec.ExitError
		if request.Name != "" && errors.As(err, &exit) && exit.ExitCode() == 2 {
			return result, fail("not_found")
		}
		return result, fail("unavailable")
	}
	values, err := ParsePasswd(raw)
	if err != nil {
		return result, err
	}
	locals := map[string]User{}
	known := false
	if localRaw, e := r.File("/etc/passwd"); e == nil {
		if local, e := ParsePasswd(localRaw); e == nil {
			known = true
			for _, u := range local {
				locals[u.Name] = u
			}
		}
	}
	if data, e := r.File("/etc/login.defs"); e == nil {
		result.UIDMin = uidMinimum(data)
	}
	for i, u := range values {
		if known {
			values[i].Source = "nss"
			if l, ok := locals[u.Name]; ok && l.UID == u.UID && l.GID == u.GID && l.Home == u.Home && l.Shell == u.Shell && l.Comment == u.Comment {
				values[i].Source = "local"
			}
		}
		if u.UID == 0 {
			values[i].Kind = "privileged"
		} else if result.UIDMin != nil {
			values[i].Kind = "regular"
			if u.UID < *result.UIDMin {
				values[i].Kind = "system"
			}
		}
	}
	result.Items = values
	result.FetchedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := ValidateSnapshot(result, request); err != nil {
		return result, err
	}
	return result, nil
}
func ServeReader(input io.Reader, output io.Writer) int {
	raw, err := io.ReadAll(io.LimitReader(input, 4097))
	var request Request
	if err != nil || len(raw) > 4096 || StrictJSON(raw, &request) != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result, err := NewReader().Read(ctx, request)
	if err != nil {
		result.Items = []User{}
		result.Code = "unavailable"
		if f, ok := err.(*Failure); ok {
			result.Code = f.Code
		}
	}
	if json.NewEncoder(output).Encode(result) != nil {
		return 1
	}
	return 0
}
