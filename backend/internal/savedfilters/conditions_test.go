package savedfilters

import (
	"strings"
	"testing"
)

func TestConditionCatalogExcludesControlStateAndValidatesWindows(t *testing.T) {
	for _, sample := range []struct {
		module string
		values map[string]string
		valid  bool
	}{
		{"jobs", map[string]string{"state": "PENDING", "perPage": "50"}, true},
		{"jobs", map[string]string{"batch": "unsafe"}, false},
		{"fleet", map[string]string{"q": strings.Repeat("集", 128)}, true},
		{"fleet", map[string]string{"q": strings.Repeat("集", 129)}, false},
		{"audits", map[string]string{"q": strings.Repeat("a", 255)}, true},
		{"audits", map[string]string{"from": "2026-10-31T00:00:00Z", "to": "2026-10-31T00:00:00Z"}, false},
		{"gpu", map[string]string{"gpu": "GPU-id", "node": "node1"}, false},
		{"partitions", map[string]string{"partition": "detail-node"}, false},
		{"history", map[string]string{"start": "1700000000", "end": "1700086400", "jobId": "42"}, true},
		{"history", map[string]string{"start": "1700000000"}, false},
		{"history", map[string]string{"start": "1700000000", "end": "1800000000"}, false},
		{"accounts", map[string]string{"tab": "associations", "sort": "id", "kind": "user"}, true},
		{"accounts", map[string]string{"sort": "id"}, false},
		{"policies", map[string]string{"tab": "qos", "user": "alice"}, false},
		{"audits", map[string]string{"from": "2026-10-31T00:00:00Z", "to": "2026-10-30T00:00:00Z"}, false},
	} {
		_, err := Conditions(sample.module, sample.values)
		if (err == nil) != sample.valid {
			t.Fatalf("%s %+v: %v", sample.module, sample.values, err)
		}
	}
}
