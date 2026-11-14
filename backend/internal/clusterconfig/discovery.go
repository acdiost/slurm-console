package clusterconfig

import (
	"context"
	"slices"
	"time"

	"slurm-console/backend/internal/slurm/client"
)

type VersionDiscovery struct {
	Versions    []string `json:"versions"`
	Recommended string   `json:"recommended"`
}

func (p Profile) Discover(ctx context.Context, timeout time.Duration) (VersionDiscovery, error) {
	result := VersionDiscovery{Versions: []string{}}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if len(p.Versions) == 0 {
		return result, ErrInvalid
	}
	c, err := p.Client(p.Versions[0], timeout)
	if err != nil {
		return result, err
	}
	versions, err := c.Versions(ctx)
	if err != nil {
		return result, err
	}
	for _, version := range versions {
		if slices.Contains(p.Versions, version) {
			result.Versions = append(result.Versions, version)
		}
	}
	if len(result.Versions) == 0 {
		return result, client.Failure("api_unsupported", "未找到服务端批准且提供完整查询接口的 v0.0.40–v0.0.44 版本。")
	}
	result.Recommended = result.Versions[0]
	return result, nil
}

func (m *Manager) Discover(ctx context.Context, ref string) (VersionDiscovery, error) {
	p, ok := m.Registry.Profiles[ref]
	if !ok {
		return VersionDiscovery{}, ErrNotFound
	}
	key := "profile:" + ref
	if _, busy := m.busy.LoadOrStore(key, true); busy {
		return VersionDiscovery{}, ErrBusy
	}
	defer m.busy.Delete(key)
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	default:
		return VersionDiscovery{}, ErrBusy
	}
	return p.Discover(ctx, 5*time.Second)
}
