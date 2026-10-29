package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
	"strings"
)

// ExpandReservationNodes expands bounded Slurm numeric host ranges, never executes shell expressions.
func ExpandReservationNodes(text string) ([]string, bool) {
	out := []string{}
	seen := map[string]bool{}
	var expand func(string) bool
	expand = func(s string) bool {
		if len(out) >= 20000 {
			return false
		}
		i := strings.IndexByte(s, '[')
		if i < 0 {
			if !client.ValidNodeTarget(s) || seen[s] {
				return false
			}
			seen[s] = true
			out = append(out, s)
			return true
		}
		j := strings.IndexByte(s[i:], ']')
		if j < 0 {
			return false
		}
		j += i
		for _, part := range strings.Split(s[i+1:j], ",") {
			a, b, rangePart := strings.Cut(part, "-")
			n, e := strconv.Atoi(a)
			if e != nil || n < 0 || len(a) > 8 {
				return false
			}
			last := n
			if rangePart {
				last, e = strconv.Atoi(b)
				if e != nil || last < n || last-n > 20000 || len(b) > 8 {
					return false
				}
			}
			for v := n; v <= last; v++ {
				t := strconv.Itoa(v)
				if len(a) > 1 && a[0] == '0' {
					t = strings.Repeat("0", max(0, len(a)-len(t))) + t
				}
				if !expand(s[:i] + t + s[j+1:]) {
					return false
				}
			}
		}
		return true
	}
	if text == "" {
		return out, true
	}
	depth, start := 0, 0
	for i, c := range text {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				if !expand(text[start:i]) {
					return nil, false
				}
				start = i + 1
			}
		}
		if depth < 0 || depth > 1 {
			return nil, false
		}
	}
	if depth != 0 || !expand(text[start:]) {
		return nil, false
	}
	slices.Sort(out)
	return out, true
}
func ReservationDigest(raw json.RawMessage, omitEnd bool) string {
	var v map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return ""
	}
	if omitEnd {
		delete(v, "end_time")
	}
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (a *Adapter) GetReservations(ctx context.Context) ([]models.Reservation, error) {
	env, err := a.reader.Read(ctx, client.Reservations, "")
	if err != nil {
		return nil, err
	}
	raw, err := decode[json.RawMessage](env.Reservations)
	if err != nil {
		return nil, err
	}
	out := []models.Reservation{}
	seen := map[string]bool{}
	for _, r := range raw {
		var v struct {
			Name, Accounts, Users, Groups, Partition, Features, Licenses string
			BurstBuffer                                                  string `json:"burst_buffer"`
			Nodes                                                        string `json:"node_list"`
			NodeCount                                                    int64  `json:"node_count"`
			CoreCount                                                    int64  `json:"core_count"`
			Start                                                        struct {
				Set, Infinite bool
				Number        int64
			} `json:"start_time"`
			End struct {
				Set, Infinite bool
				Number        int64
			} `json:"end_time"`
			TRES  string   `json:"tres"`
			Flags []string `json:"flags"`
		}
		if json.Unmarshal(r, &v) != nil || !client.ValidAccountingName(v.Name) || seen[v.Name] || len(v.Nodes) > 65536 || len(v.Users)+len(v.Accounts)+len(v.Groups) > 65536 || v.NodeCount < 0 || v.CoreCount < 0 {
			return nil, invalid()
		}
		seen[v.Name] = true
		stamp := func(set, infinite bool, n int64) *int64 {
			if !set || infinite || n <= 0 || n > 4102444800 {
				return nil
			}
			return &n
		}
		names, ok := ExpandReservationNodes(v.Nodes)
		if names == nil {
			names = []string{}
		}
		if v.Flags == nil {
			v.Flags = []string{}
		}
		editable := ok && len(names) > 0 && v.Groups == "" && v.Partition == "" && v.Features == "" && v.Licenses == "" && v.BurstBuffer == ""
		for _, flag := range v.Flags {
			if !slices.Contains([]string{"MAINT", "SPEC_NODES"}, flag) {
				editable = false
			}
		}
		out = append(out, models.Reservation{Name: v.Name, StartTime: stamp(v.Start.Set, v.Start.Infinite, v.Start.Number), EndTime: stamp(v.End.Set, v.End.Infinite, v.End.Number), Nodes: v.Nodes, NodeNames: names, NodeCount: v.NodeCount, CoreCount: v.CoreCount, Users: v.Users, Accounts: v.Accounts, Groups: v.Groups, Partition: v.Partition, Features: v.Features, Licenses: v.Licenses, BurstBuffer: v.BurstBuffer, TRES: v.TRES, Flags: v.Flags, Editable: editable, Raw: r})
	}
	return out, nil
}
