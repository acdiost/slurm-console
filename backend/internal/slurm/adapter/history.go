package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
)

type historyExit struct {
	Status     []string `json:"status"`
	ReturnCode noVal    `json:"return_code"`
	Signal     struct {
		ID   noVal
		Name string
	} `json:"signal"`
}

func (e historyExit) model() models.HistoryExit {
	s := e.Status
	if s == nil {
		s = []string{}
	}
	return models.HistoryExit{Status: s, Code: e.ReturnCode.Number, Signal: e.Signal.ID.Number, SignalName: e.Signal.Name}
}

type historyCPU struct{ Seconds, Microseconds *int64 }

func (c historyCPU) value() *float64 {
	if c.Seconds == nil || c.Microseconds == nil || *c.Seconds < 0 || *c.Seconds > 9007199254 || *c.Microseconds < 0 || *c.Microseconds >= 1000000 {
		return nil
	}
	v := float64(*c.Seconds) + float64(*c.Microseconds)/1e6
	return &v
}

type historyTRES struct {
	ID         *int64
	Type, Name string
	Count      *int64
}

func historyResources(values []historyTRES) ([]models.HistoryTRES, error) {
	if len(values) > 256 {
		return nil, invalid()
	}
	out := []models.HistoryTRES{}
	seen := map[string]bool{}
	for _, v := range values {
		if v.ID == nil || *v.ID <= 0 || v.Type == "" || len(v.Type) > 64 || len(v.Name) > 128 {
			return nil, invalid()
		}
		id := strconv.FormatInt(*v.ID, 10)
		if seen[id] {
			return nil, invalid()
		}
		seen[id] = true
		var count *string
		if v.Count != nil && *v.Count >= 0 && *v.Count != 4294967294 && *v.Count != 4294967295 && *v.Count != 9223372036854775807 {
			s := strconv.FormatInt(*v.Count, 10)
			count = &s
		}
		out = append(out, models.HistoryTRES{ID: id, Type: v.Type, Name: v.Name, Count: count})
	}
	return out, nil
}
func historyNumber(v *int64, timestamp bool) models.Number {
	if v == nil || *v < 0 || *v == 4294967294 || *v == 4294967295 || *v == 9223372036854775807 || timestamp && (*v == 0 || *v > 253402300799) {
		return models.Number{}
	}
	return models.Number{Value: v}
}

type historyStep struct {
	Step  struct{ ID, Name string }
	State []string
	Nodes struct{ Range string }
	Tasks struct{ Count *int64 }
	Time  struct {
		Start, End noVal
		Elapsed    *int64
		Total      historyCPU
	}
	Exit historyExit `json:"exit_code"`
	TRES struct {
		Allocated []historyTRES
		Consumed  struct{ Average, Total []historyTRES }
	}
}
type historyJob struct {
	ID                                                  *int64 `json:"job_id"`
	Cluster, Name, User, Account, Partition, QOS, Nodes string
	State                                               struct {
		Current []string
		Reason  string
	}
	Time struct {
		Elapsed, Suspended, Submission, Start, End *int64
		Limit                                      noVal
		Total                                      historyCPU
	}
	Array struct {
		ID   *int64 `json:"job_id"`
		Task noVal  `json:"task_id"`
	}
	Het struct {
		ID     *int64 `json:"job_id"`
		Offset noVal  `json:"job_offset"`
	}
	Exit        historyExit `json:"exit_code"`
	DerivedExit historyExit `json:"derived_exit_code"`
	TRES        struct{ Allocated, Requested []historyTRES }
	Steps       []historyStep
}

func (a *Adapter) History(ctx context.Context, key string) (models.HistorySnapshot, error) {
	var q client.HistoryQuery
	if json.Unmarshal([]byte(key), &q) != nil {
		return models.HistorySnapshot{}, invalid()
	}
	cluster, err := a.AccountingCluster(ctx)
	if err != nil {
		return models.HistorySnapshot{}, err
	}
	q.Cluster = cluster
	if !q.Valid() {
		return models.HistorySnapshot{}, client.Failure("invalid_request", "历史查询必须限定最多 31 天的提交时间范围。")
	}
	env, err := a.reader.Read(ctx, client.DBJobs, q.Key())
	if err != nil {
		return models.HistorySnapshot{}, err
	}
	values, err := decode[historyJob](env.Jobs)
	if err != nil {
		return models.HistorySnapshot{}, err
	}
	if len(values) > 5000 {
		return models.HistorySnapshot{}, client.Failure("query_too_large", "记账记录超过 5000 条，请缩小提交时间或增加精确筛选。")
	}
	out := models.HistorySnapshot{Cluster: cluster, Start: q.Start, End: q.End, Items: []models.HistoryJob{}}
	seen := map[string]bool{}
	stepsCount := 0
	for _, v := range values {
		if v.Cluster != cluster || v.ID == nil || *v.ID <= 0 || !client.ValidJobID(strconv.FormatInt(*v.ID, 10)) || !validStates(v.State.Current) || v.Time.Submission == nil || *v.Time.Submission <= 0 || len(v.Name) > 4096 || len(v.Nodes) > 65536 || len(v.State.Reason) > 4096 {
			return out, invalid()
		}
		// Defend against a source ignoring exact selectors or expanding hetjobs.
		if *v.Time.Submission < q.Start || *v.Time.Submission >= q.End || q.JobID != "" && strconv.FormatInt(*v.ID, 10) != q.JobID || q.User != "" && v.User != q.User || q.Account != "" && v.Account != q.Account || q.Partition != "" && v.Partition != q.Partition || q.QOS != "" && v.QOS != q.QOS {
			continue
		}
		if q.State != "" {
			matched := false
			for _, s := range v.State.Current {
				matched = matched || s == q.State
			}
			if !matched {
				continue
			}
		}
		id := strconv.FormatInt(*v.ID, 10)
		identity, _ := json.Marshal([]any{cluster, id, v.Time.Submission, v.Time.Start, v.Array.ID, v.Array.Task.Number, v.Het.ID, v.Het.Offset.Number})
		digest := sha256.Sum256(identity)
		recordKey := hex.EncodeToString(digest[:])
		if seen[recordKey] {
			return out, client.Failure("invalid_response", "记账来源包含无法区分的重复记录，请核对 slurmdbd 数据。")
		}
		seen[recordKey] = true
		allocated, e := historyResources(v.TRES.Allocated)
		if e != nil {
			return out, e
		}
		requested, e := historyResources(v.TRES.Requested)
		if e != nil {
			return out, e
		}
		j := models.HistoryJob{Key: recordKey, ID: id, Cluster: cluster, Name: v.Name, User: v.User, Account: v.Account, Partition: v.Partition, QOS: v.QOS, States: v.State.Current, Reason: v.State.Reason, Nodes: v.Nodes, SubmitTime: historyNumber(v.Time.Submission, true), StartTime: historyNumber(v.Time.Start, true), EndTime: historyNumber(v.Time.End, true), ElapsedSeconds: historyNumber(v.Time.Elapsed, false), SuspendedSeconds: historyNumber(v.Time.Suspended, false), TimeLimitMinutes: v.Time.Limit.Number, TotalCPUSeconds: v.Time.Total.value(), ArrayJobID: historyNumber(v.Array.ID, true), ArrayTaskID: v.Array.Task.Number, HetJobID: historyNumber(v.Het.ID, true), HetJobOffset: v.Het.Offset.Number, Exit: v.Exit.model(), DerivedExit: v.DerivedExit.model(), AllocatedTRES: allocated, RequestedTRES: requested, Steps: []models.HistoryStep{}}
		stepsCount += len(v.Steps)
		if len(v.Steps) > 512 || stepsCount > 20000 {
			return out, client.Failure("query_too_large", "记账步骤过多，请缩小查询范围。")
		}
		stepSeen := map[string]bool{}
		for _, s := range v.Steps {
			if s.Step.ID == "" || len(s.Step.ID) > 128 || stepSeen[s.Step.ID] || !validStates(s.State) {
				return out, invalid()
			}
			stepSeen[s.Step.ID] = true
			allocated, e := historyResources(s.TRES.Allocated)
			if e != nil {
				return out, e
			}
			average, e := historyResources(s.TRES.Consumed.Average)
			if e != nil {
				return out, e
			}
			total, e := historyResources(s.TRES.Consumed.Total)
			if e != nil {
				return out, e
			}
			start, end := s.Time.Start.Number, s.Time.End.Number
			if start.Value != nil {
				start = historyNumber(start.Value, true)
			}
			if end.Value != nil {
				end = historyNumber(end.Value, true)
			}
			j.Steps = append(j.Steps, models.HistoryStep{ID: s.Step.ID, Name: s.Step.Name, States: s.State, Nodes: s.Nodes.Range, Tasks: historyNumber(s.Tasks.Count, false), StartTime: start, EndTime: end, ElapsedSeconds: historyNumber(s.Time.Elapsed, false), TotalCPUSeconds: s.Time.Total.value(), Exit: s.Exit.model(), AllocatedTRES: allocated, ConsumedAverage: average, ConsumedTotal: total})
		}
		out.Items = append(out.Items, j)
	}
	return out, nil
}
