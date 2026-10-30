package models

// History values come from slurmdbd, not current controller state or telemetry.
type HistoryExit struct {
	Status     []string `json:"status"`
	Code       Number   `json:"code"`
	Signal     Number   `json:"signal"`
	SignalName string   `json:"signalName"`
}
type HistoryTRES struct {
	ID    string  `json:"id"`
	Type  string  `json:"type"`
	Name  string  `json:"name"`
	Count *string `json:"count"`
}
type HistoryStep struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	States          []string      `json:"states"`
	Nodes           string        `json:"nodes"`
	Tasks           Number        `json:"tasks"`
	StartTime       Number        `json:"startTime"`
	EndTime         Number        `json:"endTime"`
	ElapsedSeconds  Number        `json:"elapsedSeconds"`
	TotalCPUSeconds *float64      `json:"totalCpuSeconds"`
	Exit            HistoryExit   `json:"exit"`
	AllocatedTRES   []HistoryTRES `json:"allocatedTres"`
	ConsumedAverage []HistoryTRES `json:"consumedAverage"`
	ConsumedTotal   []HistoryTRES `json:"consumedTotal"`
}
type HistoryJob struct {
	Key              string        `json:"key"`
	ID               string        `json:"id"`
	Cluster          string        `json:"cluster"`
	Name             string        `json:"name"`
	User             string        `json:"user"`
	Account          string        `json:"account"`
	Partition        string        `json:"partition"`
	QOS              string        `json:"qos"`
	States           []string      `json:"states"`
	Reason           string        `json:"reason"`
	Nodes            string        `json:"nodes"`
	SubmitTime       Number        `json:"submitTime"`
	StartTime        Number        `json:"startTime"`
	EndTime          Number        `json:"endTime"`
	ElapsedSeconds   Number        `json:"elapsedSeconds"`
	SuspendedSeconds Number        `json:"suspendedSeconds"`
	TimeLimitMinutes Number        `json:"timeLimitMinutes"`
	TotalCPUSeconds  *float64      `json:"totalCpuSeconds"`
	ArrayJobID       Number        `json:"arrayJobId"`
	ArrayTaskID      Number        `json:"arrayTaskId"`
	HetJobID         Number        `json:"hetJobId"`
	HetJobOffset     Number        `json:"hetJobOffset"`
	Exit             HistoryExit   `json:"exit"`
	DerivedExit      HistoryExit   `json:"derivedExit"`
	AllocatedTRES    []HistoryTRES `json:"allocatedTres"`
	RequestedTRES    []HistoryTRES `json:"requestedTres"`
	Steps            []HistoryStep `json:"steps"`
}
type HistorySnapshot struct {
	Cluster string       `json:"cluster"`
	Start   int64        `json:"start"`
	End     int64        `json:"end"`
	Items   []HistoryJob `json:"items"`
}
