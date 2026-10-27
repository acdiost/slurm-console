// Package linuxusers queries Linux NSS identities independently of platform and Slurm accounts.
package linuxusers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const MaxUsers = 20000
const MaxBytes = 8 << 20

var numericName = regexp.MustCompile(`^[0-9]+$`)
var namePattern = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N}_.@+\\$-]{0,254}$`)

func ValidName(name string) bool {
	return len(name) <= 255 && namePattern.MatchString(name) && !numericName.MatchString(name)
}

type User struct {
	Name    string `json:"name"`
	UID     uint32 `json:"uid"`
	GID     uint32 `json:"gid"`
	Comment string `json:"comment"`
	Home    string `json:"home"`
	Shell   string `json:"shell"`
	Source  string `json:"source"`
	Kind    string `json:"kind"`
}
type Request struct {
	Version int    `json:"version"`
	Name    string `json:"name,omitempty"`
}
type Snapshot struct {
	Version     int     `json:"version"`
	Items       []User  `json:"items"`
	UIDMin      *uint32 `json:"uidMin"`
	Enumeration string  `json:"enumeration"`
	FetchedAt   string  `json:"fetchedAt"`
	Code        string  `json:"code,omitempty"`
}
type Failure struct{ Code, Message string }

func (e *Failure) Error() string { return e.Message }
func fail(code string) error {
	messages := map[string]string{"not_configured": "尚未配置此集群的 Linux 身份查询来源。", "disabled": "集群连接已停用。", "unavailable": "暂时无法读取 Linux 身份来源。", "invalid_response": "Linux 身份来源响应不合法或超过查询上限。", "not_found": "该 Linux 用户不存在或对查询来源不可见。", "busy": "Linux 身份查询繁忙，请稍后重试。", "changed": "集群或身份来源配置已变化，请刷新后重试。"}
	return &Failure{code, messages[code]}
}
func StrictJSON(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func ValidateSnapshot(s Snapshot, request Request) error {
	if s.UIDMin != nil && *s.UIDMin == 0 {
		return fail("invalid_response")
	}
	if s.Version != 1 || s.Items == nil || len(s.Items) > MaxUsers || s.Enumeration != "nss" && s.Enumeration != "lookup" || s.Enumeration == "lookup" && request.Name == "" || s.Enumeration == "nss" && request.Name != "" {
		return fail("invalid_response")
	}
	if _, err := time.Parse(time.RFC3339Nano, s.FetchedAt); err != nil {
		return fail("invalid_response")
	}
	if s.Code != "" {
		if s.Code == "not_found" && request.Name != "" && len(s.Items) == 0 {
			return fail("not_found")
		}
		return fail("unavailable")
	}
	if request.Name != "" && (len(s.Items) != 1 || s.Items[0].Name != request.Name) {
		return fail("invalid_response")
	}
	seen := map[string]bool{}
	for _, u := range s.Items {
		if !ValidName(u.Name) || seen[u.Name] || len(u.Comment) > 4096 || len(u.Home) > 4096 || len(u.Shell) > 4096 || strings.ContainsFunc(u.Name+u.Comment+u.Home+u.Shell, unicode.IsControl) {
			return fail("invalid_response")
		}
		seen[u.Name] = true
		if u.Source != "local" && u.Source != "nss" && u.Source != "unknown" {
			return fail("invalid_response")
		}
		expected := "unknown"
		if u.UID == 0 {
			expected = "privileged"
		} else if s.UIDMin != nil {
			expected = "regular"
			if u.UID < *s.UIDMin {
				expected = "system"
			}
		}
		if u.Kind != expected {
			return fail("invalid_response")
		}
	}
	return nil
}

type Provider interface {
	Read(context.Context, Profile, Request) (Snapshot, error)
}
