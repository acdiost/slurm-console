package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"slurm-console/backend/internal/slurm/client"
	"slurm-console/backend/internal/slurm/models"
	"strconv"
)

type decimal struct{ models.Decimal }

func (n *decimal) UnmarshalJSON(raw []byte) error {
	n.Decimal = models.Decimal{}
	var v struct {
		Set      *bool
		Infinite bool
		Number   *float64
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if json.Unmarshal(raw, &v) != nil || v.Set == nil {
		return fmt.Errorf("invalid decimal flags")
	}
	n.Infinite = v.Infinite
	if *v.Set && !v.Infinite {
		if v.Number == nil || math.IsNaN(*v.Number) || math.IsInf(*v.Number, 0) || *v.Number < 0 {
			return fmt.Errorf("invalid decimal")
		}
		n.Value = v.Number
	}
	return nil
}

func (a *Adapter) AllQOS(ctx context.Context) ([]models.QOS, error) {
	env, err := a.reader.Read(ctx, client.DBQOS, "")
	if err != nil {
		return nil, err
	}
	values, err := decode[json.RawMessage](env.QOS)
	if err != nil {
		return nil, err
	}
	out := []models.QOS{}
	ids := map[int64]bool{}
	names := map[string]bool{}
	for _, raw := range values {
		var v struct {
			ID                *int64
			Name, Description string
			Flags             []string
			Priority          noVal
			UsageFactor       decimal `json:"usage_factor"`
			Preempt           struct{ Mode []string }
			Limits            struct {
				Max struct {
					Jobs struct {
						Active struct{ Per struct{ User noVal } } `json:"active_jobs"`
						Per    struct{ User noVal }
					}
					Wall struct{ Per struct{ Job noVal } } `json:"wall_clock"`
				}
			}
		}
		if json.Unmarshal(raw, &v) != nil {
			return nil, invalid()
		}
		if slices.Contains(v.Flags, "DELETED") {
			continue
		}
		if v.ID == nil || *v.ID <= 0 || *v.ID > 4294967295 || ids[*v.ID] || names[v.Name] || !client.ValidAccountingName(v.Name) || len(v.Description) > 4096 || len(v.Flags) > 64 || len(v.Preempt.Mode) > 16 {
			return nil, invalid()
		}
		ids[*v.ID] = true
		names[v.Name] = true
		if v.Flags == nil {
			v.Flags = []string{}
		}
		if v.Preempt.Mode == nil {
			v.Preempt.Mode = []string{}
		}
		for _, list := range [][]string{v.Flags, v.Preempt.Mode} {
			for _, name := range list {
				if len(name) > 128 {
					return nil, invalid()
				}
			}
		}
		// Canonicalize the full policy, including fields not exposed for editing.
		var canonical any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&canonical) != nil {
			return nil, invalid()
		}
		encoded, _ := json.Marshal(canonical)
		sum := sha256.Sum256(encoded)
		unmanaged := canonical.(map[string]any)
		for _, key := range []string{"description", "priority", "usage_factor"} {
			delete(unmanaged, key)
		}
		for _, path := range [][]string{{"limits", "max", "jobs", "active_jobs", "per", "user"}, {"limits", "max", "jobs", "per", "user"}, {"limits", "max", "wall_clock", "per", "job"}} {
			current := unmanaged
			for _, key := range path[:len(path)-1] {
				next, ok := current[key].(map[string]any)
				if !ok {
					current = nil
					break
				}
				current = next
			}
			if current != nil {
				delete(current, path[len(path)-1])
			}
		}
		unmanagedRaw, _ := json.Marshal(unmanaged)
		unmanagedSum := sha256.Sum256(unmanagedRaw)
		out = append(out, models.QOS{ID: strconv.FormatInt(*v.ID, 10), Name: v.Name, Description: v.Description, Flags: v.Flags, PreemptModes: v.Preempt.Mode, Priority: v.Priority.Number, UsageFactor: v.UsageFactor.Decimal, MaxJobsPerUser: v.Limits.Max.Jobs.Active.Per.User.Number, MaxSubmitJobsPerUser: v.Limits.Max.Jobs.Per.User.Number, MaxWallMinutes: v.Limits.Max.Wall.Per.Job.Number, Fingerprint: hex.EncodeToString(sum[:]), UnmanagedFingerprint: hex.EncodeToString(unmanagedSum[:]), Raw: raw})
	}
	return out, nil
}

func (a *Adapter) Policy(ctx context.Context) (models.PolicySnapshot, error) {
	v := models.PolicySnapshot{}
	var err error
	v.Cluster, err = a.AccountingCluster(ctx)
	if err != nil {
		return v, err
	}
	v.Associations, err = a.AssociationsIn(ctx, v.Cluster)
	if err != nil {
		return v, err
	}
	v.QOS, err = a.AllQOS(ctx)
	return v, err
}

func (a *Adapter) Fairshare(ctx context.Context) ([]models.Fairshare, error) {
	cluster, err := a.AccountingCluster(ctx)
	if err != nil {
		return nil, err
	}
	assocs, err := a.AssociationsIn(ctx, cluster)
	if err != nil {
		return nil, err
	}
	byID := map[string]models.Association{}
	for _, v := range assocs {
		byID[v.ID] = v
	}
	env, err := a.reader.Read(ctx, client.Shares, "")
	if err != nil {
		return nil, err
	}
	if env.Meta.Plugin.Type != "openapi/slurmctld" {
		return nil, invalid()
	}
	var wrapper struct{ Shares json.RawMessage }
	if json.Unmarshal(env.Shares, &wrapper) != nil {
		return nil, invalid()
	}
	values, err := decode[struct {
		ID                               *int64
		Cluster, Name, Parent, Partition string
		Type                             []string
		Shares                           noVal
		NormalizedShares                 decimal `json:"shares_normalized"`
		Usage                            *uint64
		NormalizedUsage                  decimal `json:"usage_normalized"`
		EffectiveUsage                   decimal `json:"effective_usage"`
		Fairshare                        struct{ Factor, Level decimal }
	}](wrapper.Shares)
	if err != nil {
		return nil, err
	}
	out := []models.Fairshare{}
	seen := map[string]bool{}
	for _, v := range values {
		if v.Cluster != cluster {
			continue
		}
		if v.ID == nil || *v.ID <= 0 || *v.ID > 4294967295 {
			return nil, invalid()
		}
		id := strconv.FormatInt(*v.ID, 10)
		assoc, ok := byID[id]
		if !ok {
			continue
		}
		name, kind := assoc.Account, "ASSOCIATION"
		if assoc.User != "" {
			name, kind = assoc.User, "USER"
		}
		if seen[id] || v.Name != name || len(v.Type) != 1 || v.Type[0] != kind || v.Partition != assoc.Partition || len(v.Parent) > 64 || v.Usage == nil {
			return nil, invalid()
		}
		seen[id] = true
		out = append(out, models.Fairshare{ID: id, Cluster: cluster, Account: assoc.Account, User: assoc.User, Partition: assoc.Partition, Parent: v.Parent, Shares: v.Shares.Number, NormalizedShares: v.NormalizedShares.Decimal, RawUsage: strconv.FormatUint(*v.Usage, 10), NormalizedUsage: v.NormalizedUsage.Decimal, EffectiveUsage: v.EffectiveUsage.Decimal, Factor: v.Fairshare.Factor.Decimal, Level: v.Fairshare.Level.Decimal})
	}
	return out, nil
}
