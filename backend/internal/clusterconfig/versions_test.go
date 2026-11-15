package clusterconfig

import (
	"reflect"
	"sync"
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
		p, _ := r.Get("local")
		if err != nil || !reflect.DeepEqual(p.Versions, sample.want) {
			t.Fatal("version whitelist changed unexpectedly", r, err)
		}
	}
}

func TestRegistrySnapshotsAndConcurrentPublication(t *testing.T) {
	profile := Profile{Label: "Local", Endpoint: "unix:///run/rest.sock", AuthType: "local", Versions: []string{"v0.0.43"}}
	r, err := NewRegistry(map[string]Profile{"local": profile})
	if err != nil {
		t.Fatal(err)
	}
	profile.Versions[0] = "v0.0.40"
	snapshot, _ := r.Get("local")
	if snapshot.Versions[0] != "v0.0.43" {
		t.Fatal("constructor retained caller-owned slice")
	}
	snapshot.Versions[0] = "v0.0.41"
	views := r.Views()
	views[0].Versions[0] = "v0.0.42"
	current, _ := r.Get("local")
	if current.Versions[0] != "v0.0.43" {
		t.Fatal("readers can mutate live registry")
	}
	invalid := current
	invalid.Endpoint = "http://unsafe.example"
	if err := r.Set("local", invalid); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 20 {
				p, ok := r.Get("local")
				if !ok || p.Endpoint != "unix:///run/rest.sock" {
					t.Error("incomplete snapshot")
					return
				}
				if err := r.Set("local", p); err != nil {
					t.Error(err)
					return
				}
				if len(r.Views()) != 1 {
					t.Error("partial publication")
					return
				}
			}
		}()
	}
	workers.Wait()
}
