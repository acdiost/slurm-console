package gpumetrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"slurm-console/backend/internal/slurm/client"
	"strconv"
	"strings"
	"time"
)

var metricNames = []string{"DCGM_FI_DEV_GPU_UTIL", "DCGM_FI_DEV_FB_USED", "DCGM_FI_DEV_FB_FREE", "DCGM_FI_DEV_FB_TOTAL", "DCGM_FI_DEV_GPU_TEMP", "DCGM_FI_DEV_POWER_USAGE"}
var fields = map[string]string{"DCGM_FI_DEV_GPU_UTIL": "utilization", "DCGM_FI_DEV_FB_USED": "memoryUsedMiB", "DCGM_FI_DEV_FB_FREE": "memoryFreeMiB", "DCGM_FI_DEV_FB_TOTAL": "memoryTotalMiB", "DCGM_FI_DEV_GPU_TEMP": "temperatureC", "DCGM_FI_DEV_POWER_USAGE": "powerW"}

type row struct {
	Metric map[string]string   `json:"metric"`
	Values [][]json.RawMessage `json:"values"`
}
type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []row  `json:"result"`
	} `json:"data"`
	Warnings []string `json:"warnings"`
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }
func selector(p Profile, host, uuid string) string {
	s := `{__name__=~` + quote(strings.Join(metricNames, "|")) + `,` + p.ClusterLabel + `=` + quote(p.ClusterValue)
	if host != "" {
		s += `,` + p.NodeLabel + `=` + quote(host) + `,UUID=` + quote(uuid)
	}
	return s + `}`
}
func query(ctx context.Context, p Profile, kind string, params url.Values) ([]row, error) {
	transport := &http.Transport{TLSClientConfig: p.tls, MaxResponseHeaderBytes: 16384, ResponseHeaderTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	h := http.Client{Transport: transport, Timeout: 7 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect blocked") }}
	req, e := http.NewRequestWithContext(ctx, "GET", p.Endpoint+"/api/v1/"+kind+"?"+params.Encode(), nil)
	if e != nil {
		return nil, e
	}
	if p.TokenFile != "" {
		b, e := restricted(p.TokenFile, 8192, true)
		if e != nil || !validToken(b) {
			return nil, errors.New("metrics credentials unavailable")
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	}
	res, e := h.Do(req)
	if e != nil {
		return nil, errors.New("metrics upstream unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("metrics upstream rejected query")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if e != nil || len(raw) > 4<<20 {
		return nil, errors.New("metrics response exceeds limit")
	}
	var out promResponse
	if json.Unmarshal(raw, &out) != nil || out.Status != "success" || out.Data.ResultType != "matrix" || len(out.Data.Result) > 4096 || len(out.Warnings) > 0 {
		return nil, errors.New("invalid or incomplete metrics response")
	}
	count := 0
	for _, r := range out.Data.Result {
		count += len(r.Values)
		if len(r.Metric) > 64 || len(r.Values) > 2000 || count > 120000 {
			return nil, errors.New("metrics series exceeds limit")
		}
		for k, v := range r.Metric {
			if len(k) > 128 || len(v) > 512 {
				return nil, errors.New("metrics label exceeds limit")
			}
		}
	}
	return out.Data.Result, nil
}
func identity(p Profile, r row) (string, string, string, bool, error) {
	if r.Metric[p.ClusterLabel] != p.ClusterValue {
		return "", "", "", false, errors.New("metrics scope mismatch")
	}
	metric := r.Metric["__name__"]
	if !slices.Contains(metricNames, metric) {
		return "", "", "", false, errors.New("unexpected metrics family")
	}
	uuid, host := r.Metric["UUID"], r.Metric[p.NodeLabel]
	if r.Metric["GPU_I_ID"] != "" || r.Metric["GPU_CI_ID"] != "" || strings.HasPrefix(uuid, "MIG-") {
		return "", "", "", true, nil
	}
	if !uuidPattern.MatchString(uuid) || host == "" {
		return "", "", "", false, errors.New("missing physical GPU identity")
	}
	node := host
	if len(p.NodeMap) > 0 {
		var ok bool
		node, ok = p.NodeMap[host]
		if !ok {
			return "", "", "", true, nil
		}
	}
	if !client.ValidNodeName(node) {
		return "", "", "", false, errors.New("invalid GPU node identity")
	}
	return node, uuid, metric, false, nil
}
func parsePoint(v []json.RawMessage, metric string, p Profile) (int64, *float64) {
	if len(v) != 2 {
		return 0, nil
	}
	var ts float64
	var text string
	if json.Unmarshal(v[0], &ts) != nil || math.IsNaN(ts) || math.IsInf(ts, 0) || ts <= 0 || ts > 4102444800 || json.Unmarshal(v[1], &text) != nil {
		return 0, nil
	}
	n, e := strconv.ParseFloat(text, 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return int64(ts), nil
	}
	maxValue := float64(1 << 30)
	minValue := 0.0
	switch metric {
	case "DCGM_FI_DEV_GPU_UTIL":
		maxValue = 100
		if p.UtilizationUnit == "ratio" {
			maxValue = 1
		}
	case "DCGM_FI_DEV_GPU_TEMP":
		minValue = -100
		maxValue = 200
	case "DCGM_FI_DEV_POWER_USAGE":
		maxValue = 5000
	}
	if n < minValue || n > maxValue {
		return int64(ts), nil
	}
	if metric == "DCGM_FI_DEV_GPU_UTIL" && p.UtilizationUnit == "ratio" {
		n *= 100
	}
	return int64(ts), &n
}
func sample(v []json.RawMessage, metric string, p Profile, now int64) Sample {
	ts, n := parsePoint(v, metric, p)
	s := Sample{Status: "invalid"}
	if ts == 0 {
		return s
	}
	s.Timestamp = &ts
	if ts > now+10 {
		return s
	}
	if now-ts > 120 {
		s.Status = "stale"
		return s
	}
	if n == nil {
		return s
	}
	s.Value = n
	s.Status = "fresh"
	return s
}
func missing() Sample { return Sample{Status: "missing"} }
func LoadSnapshot(ctx context.Context, p Profile, now time.Time) (Snapshot, error) {
	result := Snapshot{Status: "ok", Source: p.Label, FetchedAt: now.UTC().Format(time.RFC3339Nano), Items: []Device{}}
	rows, e := query(ctx, p, "query", url.Values{"query": {selector(p, "", "") + "[10m]"}, "time": {strconv.FormatInt(now.Unix(), 10)}})
	if e != nil {
		return result, e
	}
	byKey := map[string]*Device{}
	seen := map[string]bool{}
	uuidNode := map[string]string{}
	badUUID := map[string]bool{}
	for _, r := range rows {
		node, uuid, m, skip, e := identity(p, r)
		if e != nil {
			return result, e
		}
		if skip {
			result.IgnoredSeries++
			continue
		}
		key := node + "/" + uuid
		d := byKey[key]
		if d == nil {
			if len(byKey) >= 512 {
				return result, errors.New("too many GPUs")
			}
			d = &Device{UUID: uuid, Node: node, Index: r.Metric["gpu"], Model: r.Metric["modelName"], Utilization: missing(), MemoryUsed: missing(), MemoryFree: missing(), MemoryTotal: missing(), Temperature: missing(), Power: missing(), TotalSource: "unknown"}
			byKey[key] = d
		}
		if prev, ok := uuidNode[uuid]; ok && prev != node {
			badUUID[uuid] = true
		}
		uuidNode[uuid] = node
		s := missing()
		last := int64(0)
		for _, v := range r.Values {
			ts, _ := parsePoint(v, m, p)
			if ts <= last {
				return result, errors.New("unordered GPU samples")
			}
			last = ts
			s = sample(v, m, p, now.Unix())
		}
		if seen[key+"/"+m] {
			s = Sample{Status: "ambiguous"}
		}
		seen[key+"/"+m] = true
		if d.Index != r.Metric["gpu"] || d.Model != r.Metric["modelName"] {
			badUUID[uuid] = true
		}
		switch m {
		case metricNames[0]:
			d.Utilization = s
		case metricNames[1]:
			d.MemoryUsed = s
		case metricNames[2]:
			d.MemoryFree = s
		case metricNames[3]:
			d.MemoryTotal = s
			d.TotalSource = "reported"
		case metricNames[4]:
			d.Temperature = s
		case metricNames[5]:
			d.Power = s
		}
	}
	for _, d := range byKey {
		if badUUID[d.UUID] {
			d.Utilization = Sample{Status: "ambiguous"}
			d.MemoryUsed = d.Utilization
			d.MemoryFree = d.Utilization
			d.MemoryTotal = d.Utilization
			d.Temperature = d.Utilization
			d.Power = d.Utilization
		}
		if d.MemoryTotal.Status == "missing" && d.MemoryUsed.Status == "fresh" && d.MemoryFree.Status == "fresh" && abs(*d.MemoryUsed.Timestamp-*d.MemoryFree.Timestamp) <= 60 {
			n := *d.MemoryUsed.Value + *d.MemoryFree.Value
			ts := min(*d.MemoryUsed.Timestamp, *d.MemoryFree.Timestamp)
			d.MemoryTotal = Sample{Value: &n, Timestamp: &ts, Status: "fresh"}
			d.TotalSource = "used_plus_free"
		}
		if d.MemoryUsed.Value != nil && d.MemoryTotal.Value != nil && *d.MemoryUsed.Value > *d.MemoryTotal.Value {
			d.MemoryTotal = Sample{Status: "invalid"}
			d.TotalSource = "unknown"
		}
		d.Status = "fresh"
		valid := 0
		ambiguous := false
		stale := false
		for _, s := range []Sample{d.Utilization, d.MemoryUsed, d.MemoryTotal, d.Temperature, d.Power} {
			if s.Status == "fresh" {
				valid++
			}
			if s.Status == "ambiguous" {
				ambiguous = true
			}
			if s.Status == "stale" {
				stale = true
			}
		}
		switch {
		case ambiguous:
			d.Status = "ambiguous"
		case valid == 0 && stale:
			d.Status = "stale"
		case valid < 5:
			d.Status = "partial"
		}
		result.Items = append(result.Items, *d)
	}
	slices.SortFunc(result.Items, func(a, b Device) int { return strings.Compare(a.Node+"/"+a.UUID, b.Node+"/"+b.UUID) })
	if len(result.Items) == 0 {
		result.Status = "no_data"
		result.Message = "此来源未返回可识别的物理 GPU 数据。"
	}
	return result, nil
}
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
func LoadHistory(ctx context.Context, p Profile, node, uuid, window string, now time.Time) (History, error) {
	seconds, step := int64(3600), int64(60)
	switch window {
	case "1h":
	case "6h":
		seconds = 21600
		step = 300
	case "24h":
		seconds = 86400
		step = 600
	default:
		return History{}, errors.New("invalid GPU history range")
	}
	host := node
	if len(p.NodeMap) > 0 {
		host = ""
		for h, n := range p.NodeMap {
			if n == node {
				host = h
			}
		}
		if host == "" {
			return History{}, errors.New("unknown metrics node")
		}
	}
	end := now.Unix() / step * step
	start := end - seconds
	result := History{UUID: uuid, Node: node, Range: window, Start: start, End: end, Step: step, FetchedAt: now.UTC().Format(time.RFC3339Nano), Series: map[string][]Point{}}
	rows, e := query(ctx, p, "query_range", url.Values{"query": {"last_over_time(" + selector(p, host, uuid) + "[60s])"}, "start": {strconv.FormatInt(start, 10)}, "end": {strconv.FormatInt(end, 10)}, "step": {strconv.FormatInt(step, 10)}})
	if e != nil {
		return result, e
	}
	seen := map[string]bool{}
	for _, m := range metricNames {
		points := []Point{}
		for t := start; t <= end; t += step {
			points = append(points, Point{Time: t})
		}
		result.Series[fields[m]] = points
	}
	for _, r := range rows {
		n, u, m, skip, e := identity(p, r)
		if e != nil || skip || n != node || u != uuid {
			return result, errors.New("GPU history identity mismatch")
		}
		key := fields[m]
		if seen[key] {
			for i := range result.Series[key] {
				result.Series[key][i].Value = nil
			}
			continue
		}
		seen[key] = true
		last := int64(0)
		for _, v := range r.Values {
			ts, value := parsePoint(v, m, p)
			if ts < start || ts > end || (ts-start)%step != 0 || ts <= last {
				return result, fmt.Errorf("GPU history timestamp invalid")
			}
			last = ts
			result.Series[key][(ts-start)/step].Value = value
		}
	}
	if !seen["memoryTotalMiB"] {
		for i := range result.Series["memoryTotalMiB"] {
			u, f := result.Series["memoryUsedMiB"][i].Value, result.Series["memoryFreeMiB"][i].Value
			if u != nil && f != nil {
				n := *u + *f
				result.Series["memoryTotalMiB"][i].Value = &n
			}
		}
	}
	for i := range result.Series["memoryTotalMiB"] {
		u, total := result.Series["memoryUsedMiB"][i].Value, result.Series["memoryTotalMiB"][i].Value
		if u != nil && total != nil && *u > *total {
			result.Series["memoryTotalMiB"][i].Value = nil
		}
	}
	return result, nil
}
