package clusterconfig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
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

func Probe(ctx context.Context, profile Profile, version string, timeout time.Duration) ProbeResult {
	started := time.Now()
	result := ProbeResult{Status: "unreachable", Message: "无法连接 Slurm REST 服务。", Controllers: []Controller{}}
	finish := func(status, message string) ProbeResult {
		result.Status = status
		result.Message = message
		result.DurationMS = time.Since(started).Milliseconds()
		result.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: profile.tls, DisableKeepAlives: true, ResponseHeaderTimeout: timeout, MaxResponseHeaderBytes: 16384}
	defer transport.CloseIdleConnections()
	endpoint := profile.Endpoint
	if strings.HasPrefix(endpoint, "unix:") {
		u, _ := url.Parse(endpoint)
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", u.Path)
		}
		endpoint = "http://slurm-local"
	} else {
		transport.DialContext = dialer.DialContext
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/slurm/"+version+"/ping", nil)
	if err != nil {
		return finish("unreachable", "连接配置不合法。")
	}
	request.Header.Set("Accept", "application/json")
	if profile.AuthType == "jwt" {
		data, err := readRestrictedFile(profile.TokenFile, 16384, true)
		if err != nil {
			return finish("credentials_unavailable", "服务端连接凭据不可用，请联系管理员。")
		}
		token := strings.TrimSpace(string(data))
		clear(data)
		if token == "" || strings.ContainsAny(token, "\r\n\x00 ") {
			return finish("credentials_unavailable", "服务端连接凭据格式不正确。")
		}
		request.Header.Set("X-SLURM-USER-NAME", profile.Username)
		request.Header.Set("X-SLURM-USER-TOKEN", token)
	}
	response, err := client.Do(request)
	if err != nil {
		var network net.Error
		var verification *tls.CertificateVerificationError
		var unknown x509.UnknownAuthorityError
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
			return finish("timeout", "连接超时，请检查服务和网络。")
		}
		if errors.As(err, &verification) || errors.As(err, &unknown) {
			return finish("tls_failed", "TLS 证书校验失败，请检查服务器证书与 CA。")
		}
		return finish("unreachable", "无法连接 Slurm REST 服务。")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return finish("auth_failed", "Slurm REST 拒绝认证或未授权该连接身份。")
	}
	if response.StatusCode == 404 {
		return finish("api_unsupported", "目标服务不支持所选 Slurm REST API 版本。")
	}
	if response.StatusCode != 200 {
		return finish("unreachable", "Slurm REST 服务未正常响应。")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1048577))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return finish("timeout", "连接超时，请检查服务和网络。")
	}
	if err != nil || len(data) > 1048576 {
		return finish("invalid_response", "Slurm REST 响应异常或超过大小限制。")
	}
	var body struct {
		Meta struct {
			Slurm struct {
				Release string `json:"release"`
			} `json:"slurm"`
			Plugin struct {
				Parser string `json:"data_parser"`
			} `json:"plugin"`
		} `json:"meta"`
		Pings []struct {
			Hostname string `json:"hostname"`
			Pinged   string `json:"pinged"`
			Mode     string `json:"mode"`
		} `json:"pings"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(data, &body) != nil || len(body.Pings) == 0 || len(body.Pings) > 16 || len(body.Errors) > 0 || body.Meta.Slurm.Release == "" || len(body.Meta.Slurm.Release) > 64 {
		return finish("invalid_response", "目标服务未返回有效的 Slurm 控制器检测结果。")
	}
	if strings.TrimPrefix(body.Meta.Plugin.Parser, "data_parser/") != version {
		return finish("api_unsupported", "目标服务返回的 API 版本与配置不一致。")
	}
	result.ServerVersion = body.Meta.Slurm.Release
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
