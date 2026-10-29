package adapter

import (
	"context"
	"encoding/json"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"testing"
)

type accountingReader struct{}

func (accountingReader) Read(_ context.Context, resource client.Resource, id string) (client.Envelope, error) {
	e := client.Envelope{}
	switch resource {
	case client.Ping:
		e.Meta.Slurm.Cluster = "alpha"
		e.Meta.Slurm.Release = "25.11.8"
		e.Pings = json.RawMessage(`[{"hostname":"c","pinged":"UP","mode":"primary"}]`)
	case client.DBAssociations:
		if id != "alpha" {
			panic("unscoped association query")
		}
		e.Associations = json.RawMessage(`[{"id":1,"cluster":"alpha","account":"research","user":"","parent_account":"root"},{"id":2,"cluster":"alpha","account":"research","user":"alice","is_default":true},{"id":3,"cluster":"foreign","account":"hidden-account","user":"hidden-user"}]`)
	case client.DBAccounts:
		e.Accounts = json.RawMessage(`[{"name":"research","description":"visible","organization":"lab"},{"name":"hidden-account","description":"hidden","organization":"foreign"}]`)
	case client.DBUsers:
		e.Users = json.RawMessage(`[{"name":"alice","default":{"account":"hidden-account"},"administrator_level":["None"]},{"name":"hidden-user","default":{"account":"hidden-account"},"administrator_level":["Administrator"]}]`)
	}
	return e, nil
}
func TestAccountingJoinsAreClusterScopedAndMaskForeignDefaults(t *testing.T) {
	a, _ := New(accountingReader{}, Version)
	accounts, err := a.GetAccounts(context.Background())
	if err != nil || len(accounts) != 1 || accounts[0].Name != "research" || accounts[0].UserCount != 1 || accounts[0].AssociationCount != 2 {
		t.Fatal(accounts, err)
	}
	users, err := a.GetSlurmUsers(context.Background())
	if err != nil || len(users) != 1 || users[0].DefaultAccount != "" || users[0].DefaultAccountScope != "outside_cluster" {
		t.Fatal(users, err)
	}
	assocs, err := a.GetAssociations(context.Background())
	if err != nil || len(assocs) != 2 {
		t.Fatal(assocs, err)
	}
	snapshot, err := a.Accounting(context.Background())
	if err != nil || len(snapshot.Users) != 2 || snapshot.Users[0].DefaultAccount != "hidden-account" {
		t.Fatal(snapshot, err)
	}
	// Counts are computed from the selected associations, never from raw global
	// user/account association arrays returned by Slurm.
	input := []models.SlurmUser{{Name: "alice", DefaultAccount: "research"}}
	if v := FilterUsers(input, assocs); len(v) != 1 || v[0].DefaultAccountScope != "cluster" {
		t.Fatal(v)
	}
}
