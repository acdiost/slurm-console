package models

import "encoding/json"

type Reservation struct {
	State       string          `json:"state"`
	Name        string          `json:"name"`
	StartTime   *int64          `json:"startTime"`
	EndTime     *int64          `json:"endTime"`
	Nodes       string          `json:"nodes"`
	NodeNames   []string        `json:"nodeNames"`
	NodeCount   int64           `json:"nodeCount"`
	CoreCount   int64           `json:"coreCount"`
	Users       string          `json:"users"`
	Accounts    string          `json:"accounts"`
	Groups      string          `json:"groups"`
	Partition   string          `json:"partition"`
	Features    string          `json:"features"`
	Licenses    string          `json:"licenses"`
	BurstBuffer string          `json:"burstBuffer"`
	TRES        string          `json:"tres"`
	Flags       []string        `json:"flags"`
	Editable    bool            `json:"editable"`
	Raw         json.RawMessage `json:"-"`
}
