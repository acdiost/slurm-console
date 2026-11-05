// Package freeipa connects to a configured FreeIPA server over verified HTTPS.
package freeipa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("FreeIPA unavailable")
var ErrDenied = errors.New("FreeIPA identity denied")
var ErrNotFound = errors.New("FreeIPA entry not found")
var ErrRejected = errors.New("FreeIPA rejected operation")
var GroupName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_ .-]{0,63}$`)
var Name = regexp.MustCompile(`^[a-z_][a-z0-9_.-]{0,62}$`)

const StoreKey = "platform.freeipa"

type Info struct {
	Configured bool   `json:"configured"`
	Domain     string `json:"domain"`
	Realm      string `json:"realm"`
}
type Config struct {
	URL          string `json:"url"`
	Domain       string `json:"domain"`
	Realm        string `json:"realm"`
	Username     string `json:"username"`
	PasswordFile string `json:"passwordFile"`
	CAFile       string `json:"caFile"`
}
type User struct {
	ID              string   `json:"-"`
	Username        string   `json:"username"`
	FirstName       string   `json:"firstName"`
	LastName        string   `json:"lastName"`
	Email           string   `json:"email"`
	UID             string   `json:"uid"`
	GID             string   `json:"gid"`
	Enabled         bool     `json:"enabled"`
	PasswordExpired bool     `json:"passwordExpired"`
	AccountExpired  bool     `json:"accountExpired"`
	Groups          []string `json:"groups"`
	Version         string   `json:"version"`
}
type Group struct {
	ID          string   `json:"-"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	GID         string   `json:"gid"`
	Members     []string `json:"members"`
	Version     string   `json:"version"`
}
type Change struct {
	Action      string `json:"action"`
	Target      string `json:"target"`
	Member      string `json:"member"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Email       string `json:"email"`
	Description string `json:"description"`
}
type Provider interface {
	Info() Info
	User(context.Context, string) (User, error)
	Group(context.Context, string) (Group, error)
	Users(context.Context, string) ([]User, error)
	Groups(context.Context, string) ([]Group, error)
	Authenticate(context.Context, string, string, string) error
	Change(context.Context, Change) error
	ProtectedUser(string) bool
}
type Client struct {
	config   Config
	password string
	http     *http.Client
	mu       sync.Mutex
	cookies  []*http.Cookie
	until    time.Time
	slots    chan struct{}
}

func restrictedFile(path string, limit int64, secret bool) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrUnavailable
	}
	s, e := os.Lstat(path)
	if e != nil || !s.Mode().IsRegular() || s.Mode().Perm()&0022 != 0 || secret && s.Mode().Perm()&0077 != 0 {
		return nil, ErrUnavailable
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, ErrUnavailable
	}
	return b, nil
}
func Load(path string) (*Client, error) {
	b, e := restrictedFile(path, 65536, false)
	if e != nil {
		return nil, errors.New("cannot read protected FreeIPA configuration")
	}
	var c Config
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid FreeIPA configuration")
	}
	return New(c)
}
func New(c Config) (*Client, error) {
	u, e := url.Parse(c.URL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || !Name.MatchString(c.Username) || c.Domain == "" || c.Realm == "" {
		return nil, errors.New("FreeIPA requires an HTTPS origin, domain, realm and service username")
	}
	secret, e := restrictedFile(c.PasswordFile, 4096, true)
	if e != nil {
		return nil, errors.New("cannot read private FreeIPA service password")
	}
	defer clear(secret)
	password := strings.TrimSuffix(strings.TrimSuffix(string(secret), "\n"), "\r")
	if password == "" || strings.ContainsRune(password, 0) {
		return nil, errors.New("empty or invalid FreeIPA service password")
	}
	pem, e := restrictedFile(c.CAFile, 1048576, false)
	if e != nil {
		return nil, errors.New("cannot read FreeIPA CA")
	}
	pool, e := x509.SystemCertPool()
	if e != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("invalid FreeIPA CA")
	}
	c.URL = strings.TrimSuffix(c.URL, "/")
	return &Client{config: c, password: password, slots: make(chan struct{}, 8), http: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 8, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Info() Info { return Info{true, c.config.Domain, c.config.Realm} }
func (c *Client) ProtectedUser(name string) bool {
	return name == "admin" || name == "root" || name == c.config.Username
}
func (c *Client) request(ctx context.Context, path, kind string, b []byte, cookies []*http.Cookie) (*http.Response, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return nil, ErrUnavailable
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.config.URL+path, bytes.NewReader(b))
	if e != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Content-Type", kind)
	req.Header.Set("Referer", c.config.URL+"/ipa")
	req.Header.Set("Accept", "application/json")
	for _, v := range cookies {
		req.AddCookie(v)
	}
	r, e := c.http.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	return r, nil
}
func (c *Client) login(ctx context.Context, user, password string) ([]*http.Cookie, error) {
	b := []byte(url.Values{"user": {user}, "password": {password}}.Encode())
	defer clear(b)
	r, e := c.request(ctx, "/ipa/session/login_password", "application/x-www-form-urlencoded", b, nil)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 65536))
	if r.StatusCode == 401 {
		return nil, ErrDenied
	}
	if r.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	for _, cookie := range r.Cookies() {
		if cookie.Name == "ipa_session" && cookie.Value != "" {
			return []*http.Cookie{cookie}, nil
		}
	}
	return nil, ErrUnavailable
}
func (c *Client) session(ctx context.Context) ([]*http.Cookie, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.until) && len(c.cookies) > 0 {
		return c.cookies, nil
	}
	v, e := c.login(ctx, c.config.Username, c.password)
	if e != nil {
		return nil, ErrUnavailable
	}
	c.cookies = v
	c.until = time.Now().Add(time.Minute)
	return v, nil
}

type entry map[string]json.RawMessage

func values(e entry, k string) []string {
	var a []string
	if json.Unmarshal(e[k], &a) == nil {
		return a
	}
	var s string
	if json.Unmarshal(e[k], &s) == nil {
		return []string{s}
	}
	return []string{}
}
func first(e entry, k string) string {
	v := values(e, k)
	if len(v) > 0 {
		return v[0]
	}
	return ""
}
func accountLocked(e entry) (bool, error) {
	raw, ok := e["nsaccountlock"]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false, ErrUnavailable
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b, nil
	}
	vals := values(e, "nsaccountlock")
	if len(vals) != 1 {
		return false, ErrUnavailable
	}
	switch strings.ToLower(vals[0]) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, ErrUnavailable
	}
}
func expired(e entry, k string) (bool, error) {
	s := first(e, k)
	if s == "" && len(e[k]) > 0 {
		var dates []map[string]string
		if json.Unmarshal(e[k], &dates) != nil || len(dates) != 1 {
			return false, ErrUnavailable
		}
		s = dates[0]["__datetime__"]
		if s == "" {
			return false, ErrUnavailable
		}
	}
	if s == "" {
		return false, nil
	}
	for _, layout := range []string{"20060102150405Z", time.RFC3339} {
		if v, err := time.Parse(layout, s); err == nil {
			return !v.After(time.Now()), nil
		}
	}
	return false, ErrUnavailable
}
func version(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func userView(e entry) (User, error) {
	locked, lockErr := accountLocked(e)
	if lockErr != nil {
		return User{}, lockErr
	}
	u := User{ID: first(e, "ipauniqueid"), Username: first(e, "uid"), FirstName: first(e, "givenname"), LastName: first(e, "sn"), Email: first(e, "mail"), UID: first(e, "uidnumber"), GID: first(e, "gidnumber"), Enabled: !locked, Groups: values(e, "memberof_group")}
	if u.ID == "" || !Name.MatchString(u.Username) {
		return u, ErrUnavailable
	}
	sort.Strings(u.Groups)
	var err error
	u.PasswordExpired, err = expired(e, "krbpasswordexpiration")
	if err != nil {
		return u, err
	}
	u.AccountExpired, err = expired(e, "krbprincipalexpiration")
	if err != nil {
		return u, err
	}
	u.Version = version([]any{u.ID, u})
	return u, nil
}
func groupView(e entry) (Group, error) {
	g := Group{ID: first(e, "ipauniqueid"), Name: first(e, "cn"), Description: first(e, "description"), GID: first(e, "gidnumber"), Members: values(e, "member_user")}
	if g.ID == "" || !GroupName.MatchString(g.Name) {
		return g, ErrUnavailable
	}
	sort.Strings(g.Members)
	g.Version = version([]any{g.ID, g})
	return g, nil
}
func (c *Client) rpc(ctx context.Context, method string, args []string, options map[string]any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cookies, err := c.session(ctx)
	if err != nil {
		return nil, err
	}
	if options == nil {
		options = map[string]any{}
	}
	options["version"] = "2.251"
	b, _ := json.Marshal(map[string]any{"method": method, "params": []any{args, options}, "id": 1})
	r, err := c.request(ctx, "/ipa/session/json", "application/json", b, cookies)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode == 401 {
		c.mu.Lock()
		c.until = time.Time{}
		c.mu.Unlock()
		return nil, ErrUnavailable
	}
	if r.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	b, err = io.ReadAll(io.LimitReader(r.Body, 8*1024*1024+1))
	if err != nil || len(b) > 8*1024*1024 {
		return nil, ErrUnavailable
	}
	var body struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &body) != nil {
		return nil, ErrUnavailable
	}
	if body.Error != nil {
		if body.Error.Code == 4001 {
			return nil, ErrNotFound
		}
		return nil, ErrRejected
	}
	if len(body.Result) == 0 || string(body.Result) == "null" {
		return nil, ErrUnavailable
	}
	return body.Result, nil
}
func (c *Client) show(ctx context.Context, method, name string) (entry, error) {
	if method == "user_show" && !Name.MatchString(name) || method == "group_show" && !GroupName.MatchString(name) {
		return nil, ErrDenied
	}
	b, e := c.rpc(ctx, method, []string{name}, map[string]any{"all": true, "raw": false})
	if e != nil {
		return nil, e
	}
	var r struct {
		Result entry `json:"result"`
	}
	if json.Unmarshal(b, &r) != nil || r.Result == nil {
		return nil, ErrUnavailable
	}
	return r.Result, nil
}
func (c *Client) User(ctx context.Context, name string) (User, error) {
	e, err := c.show(ctx, "user_show", name)
	if err != nil {
		return User{}, err
	}
	u, err := userView(e)
	if err == nil && u.Username != name {
		return User{}, ErrUnavailable
	}
	return u, err
}
func (c *Client) Group(ctx context.Context, name string) (Group, error) {
	e, err := c.show(ctx, "group_show", name)
	if err != nil {
		return Group{}, err
	}
	g, err := groupView(e)
	if err == nil && g.Name != name {
		return Group{}, ErrUnavailable
	}
	return g, err
}
func (c *Client) find(ctx context.Context, method, q string) ([]entry, error) {
	b, e := c.rpc(ctx, method, []string{q}, map[string]any{"all": true, "sizelimit": 1000, "timelimit": 5})
	if e != nil {
		return nil, e
	}
	var r struct {
		Result    []entry `json:"result"`
		Truncated bool    `json:"truncated"`
	}
	if json.Unmarshal(b, &r) != nil || r.Truncated {
		return nil, ErrUnavailable
	}
	return r.Result, nil
}
func (c *Client) Users(ctx context.Context, q string) ([]User, error) {
	v, e := c.find(ctx, "user_find", q)
	if e != nil {
		return nil, e
	}
	r := []User{}
	for _, x := range v {
		u, e := userView(x)
		if e != nil {
			return nil, e
		}
		r = append(r, u)
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Username < r[j].Username })
	return r, nil
}
func (c *Client) Groups(ctx context.Context, q string) ([]Group, error) {
	v, e := c.find(ctx, "group_find", q)
	if e != nil {
		return nil, e
	}
	r := []Group{}
	for _, x := range v {
		g, e := groupView(x)
		if e != nil {
			return nil, e
		}
		r = append(r, g)
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Name < r[j].Name })
	return r, nil
}
func (c *Client) Authenticate(ctx context.Context, name, id, password string) error {
	if !Name.MatchString(name) || id == "" || password == "" || len(password) > 4096 || strings.ContainsRune(password, 0) {
		return ErrDenied
	}
	u, e := c.User(ctx, name)
	if e != nil {
		return e
	}
	if u.ID != id || !u.Enabled || u.AccountExpired || u.PasswordExpired {
		return ErrDenied
	}
	cookies, e := c.login(ctx, name, password)
	if e != nil {
		return e
	}
	_ = cookies
	u, e = c.User(ctx, name)
	if e != nil {
		return e
	}
	if u.ID != id || !u.Enabled || u.AccountExpired || u.PasswordExpired {
		return ErrDenied
	}
	return nil
}
func (c *Client) Change(ctx context.Context, in Change) error {
	if !Name.MatchString(in.Target) || strings.HasPrefix(in.Action, "group.") && (in.Target == "admins" || in.Target == "editors" || in.Target == "ipausers") {
		return ErrDenied
	}
	var method string
	opts := map[string]any{}
	switch in.Action {
	case "user.create":
		method = "user_add"
		opts["givenname"] = in.FirstName
		opts["sn"] = in.LastName
		if in.Email != "" {
			opts["mail"] = []string{in.Email}
		}
	case "user.enable":
		method = "user_enable"
	case "user.disable":
		method = "user_disable"
	case "group.create":
		method = "group_add"
		if in.Description != "" {
			opts["description"] = in.Description
		}
	case "group.add-member":
		method = "group_add_member"
		opts["user"] = []string{in.Member}
	case "group.remove-member":
		method = "group_remove_member"
		opts["user"] = []string{in.Member}
	default:
		return ErrDenied
	}
	if strings.HasPrefix(in.Action, "user.") && c.ProtectedUser(in.Target) || strings.Contains(in.Action, "member") && (!Name.MatchString(in.Member) || c.ProtectedUser(in.Member)) {
		return ErrDenied
	}
	b, e := c.rpc(ctx, method, []string{in.Target}, opts)
	if e != nil {
		return e
	}
	if strings.Contains(in.Action, "member") {
		var r struct {
			Completed int `json:"completed"`
		}
		if json.Unmarshal(b, &r) != nil || r.Completed != 1 {
			return ErrRejected
		}
	}
	return nil
}
