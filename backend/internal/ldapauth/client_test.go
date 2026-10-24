package ldapauth

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
)

func configFixture(t *testing.T) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bind-secret")
	if err := os.WriteFile(path, []byte(rand.Text()), 0600); err != nil {
		t.Fatal(err)
	}
	return Config{URL: "ldaps://localhost:636", BaseDN: "ou=people,dc=example,dc=com", BindDN: "cn=reader,dc=example,dc=com", BindPasswordFile: path}
}

func TestConfigurationRejectsUnsafeLDAPInputs(t *testing.T) {
	for _, sample := range []struct {
		name   string
		change func(*Config)
	}{
		{"cleartext-scheme", func(c *Config) { c.URL = "http://localhost" }},
		{"credentials-in-url", func(c *Config) { c.URL = "ldaps://user:fixture@localhost" }},
		{"ambiguous-base", func(c *Config) { c.BaseDN = "not a DN" }},
		{"missing-service-identity", func(c *Config) { c.BindDN = "" }},
		{"missing-placeholder", func(c *Config) { c.UserFilter = "(uid=*)" }},
		{"invalid-filter", func(c *Config) { c.UserFilter = "(uid={username}" }},
		{"unsafe-attribute", func(c *Config) { c.IDAttribute = "entryUUID)(uid=*" }},
		{"readable-secret", func(c *Config) { _ = os.Chmod(c.BindPasswordFile, 0644) }},
		{"empty-secret", func(c *Config) { _ = os.WriteFile(c.BindPasswordFile, nil, 0600) }},
	} {
		t.Run(sample.name, func(t *testing.T) {
			config := configFixture(t)
			sample.change(&config)
			if _, err := New(config); err == nil {
				t.Fatal("unsafe LDAP configuration accepted")
			}
		})
	}
}

func TestTLSVerificationRejectsUntrustedCertificate(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("LDAP credentials reached an HTTP handler") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	config := configFixture(t)
	config.URL = "ldaps://" + strings.TrimPrefix(server.URL, "https://")
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Probe(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("untrusted TLS accepted: %v", err)
	}
}

func TestStartTLSFailureNeverSendsBindCredentials(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	observed := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			observed <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		packet, err := ber.ReadPacket(conn)
		if err != nil {
			observed <- err
			return
		}
		if packet.Children[1].Tag != ldap.ApplicationExtendedRequest || packet.Children[1].Children[0].Data.String() != "1.3.6.1.4.1.1466.20037" {
			observed <- errors.New("credentials sent before StartTLS")
			return
		}
		response := ber.NewSequence("LDAP message")
		response.AppendChild(packet.Children[0])
		result := ber.Encode(ber.ClassApplication, ber.TypeConstructed, ldap.ApplicationExtendedResponse, nil, "StartTLS failure")
		result.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, ldap.LDAPResultUnavailable, "resultCode"))
		result.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "matchedDN"))
		result.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "TLS unavailable", "diagnosticMessage"))
		response.AppendChild(result)
		if _, err := conn.Write(response.Bytes()); err != nil {
			observed <- err
			return
		}
		var data [1]byte
		n, _ := conn.Read(data[:])
		if n != 0 {
			observed <- errors.New("bind sent after failed StartTLS")
			return
		}
		observed <- nil
	}()
	config := configFixture(t)
	config.URL = "ldap://" + listener.Addr().String()
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Probe(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("StartTLS failure accepted: %v", err)
	}
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
}

func TestContextCancellationBoundsTLSHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = io.Copy(io.Discard, conn)
	}()
	config := configFixture(t)
	config.URL = "ldaps://" + listener.Addr().String()
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := client.Probe(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cancelled handshake accepted")
	}
	if time.Since(started) > time.Second {
		t.Fatal("TLS handshake ignored request cancellation")
	}
	<-done
}
