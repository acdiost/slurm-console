package adapter

import (
	"context"
	"encoding/json"
	"slurm-console/backend/internal/slurm/client"
	"strings"
	"testing"
)

type historyReader struct {
	raw     string
	cluster string
	queries []client.HistoryQuery
}

func (r *historyReader) Read(_ context.Context, resource client.Resource, id string) (client.Envelope, error) {
	if resource == client.Ping {
		var e client.Envelope
		e.Pings = json.RawMessage(`[{"hostname":"controller","pinged":"UP","mode":"primary"}]`)
		e.Meta.Slurm.Cluster = "alpha"
		return e, nil
	}
	var q client.HistoryQuery
	_ = json.Unmarshal([]byte(id), &q)
	r.queries = append(r.queries, q)
	return client.Envelope{Jobs: json.RawMessage(r.raw)}, nil
}

const historyWire = `{"job_id":42,"cluster":"alpha","name":"history","user":"alice","account":"research","partition":"normal","state":{"current":["COMPLETED"]},"time":{"submission":1700000010,"start":1700000020,"end":1700000030,"elapsed":10,"total":{"seconds":1,"microseconds":250000}},"exit_code":{"status":["SUCCESS"],"return_code":{"set":true,"number":0},"signal":{"id":{"set":false}}},"tres":{"allocated":[{"id":1,"type":"cpu","count":2},{"id":2,"type":"mem","count":-2},{"id":1001,"type":"gres","name":"gpu","count":9007199254740993}]},"script":"PRIVATE","environment":["PRIVATE"],"steps":[{"step":{"id":"42.batch","name":"batch"},"state":["COMPLETED"],"time":{"elapsed":10,"total":{"seconds":0,"microseconds":0}}}]}`

func TestHistoryPreservesDuplicateJobIDsStepsAndUnknowns(t *testing.T) {
	second := strings.Replace(historyWire, `"start":1700000020`, `"start":1700000040`, 1)
	r := &historyReader{raw: "[" + historyWire + "," + second + "]"}
	a, _ := New(r, Version)
	s, err := a.History(context.Background(), client.HistoryQuery{Start: 1700000000, End: 1700086400}.Key())
	if err != nil || len(s.Items) != 2 {
		t.Fatalf("%v %+v", err, s)
	}
	x := s.Items[0]
	if x.Key == s.Items[1].Key || len(x.Steps) != 1 || *x.Exit.Code.Value != 0 || x.Exit.Signal.Value != nil || x.AllocatedTRES[1].Count != nil || *x.AllocatedTRES[2].Count != "9007199254740993" || *x.TotalCPUSeconds != 1.25 || *x.Steps[0].TotalCPUSeconds != 0 {
		t.Fatalf("semantics lost %+v", x)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "PRIVATE") {
		t.Fatal("sensitive fields escaped")
	}
	if len(r.queries) != 1 || r.queries[0].Cluster != "alpha" {
		t.Fatal("controller scope absent")
	}
	r.raw = "[" + historyWire + "," + historyWire + "]"
	if _, err = a.History(context.Background(), client.HistoryQuery{Start: 1700000000, End: 1700086400}.Key()); client.Code(err) != "invalid_response" {
		t.Fatal("indistinguishable duplicate silently merged")
	}
}
func TestHistoryEnforcesClusterWindowAndSentinelTimes(t *testing.T) {
	q := client.HistoryQuery{Start: 1700000000, End: 1700086400}
	r := &historyReader{raw: "[" + strings.Replace(historyWire, `"cluster":"alpha"`, `"cluster":"beta"`, 1) + "]"}
	a, _ := New(r, Version)
	if _, err := a.History(context.Background(), q.Key()); client.Code(err) != "invalid_response" {
		t.Fatal("cross-cluster record accepted")
	}
	r.raw = "[" + strings.Replace(historyWire, `"submission":1700000010`, `"submission":1699999999`, 1) + "]"
	s, err := a.History(context.Background(), q.Key())
	if err != nil || len(s.Items) != 0 {
		t.Fatal("outside submission window shown")
	}
	r.raw = "[" + strings.Replace(historyWire, `"start":1700000020`, `"start":4294967294`, 1) + "]"
	s, err = a.History(context.Background(), q.Key())
	if err != nil || s.Items[0].StartTime.Value != nil {
		t.Fatal("NO_VAL shown as a future start")
	}
	q.User = "bob"
	s, err = a.History(context.Background(), q.Key())
	if err != nil || len(s.Items) != 0 {
		t.Fatal("source ignoring exact filter leaked rows")
	}
}
