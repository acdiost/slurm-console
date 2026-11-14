package clusterconfig

import (
	"context"
	"time"

	"slurm-console/backend/internal/slurm/adapter"
	"slurm-console/backend/internal/slurm/client"
)

type Controller struct {
	Hostname string `json:"hostname"`
	State    string `json:"state"`
	Role     string `json:"role"`
}
type ProbeResult struct {
	Status        string       `json:"status"`
	Message       string       `json:"message"`
	CheckedAt     string       `json:"checkedAt"`
	DurationMS    int64        `json:"durationMs"`
	ServerVersion string       `json:"serverVersion"`
	Controllers   []Controller `json:"controllers"`
}

func (profile Profile) Client(version string, timeout time.Duration) (*client.Client, error) {
	// Production services run as root and use that same Unix socket identity.
	// Accept legacy localUser metadata without selecting a different peer UID.
	return client.New(client.Config{Endpoint: profile.Endpoint, AuthType: profile.AuthType, Username: profile.Username, TokenFile: profile.TokenFile, TLS: profile.tls, Version: version, Timeout: timeout})
}
func Probe(ctx context.Context, profile Profile, version string, timeout time.Duration) ProbeResult {
	started := time.Now()
	result := ProbeResult{Controllers: []Controller{}}
	finish := func(status, message string) ProbeResult {
		result.Status = status
		result.Message = message
		result.DurationMS = time.Since(started).Milliseconds()
		result.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return result
	}
	connection, err := profile.Client(version, timeout)
	if err != nil {
		return finish(client.Code(err), err.Error())
	}
	adapted, err := adapter.New(connection, version)
	if err != nil {
		return finish(client.Code(err), err.Error())
	}
	health, err := adapted.Health(ctx)
	if err != nil {
		return finish(client.Code(err), err.Error())
	}
	result.ServerVersion = health.ServerVersion
	for _, ping := range health.Controllers {
		result.Controllers = append(result.Controllers, Controller{Hostname: ping.Hostname, State: ping.State, Role: ping.Role})
	}
	if health.Status != "connected" {
		return finish("controller_down", "REST 服务已响应，但 Slurm 控制器不可达。")
	}
	return finish("connected", "连接成功，Slurm 控制器已响应。")
}
