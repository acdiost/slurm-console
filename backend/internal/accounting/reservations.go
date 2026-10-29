package accounting

import (
	"context"
	"slices"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/services"
	"slurm-console/backend/internal/slurm/adapter"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
	"strings"
	"time"
)

func readReservationSnapshot(ctx context.Context, a *adapter.Adapter, action string) (adapter.AccountingSnapshot, error) {
	var s adapter.AccountingSnapshot
	var err error
	s.Cluster, err = a.AccountingCluster(ctx)
	if err != nil {
		return s, err
	}
	s.Reservations, err = a.GetReservations(ctx)
	if err != nil {
		return s, err
	}
	if action == "reservation_create" {
		nodes, e := a.GetNodes(ctx)
		if e != nil {
			return s, e
		}
		for _, n := range nodes {
			s.AvailableNodes = append(s.AvailableNodes, n.Name)
		}
		slices.Sort(s.AvailableNodes)
	}
	return s, nil
}
func findReservation(s adapter.AccountingSnapshot, m client.AccountingMutation) *models.Reservation {
	if m.Reservation == nil {
		return nil
	}
	for _, r := range s.Reservations {
		if r.Name == m.Reservation.Name {
			return &r
		}
	}
	return nil
}
func reservationState(s adapter.AccountingSnapshot, m client.AccountingMutation) string {
	r := findReservation(s, m)
	hash := ""
	if r != nil {
		hash = adapter.ReservationDigest(r.Raw, false)
	}
	return digest([]any{s.Cluster, hash, s.AvailableNodes})
}
func reservationPolicy(s adapter.AccountingSnapshot, m client.AccountingMutation) string {
	if !m.Valid() || s.Cluster != m.Cluster {
		return "预留参数或集群不合法。"
	}
	v := m.Reservation
	r := findReservation(s, m)
	now := time.Now().Unix()
	if m.Action == "reservation_create" {
		if r != nil {
			return "名称已存在，请读取既有预留。"
		}
		if v.StartTime < now+60 || v.StartTime > now+365*86400 {
			return "开始时间需在一分钟后、一年内。"
		}
		for _, n := range v.Nodes {
			if !slices.Contains(s.AvailableNodes, n) {
				return "节点不存在于当前集群。"
			}
		}
		return ""
	}
	if r == nil {
		return "预留不存在或已过期，请重新读取。"
	}
	if !r.Editable || r.StartTime == nil || r.EndTime == nil {
		return "此预留包含高级或未知配置，本项仅支持查询。"
	}
	if m.Action == "reservation_delete" {
		return ""
	}
	if *r.StartTime <= now+60 {
		return "本项仅调整尚未开始、距离开始超过一分钟的预留。"
	}
	if v.EndTime <= *r.StartTime || v.EndTime-*r.StartTime > 30*86400 {
		return "结束时间需晚于开始时间，窗口不超过三十天。"
	}
	if v.EndTime == *r.EndTime {
		return "结束时间未变化。"
	}
	if m.ReservationUnmanagedFingerprint == "" || adapter.ReservationDigest(r.Raw, true) != m.ReservationUnmanagedFingerprint {
		return "未编辑配置变化，请重新校验。"
	}
	return ""
}
func checkReservationJobs(ctx context.Context, c services.Connection, m client.AccountingMutation) error {
	if m.Action == "reservation_create" {
		return nil
	}
	jobs, err := c.Adapter.GetJobs(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		terminal := len(j.States) == 1 && slices.Contains([]string{"COMPLETED", "CANCELLED", "FAILED", "TIMEOUT", "NODE_FAIL", "OUT_OF_MEMORY", "PREEMPTED", "BOOT_FAIL", "DEADLINE", "REVOKED"}, j.States[0])
		if terminal {
			continue
		}
		if !j.ReservationKnown {
			return client.Failure("operation_forbidden", "无法确认未结束作业的预留归属，未开放变更。")
		}
		if j.Reservation == m.Reservation.Name {
			return client.Failure("operation_forbidden", "此预留仍有未结束作业，不能调整或删除。")
		}
	}
	return nil
}
func reservationReached(s adapter.AccountingSnapshot, m client.AccountingMutation) bool {
	if s.Cluster != m.Cluster {
		return false
	}
	r := findReservation(s, m)
	v := m.Reservation
	if m.Action == "reservation_delete" {
		return r == nil
	}
	if r == nil || r.EndTime == nil || *r.EndTime != v.EndTime {
		return false
	}
	if m.Action == "reservation_update" {
		return m.ReservationUnmanagedFingerprint != "" && m.ReservationUnmanagedFingerprint == adapter.ReservationDigest(r.Raw, true)
	}
	same := func(a, b []string) bool {
		a = slices.Clone(a)
		b = slices.Clone(b)
		slices.Sort(a)
		slices.Sort(b)
		return slices.Equal(a, b)
	}
	split := func(s string) []string {
		if s == "" {
			return []string{}
		}
		return strings.Split(s, ",")
	}
	return r.Editable && r.StartTime != nil && *r.StartTime == v.StartTime && same(r.NodeNames, v.Nodes) && same(split(r.Users), v.Users) && same(split(r.Accounts), v.Accounts) && slices.Contains(r.Flags, "MAINT") == v.Maintenance
}
func reservationChanges(s adapter.AccountingSnapshot, m client.AccountingMutation) []audit.Change {
	v := m.Reservation
	r := findReservation(s, m)
	before := map[string]string{}
	after := map[string]string{}
	if r != nil {
		before["reservationNodes"] = r.Nodes
		before["reservationUsers"] = r.Users
		before["reservationAccounts"] = r.Accounts
		before["reservationMaintenance"] = strconv.FormatBool(slices.Contains(r.Flags, "MAINT"))
		if r.StartTime != nil {
			before["reservationStart"] = strconv.FormatInt(*r.StartTime, 10)
		}
		if r.EndTime != nil {
			before["reservationEnd"] = strconv.FormatInt(*r.EndTime, 10)
		}
	}
	if m.Action == "reservation_create" {
		after = map[string]string{"reservationStart": strconv.FormatInt(v.StartTime, 10), "reservationEnd": strconv.FormatInt(v.EndTime, 10), "reservationNodes": strings.Join(v.Nodes, ","), "reservationUsers": strings.Join(v.Users, ","), "reservationAccounts": strings.Join(v.Accounts, ","), "reservationMaintenance": strconv.FormatBool(v.Maintenance)}
	}
	if m.Action == "reservation_update" {
		for k, x := range before {
			after[k] = x
		}
		after["reservationEnd"] = strconv.FormatInt(v.EndTime, 10)
	}
	result := []audit.Change{{Field: "reservationName", After: v.Name}}
	for _, field := range []string{"reservationStart", "reservationEnd", "reservationNodes", "reservationUsers", "reservationAccounts", "reservationMaintenance"} {
		result = append(result, audit.Change{Field: field, Before: before[field], After: after[field]})
	}
	return result
}

func (s *Service) ExecuteReservation(actor, cluster, requestID string, input Input) (Result, error) {
	p, err := s.parse(input.Proof, actor, cluster)
	if err != nil {
		return Result{}, err
	}
	if !client.IsReservation(p.Action) {
		return Result{}, client.Failure("invalid_request", "预留确认类型不匹配。")
	}
	return s.Execute(actor, cluster, requestID, input)
}
