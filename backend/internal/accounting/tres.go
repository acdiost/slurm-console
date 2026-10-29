package accounting

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"slurm-console/backend/internal/audit"
	"slurm-console/backend/internal/rbac"
	"slurm-console/backend/internal/slurm/adapter"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
	"strings"
)

func isTRES(action string) bool {
	return action == "qos_tres_update" || action == "association_tres_update"
}
func tresKind(action string) string {
	if action == "qos_tres_update" {
		return "qos"
	}
	return "association"
}
func readTRESSnapshot(ctx context.Context, a *adapter.Adapter, action string) (adapter.AccountingSnapshot, error) {
	v, err := a.TRESLimits(ctx, tresKind(action))
	return adapter.AccountingSnapshot{Cluster: v.Cluster, TRES: &v}, err
}
func findTRESTarget(s adapter.AccountingSnapshot, m client.AccountingMutation) *models.TRESTarget {
	if s.TRES == nil || m.TRES == nil {
		return nil
	}
	for _, v := range s.TRES.Targets {
		if v.Kind == m.TRES.Kind && v.ID == m.TRES.TargetID {
			return &v
		}
	}
	return nil
}
func tresAllowed(a rbac.Access, cluster, action string) bool {
	if !a.Allows("tres.read", cluster) {
		return false
	}
	if action == "qos_tres_update" {
		return a.Allows("qos.read", cluster) && a.Allows("platform.qos.read", "")
	}
	return a.Allows("associations.read", cluster)
}
func materializeTRES(s adapter.AccountingSnapshot, m client.AccountingMutation) client.AccountingMutation {
	t := findTRESTarget(s, m)
	if t == nil {
		return m
	}
	spec := m.TRES
	m.TRESUnmanagedFingerprint = adapter.TRESDigest(t.Raw, t.Kind, spec.Bucket, spec.ResourceID)
	if t.Kind == "qos" {
		var raw struct{ Preempt struct{ Mode []string } }
		_ = json.Unmarshal(t.Raw, &raw)
		m.QOSPreemptModes = raw.Preempt.Mode
	}
	byID := map[string]models.TRES{}
	for _, r := range s.TRES.Resources {
		byID[r.ID] = r
	}
	entries := []client.TRESWireEntry{}
	appendEntry := func(id, value string) bool {
		r, ok := byID[id]
		if !ok {
			return false
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false
		}
		rid, _ := strconv.ParseUint(id, 10, 32)
		entries = append(entries, client.TRESWireEntry{ID: uint32(rid), Type: r.Type, Name: r.Name, Count: n})
		return true
	}
	if t.Kind == "association" {
		m.TRESLists = map[string][]client.TRESWireEntry{}
		for _, bucket := range client.TRESBuckets(t.Kind) {
			entries = []client.TRESWireEntry{}
			for _, v := range t.Limits[bucket] {
				if bucket == spec.Bucket && v.ID == spec.ResourceID {
					continue
				}
				value := v.Value
				// Clearing a different sentinel entry would change its raw policy.
				if v.State != "finite" || !appendEntry(v.ID, value) {
					return m
				}
			}
			m.TRESLists[bucket] = entries
		}
		entries = m.TRESLists[spec.Bucket]
	}
	value := spec.Value
	if spec.Mode == "clear" {
		value = "-1"
	}
	if !appendEntry(spec.ResourceID, value) {
		return m
	}
	slices.SortFunc(entries, func(a, b client.TRESWireEntry) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	if t.Kind == "association" {
		m.TRESLists[spec.Bucket] = entries
	}
	m.TRESEntries = entries
	return m
}
func tresPolicy(s adapter.AccountingSnapshot, m client.AccountingMutation) string {
	if !m.Valid() || s.Cluster != m.Cluster || s.TRES == nil {
		return "TRES 操作字段或目标集群不合法。"
	}
	t := findTRESTarget(s, m)
	v := m.TRES
	if t == nil {
		return "限额对象不存在于当前集群或已删除。"
	}
	if t.Kind == "qos" && t.Name != v.Name || t.Kind == "association" && (t.Account != v.Account || t.User != v.User || t.Partition != v.Partition) {
		return "对象 ID 与完整身份不一致，请重新读取。"
	}
	if t.Kind == "association" && (protected(t.Account) || t.User != "" && protected(t.User)) {
		return "root 层级和系统服务关联受保护。"
	}
	resource := models.TRES{}
	for _, r := range s.TRES.Resources {
		if r.ID == v.ResourceID {
			resource = r
		}
	}
	if !resource.Editable {
		return "此资源未在目录中注册，或仅支持查询。"
	}
	if t.Relative && v.Mode == "set" {
		n, _ := strconv.ParseInt(v.Value, 10, 64)
		if strings.Contains(v.Bucket, "minutes") {
			return "Relative QOS 的时间额度仅支持查询与清除，请由管理员维护。"
		}
		if n > 100 {
			return "Relative QOS 的资源限额使用 0–100 的整数百分比。"
		}
	}
	if len(m.TRESEntries) == 0 || t.Kind == "association" && len(m.TRESLists) != len(client.TRESBuckets("association")) || t.Kind == "qos" && len(m.QOSPreemptModes) == 0 {
		return "无法完整保留原资源列表或抢占模式，未开放本次写入。"
	}
	if tresReached(s, m) {
		return "此资源的本层配置未变化。"
	}
	if m.TRESUnmanagedFingerprint == "" || m.TRESUnmanagedFingerprint != adapter.TRESDigest(t.Raw, t.Kind, v.Bucket, v.ResourceID) {
		return "未编辑字段已变化，请重新校验。"
	}
	raw, _ := json.Marshal(m)
	if len(raw) > 7000 {
		return "此关联保留列表超过单次安全上限，请管理员维护。"
	}
	return ""
}
func tresReached(s adapter.AccountingSnapshot, m client.AccountingMutation) bool {
	t := findTRESTarget(s, m)
	if t == nil || s.Cluster != m.Cluster {
		return false
	}
	if m.TRESUnmanagedFingerprint != "" && m.TRESUnmanagedFingerprint != adapter.TRESDigest(t.Raw, t.Kind, m.TRES.Bucket, m.TRES.ResourceID) {
		return false
	}
	for _, v := range t.Limits[m.TRES.Bucket] {
		if v.ID == m.TRES.ResourceID {
			return m.TRES.Mode == "set" && v.State == "finite" && v.Value == m.TRES.Value
		}
	}
	return m.TRES.Mode == "clear"
}
func tresChanges(s adapter.AccountingSnapshot, m client.AccountingMutation) []audit.Change {
	v := m.TRES
	before := "未设置（依 Slurm 规则继承）"
	if t := findTRESTarget(s, m); t != nil {
		for _, entry := range t.Limits[v.Bucket] {
			if entry.ID == v.ResourceID {
				before = entry.Value
				if entry.State != "finite" {
					before = entry.State
				}
			}
		}
	}
	after := v.Value
	if v.Mode == "clear" {
		after = "清除本层配置"
	}
	resource := v.ResourceID
	for _, r := range s.TRES.Resources {
		if r.ID == v.ResourceID {
			resource = r.Key
		}
	}
	scope := "当前集群关联及其适用的下级关联"
	if v.Kind == "qos" {
		scope = "共享 SlurmDBD QOS；可能影响多个集群"
	}
	return []audit.Change{{Field: "quotaTargetId", After: v.TargetID}, {Field: "scope", After: scope}, {Field: "tresResource", After: resource}, {Field: "tresBucket", After: v.Bucket}, {Field: "tresLimit", Before: before, After: after}, {Field: "tresIdentity", After: fmt.Sprintf("%s/%s/%s/%s", v.Name, v.Account, v.User, v.Partition)}}
}
