package gpumetrics

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEfficiencyCoverageLowEvidenceIdentityAndMissing(t *testing.T) {
	calls := 0
	p, close := profile(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/query_range" || !strings.Contains(q.Get("query"), `cluster="alpha"`) || !strings.Contains(q.Get("query"), "[60s]") {
			t.Error(r.URL)
		}
		start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
		rows := []map[string]any{}
		add := func(metric, uuid string, n int, value string) {
			x := series(metric, uuid, start, value)
			a := []any{}
			for i := 0; i < n; i++ {
				a = append(a, []any{start + int64(i)*60, value})
			}
			x["values"] = a
			rows = append(rows, x)
		}
		switch {
		case strings.Contains(q.Get("query"), metricNames[0]+"{"):
			add(metricNames[0], "GPU-zero", 60, "0")
			add(metricNames[0], "GPU-boundary", 48, "10")
			add(metricNames[0], "GPU-sparse", 1, "0")
			add(metricNames[0], "GPU-busy", 60, "100")
			add(metricNames[0], "GPU-dup", 60, "10")
			add(metricNames[0], "GPU-dup", 60, "10")
			add(metricNames[0], "MIG-one", 60, "10")
		case strings.Contains(q.Get("query"), metricNames[1]+"{"):
			add(metricNames[1], "GPU-zero", 60, "2048")
		case strings.Contains(q.Get("query"), metricNames[2]+"{"):
			add(metricNames[2], "GPU-zero", 60, "2048")
		case strings.Contains(q.Get("query"), metricNames[5]+"{"):
			add(metricNames[5], "GPU-zero", 60, "100")
		}
		output(w, rows)
	})
	defer close()
	v, e := LoadEfficiency(context.Background(), p, "1h", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if calls != 5 || v.ExpectedSamples != 60 || len(v.Points) != 60 || v.IgnoredSeries != 1 || v.Summary.ObservedDevices != 5 || v.Summary.Low != 2 || v.Summary.Insufficient != 1 || v.Summary.Ambiguous != 1 || v.Summary.Active != 1 {
		t.Fatal(calls, v)
	}
	by := map[string]EfficiencyDevice{}
	for _, d := range v.Items {
		by[d.UUID] = d
	}
	zero := by["GPU-zero"]
	if zero.Utilization.Mean == nil || *zero.Utilization.Mean != 0 || zero.MemoryPercent.Mean == nil || *zero.MemoryPercent.Mean != 50 || zero.PowerW.Mean == nil || *zero.PowerW.Mean != 100 {
		t.Fatal(zero)
	}
	if by["GPU-boundary"].Utilization.CoveragePercent != 80 || by["GPU-sparse"].Status != "insufficient" || by["GPU-dup"].Utilization.Mean != nil || v.Points[0].Devices != 4 || v.Points[59].Devices != 2 {
		t.Fatal(v)
	}
	for _, rangeName := range []string{"6h", "24h"} {
		w, e := efficiencyWindow(rangeName, time.Now())
		if e != nil || w.ExpectedSamples != int((w.End-w.Start)/w.Step) {
			t.Fatal(w, e)
		}
	}
}
func TestEfficiencyRejectsForeignAndMisalignedPoints(t *testing.T) {
	for _, mode := range []string{"foreign", "misaligned", "warning"} {
		t.Run(mode, func(t *testing.T) {
			p, close := profile(t, func(w http.ResponseWriter, r *http.Request) {
				start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
				x := series(metricNames[0], "GPU-one", start, "10")
				switch mode {
				case "foreign":
					x["metric"].(map[string]string)["cluster"] = "foreign"
				case "misaligned":
					x["values"] = []any{[]any{start + 1, "10"}}
				case "warning":
					_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]},"warnings":["partial"]}`))
					return
				}
				output(w, []map[string]any{x})
			})
			defer close()
			if _, e := LoadEfficiency(context.Background(), p, "1h", time.Now()); e == nil {
				t.Fatal("incomplete efficiency accepted")
			}
		})
	}
}
func TestEfficiencyPercentilesAndThreshold(t *testing.T) {
	a, b := 20.0, 100.0
	s := statistics([]*float64{&a, nil, &b}, 3)
	if s.Samples != 2 || *s.Mean != 60 || *s.P95 != 100 || *s.Peak != 100 {
		t.Fatal(s)
	}
	if s := statistics(nil, 60); s.Mean != nil || s.CoveragePercent != 0 {
		t.Fatal(s)
	}
	if _, e := efficiencyWindow("7d", time.Now()); e == nil {
		t.Fatal("unbounded range")
	}
}

func TestEfficiencyLowFractionAndMemoryValidity(t *testing.T) {
	p, close := profile(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
		rows := []map[string]any{}
		for _, metric := range []string{metricNames[0], metricNames[1], metricNames[3]} {
			if !strings.Contains(q.Get("query"), metric+"{") {
				continue
			}
			for _, uuid := range []string{"GPU-fraction", "GPU-twenty"} {
				x := series(metric, uuid, start, "0")
				values := []any{}
				for i := 0; i < 60; i++ {
					v := "20"
					if uuid == "GPU-fraction" {
						v = "10"
						if i >= 48 {
							v = "100"
						}
					}
					if metric == metricNames[1] {
						v = "20"
					}
					if metric == metricNames[3] {
						v = "0"
					}
					values = append(values, []any{start + int64(i)*60, v})
				}
				x["values"] = values
				rows = append(rows, x)
			}
		}
		output(w, rows)
	})
	defer close()
	v, e := LoadEfficiency(context.Background(), p, "1h", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if v.Summary.Low != 1 || v.Summary.Active != 1 {
		t.Fatal(v.Summary)
	}
	for _, d := range v.Items {
		if d.MemoryPercent.Mean != nil {
			t.Fatal("zero denominator became valid", d)
		}
		if d.UUID == "GPU-fraction" && (*d.Utilization.Mean != 28 || *d.LowSamplePercent != 80 || d.Status != "low") {
			t.Fatal(d)
		}
		if d.UUID == "GPU-twenty" && d.LowSamples != 0 {
			t.Fatal("20 percent counted as low", d)
		}
	}
}
