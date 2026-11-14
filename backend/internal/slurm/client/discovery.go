package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"slurm-console/backend/internal/slurm/protocol"
)

// Versions reads the slurmrestd OpenAPI document. Only complete controller read
// contracts are candidates; no business request or mutation is retried/version-switched.
func (c *Client) Versions(ctx context.Context) ([]string, error) {
	if c.config.LocalUsername != "" {
		return nil, Failure("api_unsupported", "接口发现需要使用当前服务进程的连接身份。")
	}
	for _, path := range []string{"/openapi/v3", "/openapi.json"} {
		var available []string
		_, err := c.exchange(ctx, http.MethodGet, path, nil, nil, func(ctx context.Context, response *http.Response) (Envelope, error) {
			if response.StatusCode == http.StatusNotFound {
				return Envelope{}, Failure("api_unsupported", "slurmrestd 未开放接口说明，请手动选择 API 版本。")
			}
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
				return Envelope{}, Failure("auth_failed", "Slurm REST 拒绝该连接身份。")
			}
			if response.StatusCode != http.StatusOK {
				return Envelope{}, Failure("unreachable", "Slurm REST 服务未正常响应。")
			}
			raw, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return Envelope{}, context.Canceled
				}
				return Envelope{}, Failure("timeout", "Slurm 接口发现超时。")
			}
			if err != nil || len(raw) > 16<<20 {
				return Envelope{}, Failure("invalid_response", "Slurm 接口说明无效或超过大小限制。")
			}
			var document struct {
				OpenAPI string                                `json:"openapi"`
				Paths   map[string]map[string]json.RawMessage `json:"paths"`
			}
			if json.Unmarshal(raw, &document) != nil || !strings.HasPrefix(document.OpenAPI, "3.") || len(document.Paths) == 0 || len(document.Paths) > 10000 {
				return Envelope{}, Failure("invalid_response", "Slurm 未返回有效的 OpenAPI 接口说明。")
			}
			reads := map[string]bool{}
			for path, methods := range document.Paths {
				if get := strings.TrimSpace(string(methods["get"])); strings.HasPrefix(get, "{") {
					reads[strings.TrimSuffix(path, "/")] = true
				}
			}
			for _, version := range protocol.Versions() {
				complete := true
				for _, resource := range []string{"ping", "jobs", "nodes", "partitions"} {
					complete = complete && reads["/slurm/"+version+"/"+resource]
				}
				if complete {
					available = append(available, version)
				}
			}
			return Envelope{}, nil
		})
		if err == nil {
			return available, nil
		}
		if Code(err) != "api_unsupported" {
			return nil, err
		}
	}
	return nil, Failure("api_unsupported", "slurmrestd 未开放接口说明，请手动选择 API 版本。")
}
