package gpumetrics

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"slices"
	"strconv"
	"time"
)

// Efficiency reports sampled physical-device telemetry, never job productivity.
type MetricStats struct {
	Samples         int      `json:"samples"`
	CoveragePercent float64  `json:"coveragePercent"`
	Mean            *float64 `json:"mean"`
	P95             *float64 `json:"p95"`
	Peak            *float64 `json:"peak"`
}
type EfficiencyDevice struct {
	UUID             string      `json:"uuid"`
	Node             string      `json:"node"`
	Index            string      `json:"index"`
	Model            string      `json:"model"`
	Status           string      `json:"status"`
	Utilization      MetricStats `json:"utilization"`
	MemoryPercent    MetricStats `json:"memoryPercent"`
	PowerW           MetricStats `json:"powerW"`
	LowSamples       int         `json:"lowSamples"`
	LowSamplePercent *float64    `json:"lowSamplePercent"`
}
type EfficiencyPoint struct {
	Time            int64    `json:"time"`
	MeanUtilization *float64 `json:"meanUtilization"`
	Devices         int      `json:"devices"`
}
type EfficiencySummary struct {
	ObservedDevices int      `json:"observedDevices"`
	Low             int      `json:"low"`
	Active          int      `json:"active"`
	Insufficient    int      `json:"insufficient"`
	Ambiguous       int      `json:"ambiguous"`
	ValidSamples    int      `json:"validSamples"`
	MeanUtilization *float64 `json:"meanUtilization"`
}
type Efficiency struct {
	Status                  string             `json:"status"`
	Source                  string             `json:"source"`
	Range                   string             `json:"range"`
	Start                   int64              `json:"start"`
	End                     int64              `json:"end"`
	Step                    int64              `json:"step"`
	ExpectedSamples         int                `json:"expectedSamples"`
	LowThresholdPercent     int                `json:"lowThresholdPercent"`
	MinimumCoveragePercent  int                `json:"minimumCoveragePercent"`
	MinimumLowSamplePercent int                `json:"minimumLowSamplePercent"`
	FetchedAt               string             `json:"fetchedAt"`
	Cached                  bool               `json:"cached"`
	IgnoredSeries           int                `json:"ignoredSeries"`
	Summary                 EfficiencySummary  `json:"summary"`
	Points                  []EfficiencyPoint  `json:"points"`
	Items                   []EfficiencyDevice `json:"items"`
}

func efficiencyWindow(window string, now time.Time) (Efficiency, error) {
	span, step := int64(3600), int64(60)
	switch window {
	case "1h":
	case "6h":
		span, step = 21600, 300
	case "24h":
		span, step = 86400, 600
	default:
		return Efficiency{}, errors.New("invalid efficiency window")
	}
	end := now.Unix() / step * step
	return Efficiency{Status: "not_configured", Range: window, Start: end - span, End: end, Step: step, ExpectedSamples: int(span / step), LowThresholdPercent: 20, MinimumCoveragePercent: 80, MinimumLowSamplePercent: 80, FetchedAt: now.UTC().Format(time.RFC3339Nano), Items: []EfficiencyDevice{}, Points: []EfficiencyPoint{}}, nil
}
func statistics(values []*float64, expected int) MetricStats {
	s := MetricStats{}
	a := []float64{}
	sum := 0.0
	for _, v := range values {
		if v != nil {
			a = append(a, *v)
			sum += *v
		}
	}
	s.Samples = len(a)
	s.CoveragePercent = 100 * float64(len(a)) / float64(expected)
	if len(a) > 0 {
		slices.Sort(a)
		mean := sum / float64(len(a))
		p95, peak := a[int(math.Ceil(.95*float64(len(a))))-1], a[len(a)-1]
		s.Mean, s.P95, s.Peak = &mean, &p95, &peak
	}
	return s
}
func LoadEfficiency(ctx context.Context, p Profile, window string, now time.Time) (Efficiency, error) {
	out, e := efficiencyWindow(window, now)
	if e != nil {
		return out, e
	}
	out.Status, out.Source = "ok", p.Label
	type device struct {
		info   EfficiencyDevice
		series map[string][]*float64
		seen   map[string]bool
		bad    bool
	}
	devices := map[string]*device{}
	uuidNodes := map[string]string{}
	badUUID := map[string]bool{}
	// Five bounded queries avoid a single all-metric response growing with fleet size.
	for _, metric := range []string{metricNames[0], metricNames[1], metricNames[2], metricNames[3], metricNames[5]} {
		sel := metric + "{" + p.ClusterLabel + "=" + quote(p.ClusterValue) + "}"
		rows, e := query(ctx, p, "query_range", url.Values{"query": {"last_over_time(" + sel + "[60s])"}, "start": {strconv.FormatInt(out.Start+out.Step, 10)}, "end": {strconv.FormatInt(out.End, 10)}, "step": {strconv.FormatInt(out.Step, 10)}})
		if e != nil {
			return out, e
		}
		for _, r := range rows {
			node, uuid, m, skip, e := identity(p, r)
			if e != nil {
				return out, e
			}
			if skip {
				out.IgnoredSeries++
				continue
			}
			if m != metric {
				return out, errors.New("unexpected efficiency metric")
			}
			key := node + "/" + uuid
			d := devices[key]
			if d == nil {
				if len(devices) >= 512 {
					return out, errors.New("too many efficiency devices")
				}
				d = &device{info: EfficiencyDevice{Node: node, UUID: uuid, Index: r.Metric["gpu"], Model: r.Metric["modelName"]}, series: map[string][]*float64{}, seen: map[string]bool{}}
				devices[key] = d
			}
			if prev, ok := uuidNodes[uuid]; ok && prev != node {
				badUUID[uuid] = true
			}
			uuidNodes[uuid] = node
			if d.seen[m] || d.info.Index != r.Metric["gpu"] || d.info.Model != r.Metric["modelName"] {
				d.bad = true
			}
			d.seen[m] = true
			points := make([]*float64, out.ExpectedSamples)
			last := int64(0)
			for _, v := range r.Values {
				ts, n := parsePoint(v, m, p)
				if ts < out.Start+out.Step || ts > out.End || (ts-out.Start)%out.Step != 0 || ts <= last {
					return out, errors.New("invalid efficiency timestamp")
				}
				last = ts
				points[(ts-out.Start)/out.Step-1] = n
			}
			d.series[m] = points
		}
	}
	sums := make([]float64, out.ExpectedSamples)
	counts := make([]int, out.ExpectedSamples)
	total := 0.0
	for _, d := range devices {
		utilization := d.series[metricNames[0]]
		memory := make([]*float64, out.ExpectedSamples)
		if d.bad || badUUID[d.info.UUID] {
			d.info.Status = "ambiguous"
			utilization = nil
			d.series = map[string][]*float64{}
		}
		for i := 0; i < out.ExpectedSamples; i++ {
			get := func(m string) *float64 {
				a := d.series[m]
				if len(a) == 0 {
					return nil
				}
				return a[i]
			}
			u, t := get(metricNames[1]), get(metricNames[3])
			if !d.seen[metricNames[3]] {
				if f := get(metricNames[2]); u != nil && f != nil {
					v := *u + *f
					t = &v
				}
			}
			if u != nil && t != nil && *t > 0 && *u <= *t {
				v := 100 * *u / *t
				memory[i] = &v
			}
		}
		d.info.Utilization = statistics(utilization, out.ExpectedSamples)
		d.info.MemoryPercent = statistics(memory, out.ExpectedSamples)
		d.info.PowerW = statistics(d.series[metricNames[5]], out.ExpectedSamples)
		for i, v := range utilization {
			if v != nil {
				sums[i] += *v
				counts[i]++
				total += *v
				out.Summary.ValidSamples++
				if *v < 20 {
					d.info.LowSamples++
				}
			}
		}
		if d.info.Utilization.Samples > 0 {
			v := 100 * float64(d.info.LowSamples) / float64(d.info.Utilization.Samples)
			d.info.LowSamplePercent = &v
		}
		if d.info.Status != "ambiguous" {
			d.info.Status = "insufficient"
			if d.info.Utilization.CoveragePercent >= 80 {
				d.info.Status = "active"
				if d.info.LowSamplePercent != nil && *d.info.LowSamplePercent >= 80 {
					d.info.Status = "low"
				}
			}
		}
		switch d.info.Status {
		case "ambiguous":
			out.Summary.Ambiguous++
		case "insufficient":
			out.Summary.Insufficient++
		case "low":
			out.Summary.Low++
		case "active":
			out.Summary.Active++
		}
		out.Items = append(out.Items, d.info)
	}
	slices.SortFunc(out.Items, func(a, b EfficiencyDevice) int {
		if a.Node < b.Node {
			return -1
		}
		if a.Node > b.Node {
			return 1
		}
		if a.UUID < b.UUID {
			return -1
		}
		if a.UUID > b.UUID {
			return 1
		}
		return 0
	})
	out.Summary.ObservedDevices = len(out.Items)
	if out.Summary.ValidSamples > 0 {
		v := total / float64(out.Summary.ValidSamples)
		out.Summary.MeanUtilization = &v
	}
	for i := range sums {
		v := EfficiencyPoint{Time: out.Start + int64(i+1)*out.Step, Devices: counts[i]}
		if counts[i] > 0 {
			mean := sums[i] / float64(counts[i])
			v.MeanUtilization = &mean
		}
		out.Points = append(out.Points, v)
	}
	if len(out.Items) == 0 {
		out.Status = "no_data"
	}
	return out, nil
}
func (s *Service) Efficiency(ctx context.Context, id, window string) (Efficiency, error) {
	if _, e := efficiencyWindow(window, time.Now()); e != nil {
		return Efficiency{}, e
	}
	b, c, e := s.read(ctx, id, "efficiency", "", "", window)
	if e != nil {
		return Efficiency{}, e
	}
	var out Efficiency
	e = json.Unmarshal(b, &out)
	out.Cached = c
	return out, e
}
