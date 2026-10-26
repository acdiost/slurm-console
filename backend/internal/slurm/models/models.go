// Package models contains platform DTOs independent of PocketBase and Slurm wire types.
package models

type Number struct {
	Value    *int64 `json:"value"`
	Infinite bool   `json:"infinite"`
}
type Controller struct {
	Hostname string `json:"hostname"`
	State    string `json:"state"`
	Role     string `json:"role"`
}
type Health struct {
	Status        string       `json:"status"`
	ServerVersion string       `json:"serverVersion"`
	APIVersion    string       `json:"apiVersion"`
	Cluster       string       `json:"cluster"`
	Controllers   []Controller `json:"controllers"`
	Capabilities  []string     `json:"capabilities"`
}
type Job struct {
	UserID           *int64 `json:"userId"`
	QOS              string `json:"qos"`
	Priority         Number `json:"priority"`
	MemoryPerNodeMiB Number `json:"memoryPerNodeMiB"`
	MemoryPerCPUMiB  Number `json:"memoryPerCpuMiB"`
	HetJobID         Number `json:"hetJobId"`
	HetJobOffset     Number `json:"hetJobOffset"`
	HetJobIDSet      string `json:"hetJobIdSet"`
	ArrayTaskString  string `json:"arrayTaskString"`
	RestartCount     *int64 `json:"restartCount"`

	ID               string   `json:"id"`
	Name             string   `json:"name"`
	User             string   `json:"user"`
	Account          string   `json:"account"`
	Partition        string   `json:"partition"`
	States           []string `json:"states"`
	Reason           string   `json:"reason"`
	Nodes            string   `json:"nodes"`
	CPUs             Number   `json:"cpus"`
	NodeCount        Number   `json:"nodeCount"`
	TimeLimitMinutes Number   `json:"timeLimitMinutes"`
	SubmitTime       Number   `json:"submitTime"`
	StartTime        Number   `json:"startTime"`
	EndTime          Number   `json:"endTime"`
	ArrayJobID       Number   `json:"arrayJobId"`
	ArrayTaskID      Number   `json:"arrayTaskId"`
}
type Node struct {
	Address         string   `json:"address"`
	Version         string   `json:"version"`
	Architecture    string   `json:"architecture"`
	OperatingSystem string   `json:"operatingSystem"`
	Comment         string   `json:"comment"`
	Reservation     string   `json:"reservation"`
	ReasonUser      string   `json:"reasonUser"`
	Features        []string `json:"features"`
	ActiveFeatures  []string `json:"activeFeatures"`
	BootTime        Number   `json:"bootTime"`
	SlurmdStartTime Number   `json:"slurmdStartTime"`
	ReasonChangedAt Number   `json:"reasonChangedAt"`
	ResumeAfter     Number   `json:"resumeAfter"`

	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	States             []string `json:"states"`
	EffectiveCPUs      int64    `json:"effectiveCpus"`
	CPUs               int64    `json:"cpus"`
	AllocatedCPUs      int64    `json:"allocatedCpus"`
	MemoryMiB          int64    `json:"memoryMiB"`
	AllocatedMemoryMiB int64    `json:"allocatedMemoryMiB"`
	FreeMemoryMiB      Number   `json:"freeMemoryMiB"`
	Partitions         []string `json:"partitions"`
	Reason             string   `json:"reason"`
	GRES               string   `json:"gres"`
	GRESUsed           string   `json:"gresUsed"`
}
type Partition struct {
	Name               string   `json:"name"`
	States             []string `json:"states"`
	Nodes              string   `json:"nodes"`
	NodeCount          int64    `json:"nodeCount"`
	CPUs               int64    `json:"cpus"`
	DefaultTimeMinutes Number   `json:"defaultTimeMinutes"`
	MaxTimeMinutes     Number   `json:"maxTimeMinutes"`
}
