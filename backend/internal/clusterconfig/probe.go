package clusterconfig

import (
	"context"
	"encoding/json"
	"time"

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
	envelope, err := connection.Read(ctx, client.Ping, "")
	if err != nil {
		return finish(client.Code(err), err.Error())
	}
	var body struct {
		Pings []struct {
			Hostname string `json:"hostname"`
			Pinged   string `json:"pinged"`
			Mode     string `json:"mode"`
		} `json:"pings"`
	}
	if json.Unmarshal(envelope.Pings, &body.Pings) != nil || len(body.Pings) == 0 || len(body.Pings) > 16 {
		return finish("invalid_response", "目标服务未返回有效的 Slurm 控制器检测结果。")
	}
	result.ServerVersion = envelope.Meta.Slurm.Release
	up := false
	for _, ping := range body.Pings {
		if ping.Hostname == "" || len(ping.Hostname) > 255 || len(ping.Mode) > 32 || (ping.Pinged != "UP" && ping.Pinged != "DOWN") {
			return finish("invalid_response", "控制器响应字段不合法。")
		}
		result.Controllers = append(result.Controllers, Controller{Hostname: ping.Hostname, State: ping.Pinged, Role: ping.Mode})
		if ping.Pinged == "UP" {
			up = true
		}
	}
	if !up {
		return finish("controller_down", "REST 服务已响应，但 Slurm 控制器不可达。")
	}
	return finish("connected", "连接成功，Slurm 控制器已响应。")
}
