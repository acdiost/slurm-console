package models

import "encoding/json"

type TRES struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	Unit     string `json:"unit"`
	Editable bool   `json:"editable"`
}
type TRESAmount struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	State string `json:"state"`
}
type TRESTarget struct {
	Kind        string                  `json:"kind"`
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Cluster     string                  `json:"cluster"`
	Account     string                  `json:"account"`
	User        string                  `json:"user"`
	Partition   string                  `json:"partition"`
	Relative    bool                    `json:"relative"`
	Limits      map[string][]TRESAmount `json:"limits"`
	Fingerprint string                  `json:"-"`
	Raw         json.RawMessage         `json:"-"`
}
type TRESSnapshot struct {
	Cluster       string       `json:"cluster"`
	Resources     []TRES       `json:"resources"`
	Targets       []TRESTarget `json:"targets"`
	ReferencedQOS []string     `json:"referencedQos"`
}
