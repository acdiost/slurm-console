package client

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// HistoryQuery is deliberately narrower than slurmdbd's job conditions. A
// bounded submission window is mandatory; scripts/environments are never requested.
type HistoryQuery struct {
	Cluster   string `json:"cluster"`
	Start     int64  `json:"start"`
	End       int64  `json:"end"`
	User      string `json:"user"`
	Account   string `json:"account"`
	Partition string `json:"partition"`
	QOS       string `json:"qos"`
	State     string `json:"state"`
	JobID     string `json:"jobId"`
}

var HistoryStates = []string{"PENDING", "RUNNING", "SUSPENDED", "COMPLETED", "CANCELLED", "FAILED", "TIMEOUT", "NODE_FAIL", "PREEMPTED", "BOOT_FAIL", "DEADLINE", "OUT_OF_MEMORY", "REQUEUED", "REVOKED"}

func (q HistoryQuery) Valid() bool {
	if !ValidAccountingName(q.Cluster) || q.Start <= 0 || q.End <= q.Start || q.End-q.Start > 31*86400 || q.End > 253402300799 {
		return false
	}
	for _, v := range []string{q.User, q.Account, q.Partition, q.QOS} {
		if v != "" && !ValidAccountingName(v) {
			return false
		}
	}
	return (q.JobID == "" || ValidJobID(q.JobID)) && (q.State == "" || slices.Contains(HistoryStates, q.State))
}
func (q HistoryQuery) Key() string { b, _ := json.Marshal(q); return string(b) }
func historyPath(version string, q HistoryQuery) (string, bool) {
	if !q.Valid() {
		return "", false
	}
	v := url.Values{"cluster": {q.Cluster}, "start_time": {strconv.FormatInt(q.Start, 10)}, "end_time": {strconv.FormatInt(q.End, 10)}, "usage_time_as_submit_time": {"true"}, "show_duplicates": {"true"}, "skip_steps": {"false"}, "disable_truncate_usage_time": {"true"}, "disable_whole_hetjob": {"true"}}
	for k, val := range map[string]string{"users": q.User, "account": q.Account, "partition": q.Partition, "qos": q.QOS, "state": q.State, "step": q.JobID} {
		if val != "" {
			v.Set(k, val)
		}
	}
	return "/slurmdb/" + version + "/jobs/?" + v.Encode(), true
}
func validHistoryWire(r localRequest) bool {
	u, err := url.Parse(r.Path)
	if err != nil || u.Path != "/slurmdb/"+r.Version+"/jobs/" {
		return false
	}
	v, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for _, values := range v {
		if len(values) != 1 {
			return false
		}
	}
	start, e1 := strconv.ParseInt(v.Get("start_time"), 10, 64)
	end, e2 := strconv.ParseInt(v.Get("end_time"), 10, 64)
	q := HistoryQuery{Cluster: v.Get("cluster"), Start: start, End: end, User: v.Get("users"), Account: v.Get("account"), Partition: v.Get("partition"), QOS: v.Get("qos"), State: v.Get("state"), JobID: v.Get("step")}
	expected, ok := historyPath(r.Version, q)
	return e1 == nil && e2 == nil && ok && strings.EqualFold(r.Method, "GET") && !r.Write && len(r.Body) == 0 && expected == r.Path
}

// Slurm 25.11 reports an empty successful result as this exact warning. Other
// warnings can indicate incomplete data and must not silently become empty UI.
func validHistoryWarnings(e Envelope) bool {
	if len(e.Warnings) == 0 {
		return true
	}
	var jobs []json.RawMessage
	if json.Unmarshal(e.Jobs, &jobs) != nil || jobs == nil || len(jobs) != 0 || len(e.Errors) > 0 {
		return false
	}
	for _, raw := range e.Warnings {
		var w struct{ Description, Source string }
		if json.Unmarshal(raw, &w) != nil || w.Description != "slurmdb_jobs_get() found nothing" || w.Source != "_dump_jobs" {
			return false
		}
	}
	return true
}
