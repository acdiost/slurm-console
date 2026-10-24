package clusterconfig

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
)

var SupportedVersions = []string{"v0.0.41", "v0.0.42", "v0.0.43", "v0.0.44"}
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type Profile struct {
	Label     string   `json:"label"`
	Endpoint  string   `json:"endpoint"`
	AuthType  string   `json:"authType"`
	Versions  []string `json:"apiVersions"`
	Username  string   `json:"username,omitempty"`
	TokenFile string   `json:"tokenFile,omitempty"`
	CAFile    string   `json:"caFile,omitempty"`
	tls       *tls.Config
}
type ProfileView struct {
	Ref      string   `json:"ref"`
	Label    string   `json:"label"`
	Endpoint string   `json:"endpoint"`
	AuthType string   `json:"authType"`
	Versions []string `json:"apiVersions"`
}
type Registry struct {
	Profiles map[string]Profile `json:"profiles"`
}

func LoadRegistry(path string) (*Registry, error) {
	if path == "" {
		return &Registry{Profiles: map[string]Profile{}}, nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("cluster profiles path must be absolute")
	}
	data, err := readRestrictedFile(path, 65536, false)
	if err != nil {
		return nil, errors.New("cannot read cluster connection profiles")
	}
	var registry Registry
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&registry) != nil {
		return nil, errors.New("invalid cluster profiles JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid trailing cluster profile data")
	}
	return NewRegistry(registry.Profiles)
}

func NewRegistry(profiles map[string]Profile) (*Registry, error) {
	result := &Registry{Profiles: map[string]Profile{}}
	if len(profiles) > 100 {
		return nil, errors.New("too many cluster profiles")
	}
	for ref, profile := range profiles {
		if !keyPattern.MatchString(ref) || profile.Label == "" || len(profile.Label) > 160 || len(profile.Versions) == 0 {
			return nil, errors.New("invalid cluster profile identity or versions")
		}
		for _, version := range profile.Versions {
			if !slices.Contains(SupportedVersions, version) {
				return nil, errors.New("unsupported Slurm REST API version in cluster profile")
			}
		}
		u, err := url.Parse(profile.Endpoint)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid cluster profile endpoint")
		}
		switch u.Scheme {
		case "unix":
			if u.Host != "" || !filepath.IsAbs(u.Path) || filepath.Clean(u.Path) != u.Path || u.RawPath != "" || profile.AuthType != "local" || profile.TokenFile != "" || profile.Username != "" || profile.CAFile != "" {
				return nil, errors.New("Unix profiles require an absolute socket and local authentication")
			}
		case "https":
			if u.Hostname() == "" || (u.Path != "" && u.Path != "/") || profile.AuthType != "jwt" || !keyPattern.MatchString(profile.Username) || !filepath.IsAbs(profile.TokenFile) {
				return nil, errors.New("HTTPS profiles require JWT identity, protected token file and an origin URL")
			}
			profile.Endpoint = "https://" + u.Host
			profile.tls = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
			if profile.CAFile != "" {
				if !filepath.IsAbs(profile.CAFile) {
					return nil, errors.New("cluster CA path must be absolute")
				}
				data, err := readRestrictedFile(profile.CAFile, 1048576, false)
				if err != nil {
					return nil, errors.New("cannot read cluster CA file")
				}
				pool, err := x509.SystemCertPool()
				if err != nil {
					pool = x509.NewCertPool()
				}
				if !pool.AppendCertsFromPEM(data) {
					return nil, errors.New("invalid cluster CA certificate")
				}
				profile.tls.RootCAs = pool
			}
		default:
			return nil, errors.New("cluster endpoints must use verified HTTPS or local Unix sockets")
		}
		profile.Versions = slices.Clone(profile.Versions)
		result.Profiles[ref] = profile
	}
	return result, nil
}

func (registry *Registry) Views() []ProfileView {
	views := []ProfileView{}
	if registry == nil {
		return views
	}
	for ref, profile := range registry.Profiles {
		views = append(views, ProfileView{Ref: ref, Label: profile.Label, Endpoint: profile.Endpoint, AuthType: profile.AuthType, Versions: slices.Clone(profile.Versions)})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Ref < views[j].Ref })
	return views
}

func readRestrictedFile(path string, limit int64, secret bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || secret && info.Mode().Perm()&0007 != 0 {
		return nil, errors.New("unsafe cluster file permissions")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("cluster file exceeds size limit")
	}
	return data, nil
}
