package aiassist

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Config struct {
	Enabled         bool     `json:"enabled"`
	Label           string   `json:"label"`
	Endpoint        string   `json:"endpoint"`
	Model           string   `json:"model"`
	ClusterIDs      []string `json:"clusterIds"`
	APIKeyFile      string   `json:"apiKeyFile,omitempty"`
	CAFile          string   `json:"caFile,omitempty"`
	TimeoutSeconds  int      `json:"timeoutSeconds,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	TokenParameter  string   `json:"tokenParameter,omitempty"`
}
type Profile struct {
	config Config
	client *http.Client
	key    string
}
type Destination struct {
	Label    string `json:"label"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

func ReadFile(path string, limit int64, secret bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("AI file path must be absolute")
	}
	before, e := os.Lstat(path)
	if e != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 || secret && before.Mode().Perm()&0077 != 0 {
		return nil, errors.New("unsafe AI file")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("AI file unavailable")
	}
	defer f.Close()
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		return nil, errors.New("AI file changed")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("AI file exceeds limit")
	}
	return b, nil
}
func LoadConfig(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}
	b, e := ReadFile(path, 32768, false)
	if e != nil {
		return Config{}, e
	}
	var c Config
	if decode(b, &c) != nil {
		return Config{}, errors.New("invalid AI configuration JSON")
	}
	return c, nil
}
func Load(path string) (*Profile, error) {
	c, e := LoadConfig(path)
	if e != nil {
		return nil, e
	}
	return NewProfile(c)
}

func text(s string, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) > 0 && utf8.RuneCountInString(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
}
func parseEndpoint(endpoint string, privateConfig bool) (*url.URL, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" || !strings.HasSuffix(u.Path, "/chat/completions") || len(endpoint) > 2048 {
		return nil, errors.New("AI endpoint must be a fixed chat/completions URL without credentials or parameters")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback()) {
		return nil, errors.New("AI requires HTTPS or literal loopback HTTP")
	}
	if u.Scheme == "http" && privateConfig {
		return nil, errors.New("loopback AI HTTP must not carry credentials or custom CA")
	}
	return u, nil
}

// ValidateEndpoint also applies to disabled drafts: credential-bearing URLs are never metadata.
func ValidateEndpoint(endpoint string, privateConfig bool) error {
	_, e := parseEndpoint(endpoint, privateConfig)
	return e
}

func NewProfile(c Config) (*Profile, error) {
	if !c.Enabled {
		return nil, nil
	}
	if !text(c.Label, 100) || !text(c.Model, 120) || len(c.ClusterIDs) == 0 || len(c.ClusterIDs) > 100 {
		return nil, errors.New("AI provider, model and explicit cluster list required")
	}
	seen := map[string]bool{}
	for _, id := range c.ClusterIDs {
		if !regexp.MustCompile(`^[a-z0-9]{15}$`).MatchString(id) || seen[id] {
			return nil, errors.New("invalid AI cluster binding")
		}
		seen[id] = true
	}
	u, e := parseEndpoint(c.Endpoint, c.APIKeyFile != "" || c.CAFile != "")
	if e != nil {
		return nil, e
	}

	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 30
	}
	if c.MaxOutputTokens == 0 {
		c.MaxOutputTokens = 2048
	}
	if c.TokenParameter == "" {
		c.TokenParameter = "max_completion_tokens"
	}
	if c.TimeoutSeconds < 5 || c.TimeoutSeconds > 60 || c.MaxOutputTokens < 512 || c.MaxOutputTokens > 4096 || (c.TokenParameter != "max_completion_tokens" && c.TokenParameter != "max_tokens") {
		return nil, errors.New("invalid AI request budget")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if c.CAFile != "" {
		b, e := ReadFile(c.CAFile, 1<<20, false)
		if e != nil {
			return nil, e
		}
		pool, e := x509.SystemCertPool()
		if e != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, errors.New("invalid AI CA")
		}
		tlsConfig.RootCAs = pool
	}
	if c.APIKeyFile != "" {
		if _, e := apiKey(c.APIKeyFile); e != nil {
			return nil, e
		}
	}
	c.ClusterIDs = append([]string(nil), c.ClusterIDs...)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: tlsConfig, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: time.Duration(c.TimeoutSeconds) * time.Second, MaxResponseHeaderBytes: 16384, MaxIdleConns: 2, MaxIdleConnsPerHost: 2}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return &Profile{config: c, key: hex.EncodeToString(sum[:]), client: &http.Client{Transport: transport, Timeout: time.Duration(c.TimeoutSeconds) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("AI redirects disabled") }}}, nil
}
func apiKey(path string) (string, error) {
	b, e := ReadFile(path, 8192, true)
	if e != nil {
		return "", e
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", errors.New("empty AI key")
	}
	for _, c := range []byte(v) {
		if c <= 32 || c >= 127 {
			return "", errors.New("invalid AI key")
		}
	}
	return v, nil
}
func (p *Profile) destination() Destination {
	return Destination{p.config.Label, p.config.Endpoint, p.config.Model}
}
