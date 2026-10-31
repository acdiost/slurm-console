// Package savedfilters owns the reviewed, read-only list condition contract.
package savedfilters

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/slurm/client"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Catalog is shared with the browser bundle; no executable expressions or paths.
//
//go:embed catalog.json
var catalogJSON []byte

type Field struct {
	Type     string   `json:"type"`
	Max      int      `json:"max"`
	MaxRunes int      `json:"maxRunes"`
	Values   []string `json:"values"`
}
type Definition struct {
	Label       string           `json:"label"`
	Scoped      bool             `json:"scoped"`
	Permissions [][]string       `json:"permissions"`
	Fields      map[string]Field `json:"fields"`
}

var catalog = func() map[string]Definition {
	var v map[string]Definition
	if json.Unmarshal(catalogJSON, &v) != nil {
		panic("invalid saved filter catalog")
	}
	return v
}()

func Spec(module string) (Definition, bool) { d, ok := catalog[module]; return d, ok }

var state = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func Conditions(module string, input map[string]string) (map[string]string, error) {
	d, ok := Spec(module)
	if !ok || input == nil || len(input) > 20 {
		return nil, errors.New("unsupported conditions")
	}
	out := map[string]string{}
	for k, v := range input {
		f, ok := d.Fields[k]
		if !ok || strings.ContainsFunc(v, unicode.IsControl) {
			return nil, errors.New("unsupported condition field")
		}
		if v == "" {
			continue
		}
		if f.Max > 0 && len(v) > f.Max || f.MaxRunes > 0 && utf8.RuneCountInString(v) > f.MaxRunes {
			return nil, errors.New("condition too long")
		}
		switch f.Type {
		case "text":
		case "enum":
			if !slices.Contains(f.Values, v) {
				return nil, errors.New("invalid choice")
			}
		case "state":
			if !state.MatchString(v) {
				return nil, errors.New("invalid state")
			}
		case "name":
			if !client.ValidAccountingName(v) {
				return nil, errors.New("invalid name")
			}
		case "jobId":
			if !client.ValidJobID(v) {
				return nil, errors.New("invalid job ID")
			}
		case "unix":
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil || n <= 0 || n > 253402300799 || strconv.FormatInt(n, 10) != v {
				return nil, errors.New("invalid timestamp")
			}
		case "date":
			t, e := time.Parse(time.RFC3339Nano, v)
			if e != nil || t.Nanosecond()%1000000 != 0 {
				return nil, errors.New("invalid date")
			}
		default:
			return nil, errors.New("unsupported type")
		}
		out[k] = v
	}
	if module == "history" {
		if (out["start"] != "") != (out["end"] != "") {
			return nil, errors.New("paired time window required")
		}
		if out["start"] != "" {
			a, _ := strconv.ParseInt(out["start"], 10, 64)
			b, _ := strconv.ParseInt(out["end"], 10, 64)
			if b <= a || b-a > 31*86400 {
				return nil, errors.New("invalid time window")
			}
		}
	}
	if module == "audits" && out["from"] != "" && out["to"] != "" {
		a, _ := time.Parse(time.RFC3339Nano, out["from"])
		b, _ := time.Parse(time.RFC3339Nano, out["to"])
		if !a.Before(b) {
			return nil, errors.New("invalid time range")
		}
	}
	if module == "accounts" && out["tab"] != "associations" || module == "policies" && out["tab"] != "fairshare" {
		for _, k := range []string{"account", "user", "partition", "kind"} {
			if out[k] != "" {
				return nil, errors.New("inactive tab field")
			}
		}
	}
	if key := out["sort"]; key != "" {
		valid := true
		if module == "accounts" {
			if out["tab"] == "associations" {
				valid = slices.Contains([]string{"id", "account", "user", "partition"}, key)
			} else {
				valid = key == "name"
			}
		}
		if module == "policies" {
			if out["tab"] == "fairshare" {
				valid = slices.Contains([]string{"account", "user", "factor", "id"}, key)
			} else {
				valid = key == "name" || key == "priority"
			}
		}
		if !valid {
			return nil, errors.New("invalid tab sort")
		}
	}
	raw, _ := json.Marshal(out)
	if len(raw) > 4096 {
		return nil, errors.New("conditions too large")
	}
	return out, nil
}
func allows(a rbac.Access, permission, cluster string) bool {
	if cluster != "" || strings.HasPrefix(permission, "platform.") {
		return a.Allows(permission, cluster)
	}
	if a.Allows(permission, "") {
		return true
	}
	for _, c := range a.Clusters {
		if a.Allows(permission, c.Cluster.ID) {
			return true
		}
	}
	return false
}
func CanEnter(a rbac.Access, module, cluster string) bool {
	d, ok := Spec(module)
	if !ok {
		return false
	}
	for _, all := range d.Permissions {
		allowed := true
		for _, p := range all {
			allowed = allowed && allows(a, p, cluster)
		}
		if allowed {
			return true
		}
	}
	return false
}
func CanUse(a rbac.Access, module, cluster string, conditions map[string]string) bool {
	required := []string{}
	switch module {
	case "accounts":
		p := "accounts.read"
		if conditions["tab"] == "slurm-users" {
			p = "slurm_users.read"
		} else if conditions["tab"] == "associations" {
			p = "associations.read"
		}
		required = []string{p}
	case "policies":
		p := "qos.read"
		if conditions["tab"] == "fairshare" {
			p = "fairshare.read"
		}
		required = []string{"associations.read", p}
	case "tres":
		required = []string{"tres.read", "associations.read"}
		if conditions["target"] == "qos" {
			required = append(required, "qos.read")
		}
	default:
		return CanEnter(a, module, cluster)
	}
	for _, p := range required {
		if !allows(a, p, cluster) {
			return false
		}
	}
	return true
}
