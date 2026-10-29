// Package client implements bounded, authenticated access to approved Slurm REST origins.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string           { return e.Message }
func Failure(code, message string) error { return &Error{code, message} }
func Code(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return "unreachable"
}

type Config struct {
	Endpoint, AuthType, Username, TokenFile, Version string
	LocalUsername                                    string
	TLS                                              *tls.Config
	Timeout                                          time.Duration
}
type Client struct{ config Config }
type Resource string

const (
	Ping           Resource = "ping"
	Jobs           Resource = "jobs"
	Job            Resource = "job"
	Nodes          Resource = "nodes"
	Node           Resource = "node"
	Partitions     Resource = "partitions"
	DBAccounts     Resource = "db_accounts"
	DBUsers        Resource = "db_users"
	DBAssociations Resource = "db_associations"
	DBQOS          Resource = "db_qos"
	Shares         Resource = "shares"
	DBTRES         Resource = "db_tres"
	Reservations   Resource = "reservations"
)

var nodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)
var jobPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)

func ValidJobID(id string) bool {
	_, err := strconv.ParseUint(id, 10, 32)
	return jobPattern.MatchString(id) && err == nil
}
func ValidNodeName(name string) bool { return nodePattern.MatchString(name) }
func New(config Config) (*Client, error) {
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || config.Timeout <= 0 || config.Timeout > 30*time.Second || !slices.Contains([]string{"v0.0.41", "v0.0.42", "v0.0.43", "v0.0.44"}, config.Version) {
		return nil, Failure("api_unsupported", "Slurm REST 连接或版本配置不受支持。")
	}
	if u.Scheme == "unix" {
		if u.Host != "" || !filepath.IsAbs(u.Path) || filepath.Clean(u.Path) != u.Path || u.RawPath != "" || config.AuthType != "local" {
			return nil, Failure("profile_unavailable", "本地连接配置不合法。")
		}
	} else if u.Scheme != "https" || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || config.AuthType != "jwt" || config.Username == "" || strings.ContainsAny(config.Username, "\r\n\x00") || !filepath.IsAbs(config.TokenFile) {
		return nil, Failure("profile_unavailable", "远程连接配置不合法。")
	}
	if config.TLS != nil {
		config.TLS = config.TLS.Clone()
		if config.TLS.InsecureSkipVerify {
			return nil, Failure("tls_failed", "不允许跳过 Slurm TLS 证书校验。")
		}
		config.TLS.MinVersion = tls.VersionTLS12
	}
	return &Client{config}, nil
}

type Envelope struct {
	Meta struct {
		Plugin struct {
			Type   string `json:"type"`
			Parser string `json:"data_parser"`
		} `json:"plugin"`
		Slurm struct {
			Release string `json:"release"`
			Cluster string `json:"cluster"`
		} `json:"slurm"`
	} `json:"meta"`
	Errors       []json.RawMessage `json:"errors"`
	Pings        json.RawMessage   `json:"pings"`
	Jobs         json.RawMessage   `json:"jobs"`
	Nodes        json.RawMessage   `json:"nodes"`
	Partitions   json.RawMessage   `json:"partitions"`
	Accounts     json.RawMessage   `json:"accounts"`
	Users        json.RawMessage   `json:"users"`
	Associations json.RawMessage   `json:"associations"`
	QOS          json.RawMessage   `json:"qos"`
	Shares       json.RawMessage   `json:"shares"`
	TRES         json.RawMessage   `json:"TRES"`
	Reservations json.RawMessage   `json:"reservations"`
}

func (c *Client) Read(ctx context.Context, resource Resource, id string) (Envelope, error) {
	var empty Envelope
	path := "/slurm/" + c.config.Version + "/"
	switch resource {
	case DBAccounts, DBUsers, DBAssociations, DBQOS, DBTRES:
		if resource != DBAssociations && id != "" || resource == DBAssociations && !ValidAccountingName(id) {
			return empty, Failure("invalid_request", "记账查询参数不合法。")
		}
		name := map[Resource]string{DBAccounts: "accounts", DBUsers: "users", DBAssociations: "associations", DBQOS: "qos", DBTRES: "tres"}[resource]
		path = "/slurmdb/" + c.config.Version + "/" + name + "/"
		if resource == DBUsers {
			path += "?with_assocs=true"
		}
		if resource == DBAssociations {
			path += "?cluster=" + url.QueryEscape(id)
		}
	case Shares:
		if id != "" {
			return empty, Failure("invalid_request", "Fairshare 查询参数不合法。")
		}
		path += "shares"
	case Ping, Jobs, Nodes, Partitions, Reservations:
		if id != "" {
			return empty, Failure("invalid_request", "查询参数不合法。")
		}
		path += string(resource) + "/"
	case Job:
		if !ValidJobID(id) {
			return empty, Failure("invalid_request", "作业 ID 不合法。")
		}
		path += "job/" + id
	case Node:
		if !ValidNodeName(id) {
			return empty, Failure("invalid_request", "节点名称不合法。")
		}
		path += "node/" + id
	default:
		return empty, Failure("invalid_request", "不支持的 Slurm 查询。")
	}
	return c.request(ctx, http.MethodGet, path, nil, nil)
}

// Cancel only targets one validated numeric job. Callers must enforce platform
// permissions, ownership, fresh state and a durable audit before invoking it.
func (c *Client) Cancel(ctx context.Context, id string) (bool, error) {
	if !ValidJobID(id) {
		return false, Failure("invalid_request", "作业 ID 不合法。")
	}
	sent := false
	_, err := c.request(ctx, http.MethodDelete, "/slurm/"+c.config.Version+"/job/"+id+"?signal=KILL", nil, &sent)
	return sent, err
}

// ControlNode exposes only the two reviewed state transitions for one literal name.
func (c *Client) ControlNode(ctx context.Context, name, action, reason string) (bool, error) {
	if !ValidNodeTarget(name) || !slices.Contains([]string{"drain", "resume"}, action) || utf8.RuneCountInString(reason) < 2 || utf8.RuneCountInString(reason) > 300 || strings.ContainsAny(reason, "\r\n\x00") {
		return false, Failure("invalid_request", "节点、动作或原因不合法。")
	}
	body := map[string]any{"state": []string{strings.ToUpper(action)}}
	if action == "drain" {
		body["reason"] = reason
	}
	data, _ := json.Marshal(body)
	sent := false
	_, err := c.request(ctx, http.MethodPost, "/slurm/"+c.config.Version+"/node/"+name, data, &sent)
	return sent, err
}
func ValidNodeTarget(name string) bool { return ValidNodeName(name) && !strings.EqualFold(name, "ALL") }
func (c *Client) request(ctx context.Context, method, path string, bodyData []byte, sent *bool) (Envelope, error) {
	var empty Envelope
	if strings.HasPrefix(c.config.Endpoint, "unix:") && os.Geteuid() == 0 && c.config.LocalUsername != "" {
		return c.localRequest(ctx, method, path, bodyData, sent)
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	dialer := &net.Dialer{Timeout: c.config.Timeout}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: c.config.TLS, DisableKeepAlives: true, ResponseHeaderTimeout: c.config.Timeout, MaxResponseHeaderBytes: 16384, DialContext: dialer.DialContext}
	defer transport.CloseIdleConnections()
	endpoint := strings.TrimRight(c.config.Endpoint, "/")
	if strings.HasPrefix(c.config.Endpoint, "unix:") {
		u, _ := url.Parse(c.config.Endpoint)
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", u.Path)
		}
		endpoint = "http://slurm-local"
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(bodyData))
	if err != nil {
		return empty, Failure("profile_unavailable", "Slurm 连接配置不合法。")
	}
	request.Header.Set("Accept", "application/json")
	if len(bodyData) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.config.AuthType == "jwt" {
		token, err := readToken(c.config.TokenFile)
		if err != nil {
			return empty, err
		}
		request.Header.Set("X-SLURM-USER-NAME", c.config.Username)
		request.Header.Set("X-SLURM-USER-TOKEN", token)
	}
	httpClient := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if sent != nil {
		*sent = true
	}
	response, err := httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return empty, context.Canceled
			}
			return empty, Failure("timeout", "Slurm 请求超时。")
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return empty, Failure("timeout", "Slurm 请求超时。")
		}
		var verification *tls.CertificateVerificationError
		var unknown x509.UnknownAuthorityError
		if errors.As(err, &verification) || errors.As(err, &unknown) {
			return empty, Failure("tls_failed", "Slurm TLS 证书校验失败。")
		}
		return empty, Failure("unreachable", "无法连接 Slurm REST 服务。")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 200:
	case 500:
		if sent == nil {
			return empty, Failure("unreachable", "Slurm REST 服务未正常响应。")
		}
	case 401, 403:
		if sent != nil {
			*sent = false
		}
		return empty, Failure("auth_failed", "Slurm REST 拒绝该连接身份。")
	case 404:
		if sent != nil {
			*sent = false
		}
		if strings.HasPrefix(path, "/slurmdb/") {
			return empty, Failure("accounting_unavailable", "连接未开放 slurmdbd 记账接口，请管理员配置对应 REST 插件。")
		}
		if strings.Contains(path, "/job/") || strings.Contains(path, "/node/") {
			return empty, Failure("not_found", "Slurm 资源不存在或对连接身份不可见。")
		}
		return empty, Failure("api_unsupported", "Slurm REST 不支持该版本或查询。")
	default:
		return empty, Failure("unreachable", "Slurm REST 服务未正常响应。")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return empty, context.Canceled
		}
		return empty, Failure("timeout", "Slurm 请求超时。")
	}
	if err != nil || len(body) > 8<<20 || json.Unmarshal(body, &empty) != nil {
		return Envelope{}, Failure("invalid_response", "Slurm 响应格式异常或超过大小限制。")
	}
	if strings.HasPrefix(path, "/slurmdb/") && empty.Meta.Plugin.Type != "openapi/slurmdbd" {
		return Envelope{}, Failure("invalid_response", "记账响应来源不是已批准的 slurmdbd 插件。")
	}
	if len(empty.Errors) > 0 {
		// Stable Slurm authorization errors are explicit rejections, even when
		// REST reports them with HTTP 500. Never expose the upstream text.
		if sent != nil && strings.TrimPrefix(empty.Meta.Plugin.Parser, "data_parser/") == c.config.Version && empty.Meta.Slurm.Release != "" && len(empty.Meta.Slurm.Release) <= 64 {
			denied := true
			ambiguous := strings.HasPrefix(path, "/slurmdb/") && method == http.MethodDelete
			for _, raw := range empty.Errors {
				var item struct {
					Number int `json:"error_number"`
				}
				if json.Unmarshal(raw, &item) != nil || item.Number != 9210 {
					ambiguous = false
				}
				if json.Unmarshal(raw, &item) != nil || (item.Number != 2002 && item.Number != 2010) {
					denied = false
				}
			}
			if ambiguous {
				// ESLURM_DATA_AMBIGUOUS_MODIFY: Slurm refuses to commit the
				// singular endpoint when removal would affect several rows.
				*sent = false
				return Envelope{}, Failure("operation_rejected", "Slurm 拒绝了多目标记账变更，未提交删除。请先核对并移除分区子关联。")
			}
			if denied {
				*sent = false
				return Envelope{}, Failure("auth_failed", "Slurm 拒绝固定连接身份，请管理员核实上游身份及动作授权。")
			}
		}
		return Envelope{}, Failure("upstream_error", "Slurm 未完成查询，请检查上游权限与服务状态。")
	}
	if response.StatusCode != 200 {
		return Envelope{}, Failure("unreachable", "Slurm REST 服务未正常响应。")
	}
	if strings.TrimPrefix(empty.Meta.Plugin.Parser, "data_parser/") != c.config.Version {
		return Envelope{}, Failure("api_unsupported", "Slurm 响应 API 版本与配置不一致。")
	}
	if empty.Meta.Slurm.Release == "" || len(empty.Meta.Slurm.Release) > 64 {
		return Envelope{}, Failure("invalid_response", "Slurm 未返回有效版本元数据。")
	}
	return empty, nil
}
func readToken(path string) (string, error) {
	failure := Failure("credentials_unavailable", "服务端 Slurm 凭据不可用。")
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0027 != 0 {
		return "", failure
	}
	f, err := os.Open(path)
	if err != nil {
		return "", failure
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", failure
	}
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	defer clear(data)
	token := strings.TrimSpace(string(data))
	if err != nil || len(data) > 16384 || token == "" || strings.ContainsAny(token, "\r\n\x00 \t") {
		return "", failure
	}
	return token, nil
}
