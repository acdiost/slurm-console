package clusterconfig

import (
	"reflect"
	"testing"

	"slurm-console/backend/internal/slurm/protocol"
)

func TestProfileVersionsDefaultNormalizeAndKeepExplicitWhitelist(t *testing.T) {
	for _, sample := range []struct{ input, want []string }{
		{nil, protocol.Versions()},
		{[]string{"0.0.40", "v0.0.40", "v0.0.43"}, []string{"v0.0.40", "v0.0.43"}},
		{[]string{"v0.0.43"}, []string{"v0.0.43"}},
	} {
		r, err := NewRegistry(map[string]Profile{"local": {Label: "Local", Endpoint: "unix:///run/rest.sock", AuthType: "local", LocalUser: "root", Versions: sample.input}})
		if err != nil || !reflect.DeepEqual(r.Profiles["local"].Versions, sample.want) {
			t.Fatal("version whitelist changed unexpectedly", r, err)
		}
	}
}
