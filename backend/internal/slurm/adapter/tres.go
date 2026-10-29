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
)

func (a *Adapter) TRESCatalog(ctx context.Context) ([]models.TRES, error) {
	env, err := a.reader.Read(ctx, client.DBTRES, "")
	if err != nil {
		return nil, err
	}
	rows, err := decode[struct {
		ID         *uint32
		Type, Name string
	}](env.TRES)
	if err != nil {
		return nil, err
	}
	out := []models.TRES{}
	seen := map[uint32]bool{}
	keys := map[string]bool{}
	for _, v := range rows {
		key := v.Type
		if v.Name != "" {
			key += "/" + v.Name
		}
		if v.ID == nil || *v.ID == 0 || seen[*v.ID] || keys[key] || !client.ValidTRESIdentity(v.Type, v.Name) {
			return nil, invalid()
		}
		seen[*v.ID] = true
		keys[key] = true
		unit := "个"
		switch v.Type {
		case "mem", "vmem":
			unit = "MiB"
		case "cpu":
			unit = "CPU"
		case "node":
			unit = "节点"
		case "billing":
			unit = "计费单位"
		}
		out = append(out, models.TRES{ID: strconv.FormatUint(uint64(*v.ID), 10), Type: v.Type, Name: v.Name, Key: key, Unit: unit, Editable: client.EditableTRES(v.Type, v.Name)})
	}
	slices.SortFunc(out, func(a, b models.TRES) int {
		ai, _ := strconv.ParseUint(a.ID, 10, 32)
		bi, _ := strconv.ParseUint(b.ID, 10, 32)
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
		return 0
	})
	return out, nil
}
func tresValue(raw json.RawMessage) (models.TRESAmount, error) {
	var v struct {
		ID    uint32
		Count json.Number
	}
	if json.Unmarshal(raw, &v) != nil || v.ID == 0 || v.Count == "" {
		return models.TRESAmount{}, invalid()
	}
	value, state := v.Count.String(), "finite"
	switch value {
	case "-1", "18446744073709551615":
		state = "infinite"
		value = ""
	case "-2", "18446744073709551614":
		state = "unset"
		value = ""
	default:
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return models.TRESAmount{}, invalid()
		}
	}
	return models.TRESAmount{ID: strconv.FormatUint(uint64(v.ID), 10), Value: value, State: state}, nil
}
func rawAt(raw json.RawMessage, path []string) json.RawMessage {
	for _, key := range path {
		var next map[string]json.RawMessage
		if json.Unmarshal(raw, &next) != nil {
			return nil
		}
		raw = next[key]
	}
	return raw
}
func projectTRES(target models.TRESTarget) (models.TRESTarget, error) {
	target.Limits = map[string][]models.TRESAmount{}
	for _, bucket := range client.TRESBuckets(target.Kind) {
		raw := rawAt(target.Raw, client.TRESPath(target.Kind, bucket))
		entries := []models.TRESAmount{}
		if len(raw) > 0 && string(raw) != "null" {
			rows, err := decode[json.RawMessage](raw)
			if err != nil || len(rows) > 256 {
				return target, invalid()
			}
			seen := map[string]bool{}
			for _, row := range rows {
				v, err := tresValue(row)
				if err != nil || seen[v.ID] {
					return target, invalid()
				}
				seen[v.ID] = true
				entries = append(entries, v)
			}
		}
		slices.SortFunc(entries, func(a, b models.TRESAmount) int {
			ai, _ := strconv.Atoi(a.ID)
			bi, _ := strconv.Atoi(b.ID)
			return ai - bi
		})
		target.Limits[bucket] = entries
	}
	target.Fingerprint = TRESDigest(target.Raw, target.Kind, "", "")
	return target, nil
}

// Canonicalize TRES arrays by ID. For write verification omit exactly the edited
// resource, leaving every other entry and every other policy field in the hash.
func TRESDigest(raw json.RawMessage, kind, bucket, resourceID string) string {
	var value map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil || value == nil {
		return ""
	}
	for _, key := range client.TRESBuckets(kind) {
		path := client.TRESPath(kind, key)
		current := value
		for _, part := range path[:len(path)-1] {
			next, ok := current[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				current[part] = next
			}
			current = next
		}
		list, _ := current[path[len(path)-1]].([]any)
		out := []any{}
		for _, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				return ""
			}
			id, ok := entry["id"].(json.Number)
			if !ok {
				return ""
			}
			if key == bucket && id.String() == resourceID {
				continue
			}
			out = append(out, item)
		}
		slices.SortFunc(out, func(a, b any) int {
			ai, _ := strconv.ParseUint(a.(map[string]any)["id"].(json.Number).String(), 10, 32)
			bi, _ := strconv.ParseUint(b.(map[string]any)["id"].(json.Number).String(), 10, 32)
			if ai < bi {
				return -1
			}
			if ai > bi {
				return 1
			}
			return 0
		})
		current[path[len(path)-1]] = out
	}
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
func (a *Adapter) TRESLimits(ctx context.Context, kind string) (models.TRESSnapshot, error) {
	v := models.TRESSnapshot{Targets: []models.TRESTarget{}, ReferencedQOS: []string{}}
	var err error
	if len(client.TRESBuckets(kind)) == 0 {
		return v, client.Failure("invalid_request", "TRES 对象类型不合法。")
	}
	v.Cluster, err = a.AccountingCluster(ctx)
	if err != nil {
		return v, err
	}
	v.Resources, err = a.TRESCatalog(ctx)
	if err != nil {
		return v, err
	}
	if kind == "qos" {
		assocs, err := a.AssociationsIn(ctx, v.Cluster)
		if err != nil {
			return v, err
		}
		refs := map[string]bool{}
		for _, assoc := range assocs {
			for _, name := range assoc.QOS {
				refs[name] = true
			}
			if assoc.DefaultQOS != "" {
				refs[assoc.DefaultQOS] = true
			}
		}
		for name := range refs {
			v.ReferencedQOS = append(v.ReferencedQOS, name)
		}
		rows, err := a.AllQOS(ctx)
		if err != nil {
			return v, err
		}
		for _, q := range rows {
			target, err := projectTRES(models.TRESTarget{Kind: kind, ID: q.ID, Name: q.Name, Cluster: v.Cluster, Relative: slices.Contains(q.Flags, "RELATIVE"), Raw: q.Raw})
			if err != nil {
				return v, err
			}
			v.Targets = append(v.Targets, target)
		}
	} else {
		rows, err := a.AssociationsIn(ctx, v.Cluster)
		if err != nil {
			return v, err
		}
		for _, a := range rows {
			target, err := projectTRES(models.TRESTarget{Kind: kind, ID: a.ID, Cluster: v.Cluster, Account: a.Account, User: a.User, Partition: a.Partition, Raw: a.Raw})
			if err != nil {
				return v, err
			}
			v.Targets = append(v.Targets, target)
		}
	}
	return v, nil
}
