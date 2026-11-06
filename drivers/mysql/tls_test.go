package mysql

import (
	"crypto/tls"
	"net/url"
	"testing"
)

func TestCustomTLSVerifiesByDefaultAndKeepsGatewayIdentity(t *testing.T) {
	q := url.Values{"ssl-server-name": {"database.example"}}
	cfg, err := connectionTLS(q, "127.0.0.1")
	if err != nil || cfg.InsecureSkipVerify || cfg.ServerName != "database.example" || cfg.MinVersion != tls.VersionTLS12 {
		t.Fatal("custom TLS must verify the logical database identity, not the SSH endpoint", err)
	}
	q.Set("ssl-insecure", "true")
	cfg, err = connectionTLS(q, "127.0.0.1")
	if err != nil || !cfg.InsecureSkipVerify {
		t.Fatal("explicit certificate-verification opt-out was not preserved", err)
	}
	q.Set("ssl-insecure", "typo")
	if _, err := connectionTLS(q, "host"); err == nil {
		t.Fatal("invalid TLS option accepted")
	}
	q = url.Values{"ssl-cert": {"client.pem"}}
	if _, err := connectionTLS(q, "host"); err == nil {
		t.Fatal("unpaired client certificate accepted")
	}
	q = url.Values{"ssl-ca": {"/nonexistent/ca.pem"}}
	if _, err := connectionTLS(q, "host"); err == nil {
		t.Fatal("unreadable CA ignored")
	}
}
