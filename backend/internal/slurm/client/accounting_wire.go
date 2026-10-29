package client

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

func validAccountingWire(r localRequest) bool {
	if !r.Write {
		return false
	}
	prefix := "/slurmdb/" + r.Version + "/"
	m := AccountingMutation{}
	if r.Method == "DELETE" {
		u, err := url.Parse(r.Path)
		if err != nil || u.Path != prefix+"association/" || len(r.Body) != 0 {
			return false
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return false
		}
		m = AccountingMutation{Action: "association_remove", Cluster: q.Get("cluster"), Account: q.Get("account"), User: q.Get("user"), Partition: q.Get("partition"), AssociationID: q.Get("id")}
	} else if r.Method == "POST" {
		var wire struct {
			Accounts     []struct{ Name, Description, Organization string }
			Associations []struct{ Cluster, Account, User, Partition string }
			Account      struct{ Description, Organization string }
			User         struct {
				Adminlevel     []string
				Defaultaccount string
			}
			AssociationCondition struct {
				Accounts, Clusters, Users []string
				Association               struct{ Parent string }
			} `json:"association_condition"`
		}
		if json.Unmarshal(r.Body, &wire) != nil {
			return false
		}
		switch r.Path {
		case prefix + "accounts/":
			if len(wire.Accounts) != 1 {
				return false
			}
			a := wire.Accounts[0]
			m = AccountingMutation{Action: "account_update", Cluster: "wire-validation", Account: a.Name, Description: a.Description, Organization: a.Organization}
		case prefix + "accounts_association/", prefix + "users_association/":
			cond := wire.AssociationCondition
			if len(cond.Accounts) != 1 || len(cond.Clusters) != 1 {
				return false
			}
			m = AccountingMutation{Cluster: cond.Clusters[0], Account: cond.Accounts[0]}
			if strings.HasSuffix(r.Path, "/accounts_association/") {
				m.Action = "account_create"
				m.Description = wire.Account.Description
				m.Organization = wire.Account.Organization
			} else {
				if len(cond.Users) != 1 {
					return false
				}
				m.Action = "user_create"
				m.User = cond.Users[0]
			}
		case prefix + "associations/":
			if len(wire.Associations) != 1 {
				return false
			}
			a := wire.Associations[0]
			m = AccountingMutation{Action: "association_create", Cluster: a.Cluster, Account: a.Account, User: a.User, Partition: a.Partition}
		default:
			return false
		}
	} else {
		return false
	}
	method, path, body, ok := accountingWrite(r.Version, m)
	// Equal decoded JSON also accepts harmless object key order changes, while
	// rejecting any extra fields or changed constants (e.g. administrator level).
	if !ok || method != r.Method || path != r.Path {
		return false
	}
	if len(body) == 0 {
		return len(r.Body) == 0
	}
	var a, b any
	if json.Unmarshal(body, &a) != nil || json.Unmarshal(r.Body, &b) != nil {
		return false
	}
	ca, _ := json.Marshal(a)
	cb, _ := json.Marshal(b)
	return bytes.Equal(ca, cb)
}
