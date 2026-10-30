package gpumetrics

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slurm-console/backend/internal/slurm/client"
	"strings"
)

var labelPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,99}$`)
var uuidPattern = regexp.MustCompile(`^GPU-[a-zA-Z0-9-]{1,100}$`)

type Profile struct {
	Label           string            `json:"label"`
	Endpoint        string            `json:"endpoint"`
	ClusterLabel    string            `json:"clusterLabel"`
	ClusterValue    string            `json:"clusterValue"`
	NodeLabel       string            `json:"nodeLabel"`
	UtilizationUnit string            `json:"utilizationUnit"`
	NodeMap         map[string]string `json:"nodeMap,omitempty"`
	TokenFile       string            `json:"tokenFile,omitempty"`
	CAFile          string            `json:"caFile,omitempty"`
	tls             *tls.Config
}
type Registry struct {
	Clusters map[string]Profile `json:"clusters"`
}

func restricted(path string, limit int64, secret bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("metrics path must be absolute")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || secret && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("unsafe metrics file")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("metrics file exceeds limit")
	}
	return b, nil
}
func LoadRegistry(path string) (*Registry, error) {
	if path == "" {
		return &Registry{Clusters: map[string]Profile{}}, nil
	}
	raw, e := restricted(path, 65536, false)
	if e != nil {
		return nil, e
	}
	var r Registry
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid metrics profiles JSON")
	}
	return NewRegistry(r.Clusters)
}
func NewRegistry(profiles map[string]Profile) (*Registry, error) {
	r := &Registry{Clusters: map[string]Profile{}}
	if len(profiles) > 100 {
		return nil, errors.New("too many metrics profiles")
	}
	for id, p := range profiles {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(id) || p.Label == "" || len(p.Label) > 160 || strings.ContainsAny(p.Label, "\r\n\x00") || !labelPattern.MatchString(p.ClusterLabel) || p.ClusterLabel == "__name__" || p.ClusterLabel == "UUID" || p.ClusterValue == "" || len(p.ClusterValue) > 128 || strings.ContainsAny(p.ClusterValue, "\r\n\x00") || !labelPattern.MatchString(p.NodeLabel) || p.NodeLabel == p.ClusterLabel || p.NodeLabel == "__name__" || p.NodeLabel == "UUID" || (p.UtilizationUnit != "percent" && p.UtilizationUnit != "ratio") {
			return nil, errors.New("invalid metrics scope or units")
		}
		u, e := url.Parse(p.Endpoint)
		if e != nil || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("metrics endpoint must be an origin URL")
		}
		if u.Scheme != "https" && (u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback()) {
			return nil, errors.New("metrics requires verified HTTPS or literal loopback HTTP")
		}
		if u.Scheme == "http" && (p.TokenFile != "" || p.CAFile != "") {
			return nil, errors.New("loopback HTTP cannot carry credentials")
		}
		p.Endpoint = u.Scheme + "://" + u.Host
		p.tls = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
		if p.CAFile != "" {
			raw, e := restricted(p.CAFile, 1<<20, false)
			if e != nil {
				return nil, e
			}
			pool, e := x509.SystemCertPool()
			if e != nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(raw) {
				return nil, errors.New("invalid metrics CA")
			}
			p.tls.RootCAs = pool
		}
		if p.TokenFile != "" {
			b, e := restricted(p.TokenFile, 8192, true)
			if e != nil {
				return nil, e
			}
			if !validToken(b) {
				return nil, errors.New("invalid metrics token")
			}
		}
		if len(p.NodeMap) > 20000 {
			return nil, errors.New("too many metrics node aliases")
		}
		p.NodeMap = cloneMap(p.NodeMap)
		seen := map[string]bool{}
		for host, node := range p.NodeMap {
			if host == "" || len(host) > 255 || strings.ContainsAny(host, "\r\n\x00") || !client.ValidNodeName(node) || seen[node] {
				return nil, errors.New("metrics node map must be unambiguous")
			}
			seen[node] = true
		}
		r.Clusters[id] = p
	}
	return r, nil
}
func cloneMap(m map[string]string) map[string]string {
	v := map[string]string{}
	for k, x := range m {
		v[k] = x
	}
	return v
}
func validToken(b []byte) bool {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}
