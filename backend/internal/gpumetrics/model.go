// Package gpumetrics reads bounded, cluster-scoped DCGM telemetry from administrator-owned Prometheus sources.
package gpumetrics

type Sample struct {
	Value     *float64 `json:"value"`
	Timestamp *int64   `json:"timestamp"`
	Status    string   `json:"status"`
}
type Device struct {
	UUID        string `json:"uuid"`
	Node        string `json:"node"`
	Index       string `json:"index"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	Utilization Sample `json:"utilization"`
	MemoryUsed  Sample `json:"memoryUsedMiB"`
	MemoryFree  Sample `json:"memoryFreeMiB"`
	MemoryTotal Sample `json:"memoryTotalMiB"`
	Temperature Sample `json:"temperatureC"`
	Power       Sample `json:"powerW"`
	TotalSource string `json:"totalSource"`
}
type Snapshot struct {
	Status        string   `json:"status"`
	Message       string   `json:"message"`
	Source        string   `json:"source"`
	FetchedAt     string   `json:"fetchedAt"`
	Cached        bool     `json:"cached"`
	IgnoredSeries int      `json:"ignoredSeries"`
	Items         []Device `json:"items"`
}
type Point struct {
	Time  int64    `json:"time"`
	Value *float64 `json:"value"`
}
type History struct {
	UUID      string             `json:"uuid"`
	Node      string             `json:"node"`
	Range     string             `json:"range"`
	Start     int64              `json:"start"`
	End       int64              `json:"end"`
	Step      int64              `json:"step"`
	Series    map[string][]Point `json:"series"`
	FetchedAt string             `json:"fetchedAt"`
	Cached    bool               `json:"cached"`
}
