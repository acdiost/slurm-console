package ldapauth

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
	"strings"

	"github.com/go-ldap/ldap/v3"
)

type Config struct {
	URL               string `json:"url"`
	BaseDN            string `json:"baseDN"`
	BindDN            string `json:"bindDN"`
	BindPasswordFile  string `json:"bindPasswordFile"`
	CAFile            string `json:"caFile"`
	UserFilter        string `json:"userFilter"`
	UsernameAttribute string `json:"usernameAttribute"`
	IDAttribute       string `json:"idAttribute"`
}

func Load(path string) (*Client, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("LDAP configuration path must be absolute")
	}
	data, err := readFile(path, 65536, false)
	if err != nil {
		return nil, errors.New("cannot read LDAP configuration file")
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return nil, errors.New("invalid LDAP configuration JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid trailing LDAP configuration data")
	}
	return New(config)
}

func New(config Config) (*Client, error) {
	u, err := url.Parse(config.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("LDAP URL must be ldap:// or ldaps:// without credentials, query or base DN")
	}
	if config.BaseDN == "" || config.BindDN == "" {
		return nil, errors.New("LDAP baseDN and bindDN are required")
	}
	if _, err := ldap.ParseDN(config.BaseDN); err != nil {
		return nil, errors.New("invalid LDAP baseDN")
	}
	if _, err := ldap.ParseDN(config.BindDN); err != nil {
		return nil, errors.New("invalid LDAP bindDN")
	}
	if config.UsernameAttribute == "" {
		config.UsernameAttribute = "uid"
	}
	if config.IDAttribute == "" {
		config.IDAttribute = "entryUUID"
	}
	attribute := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
	if !attribute.MatchString(config.UsernameAttribute) || !attribute.MatchString(config.IDAttribute) {
		return nil, errors.New("invalid LDAP identity attribute name")
	}
	if config.UserFilter == "" {
		config.UserFilter = "(&(objectClass=inetOrgPerson)(uid={username})(!(pwdAccountLockedTime=*))(!(pwdReset=TRUE)))"
	}
	if strings.Count(config.UserFilter, "{username}") != 1 {
		return nil, errors.New("LDAP userFilter requires exactly one {username} placeholder")
	}
	if _, err := ldap.CompileFilter(strings.ReplaceAll(config.UserFilter, "{username}", ldap.EscapeFilter("probe"))); err != nil {
		return nil, errors.New("invalid LDAP userFilter")
	}
	if !filepath.IsAbs(config.BindPasswordFile) {
		return nil, errors.New("LDAP bindPasswordFile must be absolute")
	}
	secret, err := readFile(config.BindPasswordFile, 4096, true)
	if err != nil {
		return nil, errors.New("cannot read restricted LDAP bind password file")
	}
	defer clear(secret)
	password := strings.TrimSuffix(strings.TrimSuffix(string(secret), "\n"), "\r")
	if password == "" || strings.ContainsRune(password, 0) {
		return nil, errors.New("LDAP service bind password must not be empty or contain NUL")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if config.CAFile != "" {
		if !filepath.IsAbs(config.CAFile) {
			return nil, errors.New("LDAP caFile must be absolute")
		}
		pem, err := readFile(config.CAFile, 1048576, false)
		if err != nil {
			return nil, errors.New("cannot read LDAP CA file")
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("LDAP CA file contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	port := u.Port()
	if port == "" {
		port = "389"
		if u.Scheme == "ldaps" {
			port = "636"
		}
	}
	return &Client{config: config, host: u.Hostname(), port: port, ldaps: u.Scheme == "ldaps", tls: tlsConfig, bindPassword: password, slots: make(chan struct{}, 8)}, nil
}

func readFile(path string, limit int64, secret bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || secret && info.Mode().Perm()&0007 != 0 {
		return nil, errors.New("unsafe LDAP file permissions")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("LDAP file exceeds size limit")
	}
	return data, nil
}
