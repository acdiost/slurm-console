package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var accountingName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

func ValidAccountingName(value string) bool {
	return accountingName.MatchString(value) && !strings.EqualFold(value, "ALL")
}

type AccountingMutation struct {
	Action                  string   `json:"action"`
	Cluster                 string   `json:"cluster"`
	Account                 string   `json:"account"`
	User                    string   `json:"user"`
	Partition               string   `json:"partition"`
	AssociationID           string   `json:"associationId"`
	Description             string   `json:"description"`
	Organization            string   `json:"organization"`
	QOS                     *QOSSpec `json:"qos,omitempty"`
	QOSPreemptModes         []string `json:"qosPreemptModes,omitempty"`
	QOSUnmanagedFingerprint string   `json:"qosUnmanagedFingerprint,omitempty"`
}

func (m AccountingMutation) Valid() bool {
	if m.Action == "qos_create" || m.Action == "qos_update" {
		return ValidAccountingName(m.Cluster) && m.Account == "" && m.User == "" && m.Partition == "" && m.AssociationID == "" && m.Description == "" && m.Organization == "" && m.QOS != nil && m.QOS.Valid(m.Action) && validQOSPreservation(m)
	}
	if m.QOS != nil || len(m.QOSPreemptModes) != 0 || m.QOSUnmanagedFingerprint != "" {
		return false
	}
	if !ValidAccountingName(m.Cluster) || !ValidAccountingName(m.Account) || strings.EqualFold(m.Account, "root") {
		return false
	}
	text := func(s string) bool { return utf8.RuneCountInString(s) <= 200 && !strings.ContainsAny(s, "\r\n\x00") }
	switch m.Action {
	case "account_create", "account_update":
		return (m.Action != "account_create" || m.Account == strings.ToLower(m.Account)) && m.User == "" && m.Partition == "" && m.AssociationID == "" && text(m.Description) && text(m.Organization) && strings.TrimSpace(m.Description) != "" && strings.TrimSpace(m.Organization) != ""
	case "user_create", "association_create":
		return (m.Action != "user_create" || m.User == strings.ToLower(m.User)) && ValidAccountingName(m.User) && m.User != "root" && (m.Partition == "" || ValidAccountingName(m.Partition)) && m.AssociationID == "" && m.Description == "" && m.Organization == "" && (m.Action != "user_create" || m.Partition == "")
	case "association_remove":
		return ValidAccountingName(m.User) && m.User != "root" && (m.Partition == "" || ValidAccountingName(m.Partition)) && ValidJobID(m.AssociationID) && m.Description == "" && m.Organization == ""
	}
	return false
}

func accountingWrite(version string, m AccountingMutation) (string, string, []byte, bool) {
	if m.Action == "qos_create" || m.Action == "qos_update" {
		return qosWrite(version, m)
	}
	if !m.Valid() {
		return "", "", nil, false
	}
	prefix := "/slurmdb/" + version + "/"
	var payload any
	endpoint := prefix + "accounts/"
	switch m.Action {
	case "account_create":
		endpoint = prefix + "accounts_association/"
		payload = map[string]any{"account": map[string]any{"description": m.Description, "organization": m.Organization}, "association_condition": map[string]any{"accounts": []string{m.Account}, "clusters": []string{m.Cluster}, "association": map[string]any{"parent": "root"}}}
	case "account_update":
		payload = map[string]any{"accounts": []any{map[string]any{"name": m.Account, "description": m.Description, "organization": m.Organization}}}
	case "user_create":
		endpoint = prefix + "users_association/"
		payload = map[string]any{"user": map[string]any{"adminlevel": []string{"None"}, "defaultaccount": m.Account}, "association_condition": map[string]any{"users": []string{m.User}, "accounts": []string{m.Account}, "clusters": []string{m.Cluster}}}
	case "association_create":
		endpoint = prefix + "associations/"
		assoc := map[string]any{"cluster": m.Cluster, "account": m.Account, "user": m.User}
		if m.Partition != "" {
			assoc["partition"] = m.Partition
		}
		payload = map[string]any{"associations": []any{assoc}}
	case "association_remove":
		query := url.Values{"id": {m.AssociationID}, "cluster": {m.Cluster}, "account": {m.Account}, "user": {m.User}}
		// Preserve the empty partition dimension too; it denotes a general
		// association rather than permission to delete every partition.
		query.Set("partition", m.Partition)
		return http.MethodDelete, prefix + "association/?" + query.Encode(), nil, true
	}
	raw, _ := json.Marshal(payload)
	return http.MethodPost, endpoint, raw, true
}
func (c *Client) MutateAccounting(ctx context.Context, m AccountingMutation) (bool, error) {
	method, path, raw, ok := accountingWrite(c.config.Version, m)
	if !ok {
		return false, Failure("invalid_request", "记账操作参数不合法。")
	}
	sent := false
	_, err := c.request(ctx, method, path, raw, &sent)
	return sent, err
}
