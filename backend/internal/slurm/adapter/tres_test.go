package adapter

import (
	"encoding/json"
	"slurm-console/backend/internal/slurm/models"
	"testing"
)

func TestTRESDigestBindsUneditedResourceAndIgnoresOrdering(t *testing.T) {
	raw := json.RawMessage(`{"id":4,"cluster":"cluster","account":"lab","user":"alice","max":{"tres":{"total":[{"id":1,"type":"cpu","name":"","count":2},{"id":2,"type":"mem","name":"","count":512}]}}}`)
	target, err := projectTRES(models.TRESTarget{Kind: "association", Raw: raw})
	a := target.Limits["group_total"]
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || a[0].Value != "2" {
		t.Fatal(a)
	}
	hash := TRESDigest(raw, "association", "group_total", "1")
	changed := json.RawMessage(`{"id":4,"cluster":"cluster","account":"lab","user":"alice","max":{"tres":{"total":[{"id":2,"type":"mem","name":"","count":512},{"id":1,"type":"cpu","name":"","count":9007199254740993}]}}}`)
	if hash != TRESDigest(changed, "association", "group_total", "1") {
		t.Fatal("edited field or ordering affected preservation hash")
	}
	for _, bad := range []json.RawMessage{json.RawMessage(`{"id":4,"cluster":"cluster","account":"lab","user":"alice","max":{"tres":{"total":[{"id":2,"type":"mem","name":"","count":256}]}}}`), json.RawMessage(`{"id":4,"cluster":"foreign","account":"lab","user":"alice","max":{"tres":{"total":[{"id":2,"type":"mem","name":"","count":512}]}}}`)} {
		if hash == TRESDigest(bad, "association", "group_total", "1") {
			t.Fatal("unreviewed change ignored")
		}
	}
	for _, n := range []string{"9007199254740993", "18446744073709551613"} {
		v, err := tresValue(json.RawMessage(`{"id":1,"count":` + n + `}`))
		if err != nil || v.Value != n {
			t.Fatal(v, err)
		}
	}
}
