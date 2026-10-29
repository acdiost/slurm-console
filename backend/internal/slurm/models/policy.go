package models

type Decimal struct {
	Value    *float64 `json:"value"`
	Infinite bool     `json:"infinite"`
}

type QOS struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	Priority             Number   `json:"priority"`
	UsageFactor          Decimal  `json:"usageFactor"`
	MaxJobsPerUser       Number   `json:"maxJobsPerUser"`
	MaxSubmitJobsPerUser Number   `json:"maxSubmitJobsPerUser"`
	MaxWallMinutes       Number   `json:"maxWallMinutes"`
	Flags                []string `json:"flags"`
	PreemptModes         []string `json:"preemptModes"`
	Fingerprint          string   `json:"-"`
	UnmanagedFingerprint string   `json:"-"`
}

type PolicySnapshot struct {
	Cluster      string        `json:"cluster"`
	QOS          []QOS         `json:"qos"`
	Associations []Association `json:"associations"`
}

type Fairshare struct {
	ID               string  `json:"id"`
	Cluster          string  `json:"cluster"`
	Account          string  `json:"account"`
	User             string  `json:"user"`
	Parent           string  `json:"parent"`
	Partition        string  `json:"partition"`
	Shares           Number  `json:"shares"`
	NormalizedShares Decimal `json:"normalizedShares"`
	RawUsage         string  `json:"rawUsage"`
	NormalizedUsage  Decimal `json:"normalizedUsage"`
	EffectiveUsage   Decimal `json:"effectiveUsage"`
	Factor           Decimal `json:"factor"`
	Level            Decimal `json:"level"`
}
