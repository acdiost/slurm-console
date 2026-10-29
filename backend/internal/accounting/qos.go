package accounting

import (
	"context"
	"fmt"
	"slices"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/slurm/adapter"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
)

func isQOS(action string) bool { return action == "qos_create" || action == "qos_update" }
func readPermission(action string) string {
	if client.IsReservation(action) {
		return "reservations.read"
	}
	if isTRES(action) {
		return "tres.read"
	}
	if isQOS(action) {
		return "qos.read"
	}
	if action == "account_create" || action == "account_update" {
		return "accounts.read"
	}
	if action == "user_create" {
		return "slurm_users.read"
	}
	return "associations.read"
}
func readSnapshot(ctx context.Context, a *adapter.Adapter, action string) (adapter.AccountingSnapshot, error) {
	if client.IsReservation(action) {
		return readReservationSnapshot(ctx, a, action)
	}
	if isTRES(action) {
		return readTRESSnapshot(ctx, a, action)
	}
	if !isQOS(action) {
		return a.Accounting(ctx)
	}
	v := adapter.AccountingSnapshot{}
	var err error
	v.Cluster, err = a.AccountingCluster(ctx)
	if err != nil {
		return v, err
	}
	v.QOS, err = a.AllQOS(ctx)
	return v, err
}
func findQOS(s adapter.AccountingSnapshot, name string) *models.QOS {
	for _, v := range s.QOS {
		if v.Name == name {
			return &v
		}
	}
	return nil
}
func preserveQOS(s adapter.AccountingSnapshot, m client.AccountingMutation) client.AccountingMutation {
	if !isQOS(m.Action) || m.QOS == nil {
		return m
	}
	if m.Action == "qos_create" {
		m.QOSPreemptModes = []string{"DISABLED"}
		return m
	}
	if q := findQOS(s, m.QOS.Name); q != nil {
		m.QOSPreemptModes = slices.Clone(q.PreemptModes)
		m.QOSUnmanagedFingerprint = q.UnmanagedFingerprint
	}
	return m
}
func qosPolicy(s adapter.AccountingSnapshot, m client.AccountingMutation) string {
	if !m.Valid() || s.Cluster != m.Cluster {
		return "QOS 字段或集群不符合操作策略。"
	}
	q := findQOS(s, m.QOS.Name)
	if m.Action == "qos_create" {
		if q != nil {
			return "QOS 名称已存在于共享记账库，不会覆盖现有策略。"
		}
		return ""
	}
	if q == nil || q.ID != m.QOS.ID {
		return "QOS 已不存在或 ID 已变化。"
	}
	if len(m.QOSPreemptModes) == 0 || !slices.Equal(m.QOSPreemptModes, q.PreemptModes) || m.QOSUnmanagedFingerprint != q.UnmanagedFingerprint {
		return "未编辑字段已变化或未明确返回抢占模式，请重新读取 QOS。"
	}
	if qosReached(s, m) {
		return "QOS 可编辑字段均未变化。"
	}
	return ""
}
func qosReached(s adapter.AccountingSnapshot, m client.AccountingMutation) bool {
	if s.Cluster != m.Cluster || m.QOS == nil {
		return false
	}
	q := findQOS(s, m.QOS.Name)
	v := m.QOS
	if q == nil || v.ID != "" && q.ID != v.ID || q.Description != v.Description || q.Priority.Value == nil || *q.Priority.Value != int64(v.Priority) || q.Priority.Infinite || q.UsageFactor.Value == nil || *q.UsageFactor.Value != v.UsageFactor || q.UsageFactor.Infinite {
		return false
	}
	if !slices.Equal(q.PreemptModes, m.QOSPreemptModes) || m.QOSUnmanagedFingerprint != "" && q.UnmanagedFingerprint != m.QOSUnmanagedFingerprint {
		return false
	}
	limit := func(n models.Number, want *uint32) bool {
		if want == nil {
			return n.Infinite
		}
		return !n.Infinite && n.Value != nil && *n.Value == int64(*want)
	}
	return limit(q.MaxJobsPerUser, v.MaxJobsPerUser) && limit(q.MaxSubmitJobsPerUser, v.MaxSubmitJobsPerUser) && limit(q.MaxWallMinutes, v.MaxWallMinutes)
}
func qosChanges(s adapter.AccountingSnapshot, m client.AccountingMutation) []audit.Change {
	v := m.QOS
	before := findQOS(s, v.Name)
	old := map[string]any{}
	if before != nil {
		old = map[string]any{"description": before.Description, "qosPriority": before.Priority, "usageFactor": before.UsageFactor, "maxJobsPerUser": before.MaxJobsPerUser, "maxSubmitJobsPerUser": before.MaxSubmitJobsPerUser, "maxWallMinutes": before.MaxWallMinutes}
	}
	out := []audit.Change{{Field: "qosName", After: v.Name}, {Field: "scope", After: "共享 SlurmDBD；可能影响多个集群"}}
	after := map[string]any{"description": v.Description, "qosPriority": v.Priority, "usageFactor": v.UsageFactor, "maxJobsPerUser": v.MaxJobsPerUser, "maxSubmitJobsPerUser": v.MaxSubmitJobsPerUser, "maxWallMinutes": v.MaxWallMinutes}
	for _, field := range []string{"description", "qosPriority", "usageFactor", "maxJobsPerUser", "maxSubmitJobsPerUser", "maxWallMinutes"} {
		out = append(out, audit.Change{Field: field, Before: qosAuditValue(old[field]), After: qosAuditValue(after[field])})
	}
	return out
}
func qosAuditValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case *uint32:
		if v == nil {
			return "无限制"
		}
		return strconv.FormatUint(uint64(*v), 10)
	case models.Number:
		if v.Infinite {
			return "无限制"
		}
		if v.Value == nil {
			return "未提供"
		}
		return strconv.FormatInt(*v.Value, 10)
	case models.Decimal:
		if v.Infinite {
			return "无限"
		}
		if v.Value == nil {
			return "未提供"
		}
		return strconv.FormatFloat(*v.Value, 'g', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
