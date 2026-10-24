package ldapauth

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

var ErrDenied = errors.New("LDAP authentication denied")
var ErrUnavailable = errors.New("LDAP service unavailable")

type Identity struct {
	Username string
	ID       string
}
type Provider interface {
	Lookup(context.Context, string) (Identity, error)
	Authenticate(context.Context, string, string, string) error
	Check(context.Context, string, string) error
	Probe(context.Context) error
}
type Client struct {
	config                   Config
	host, port, bindPassword string
	ldaps                    bool
	tls                      *tls.Config
	slots                    chan struct{}
}

func (c *Client) connect(ctx context.Context, work func(*ldap.Conn) error) error {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(c.host, c.port))
	if err != nil {
		return ErrUnavailable
	}
	deadline, _ := ctx.Deadline()
	_ = raw.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	var transport net.Conn = raw
	if c.ldaps {
		encrypted := tls.Client(raw, c.tls)
		if err := encrypted.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return ErrUnavailable
		}
		transport = encrypted
	}
	conn := ldap.NewConn(transport, c.ldaps)
	conn.SetTimeout(3 * time.Second)
	conn.Start()
	defer conn.Close()
	// No service or user credentials are sent until verified TLS is established.
	if !c.ldaps {
		if err := conn.StartTLS(c.tls); err != nil {
			return ErrUnavailable
		}
	}
	if err := conn.Bind(c.config.BindDN, c.bindPassword); err != nil {
		return ErrUnavailable
	}
	return work(conn)
}

func (c *Client) find(conn *ldap.Conn, username string) (*ldap.Entry, Identity, error) {
	if username == "" || len(username) > 255 || strings.ContainsAny(username, "\x00\r\n") {
		return nil, Identity{}, ErrDenied
	}
	filter := strings.ReplaceAll(c.config.UserFilter, "{username}", ldap.EscapeFilter(username))
	result, err := conn.Search(ldap.NewSearchRequest(c.config.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 3, false, filter, []string{c.config.UsernameAttribute, c.config.IDAttribute}, nil))
	if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return nil, Identity{}, ErrDenied
	}
	if err != nil {
		return nil, Identity{}, ErrUnavailable
	}
	if len(result.Entries) != 1 {
		return nil, Identity{}, ErrDenied
	}
	entry := result.Entries[0]
	names := entry.GetAttributeValues(c.config.UsernameAttribute)
	ids := entry.GetRawAttributeValues(c.config.IDAttribute)
	if len(names) != 1 || names[0] != username || len(ids) != 1 || len(ids[0]) == 0 || len(ids[0]) > 1024 {
		return nil, Identity{}, ErrDenied
	}
	return entry, Identity{Username: names[0], ID: base64.RawStdEncoding.EncodeToString(ids[0])}, nil
}

func (c *Client) Lookup(ctx context.Context, username string) (Identity, error) {
	var identity Identity
	err := c.connect(ctx, func(conn *ldap.Conn) error { _, found, err := c.find(conn, username); identity = found; return err })
	return identity, err
}

func (c *Client) Authenticate(ctx context.Context, username, id, password string) error {
	if password == "" || strings.ContainsRune(password, 0) || id == "" {
		return ErrDenied
	}
	return c.connect(ctx, func(conn *ldap.Conn) error {
		entry, found, err := c.find(conn, username)
		if err != nil {
			return err
		}
		if found.ID != id {
			return ErrDenied
		}
		result, err := conn.SimpleBind(ldap.NewSimpleBindRequest(entry.DN, password, []ldap.Control{ldap.NewControlBeheraPasswordPolicy()}))
		if err != nil {
			if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) || ldap.IsErrorWithCode(err, ldap.LDAPResultConstraintViolation) || ldap.IsErrorWithCode(err, ldap.LDAPResultUnwillingToPerform) {
				return ErrDenied
			}
			return ErrUnavailable
		}
		if policy, ok := ldap.FindControl(result.Controls, ldap.ControlTypeBeheraPasswordPolicy).(*ldap.ControlBeheraPasswordPolicy); ok && policy.Error >= 0 {
			return ErrDenied
		}
		return nil
	})
}

func (c *Client) Check(ctx context.Context, username, id string) error {
	if id == "" {
		return ErrDenied
	}
	return c.connect(ctx, func(conn *ldap.Conn) error {
		_, found, err := c.find(conn, username)
		if err != nil {
			return err
		}
		if found.ID != id {
			return ErrDenied
		}
		return nil
	})
}

func (c *Client) Probe(ctx context.Context) error {
	return c.connect(ctx, func(conn *ldap.Conn) error {
		result, err := conn.Search(ldap.NewSearchRequest(c.config.BaseDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 3, false, "(objectClass=*)", []string{"1.1"}, nil))
		if err != nil || len(result.Entries) != 1 {
			return ErrUnavailable
		}
		return nil
	})
}
