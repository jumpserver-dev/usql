package mongoshell

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyConnectAndBufferedBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "mongo.test:27017" {
			t.Error("incorrect proxy target")
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\nhello")
		_, _ = io.Copy(conn, conn)
	}))
	defer server.Close()
	dialer, err := newProxyDialer(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", "mongo.test:27017")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	data := make([]byte, 5)
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != "hello" {
		t.Fatalf("CONNECT discarded buffered tunnel bytes: %q, %v", data, err)
	}
	for _, value := range []string{"https://proxy.test", "http://user:password@proxy.test", "http://proxy.test/path"} {
		if _, err := newProxyDialer(value); err == nil {
			t.Fatal("invalid HTTP proxy accepted")
		}
	}
}

func TestProxyHandshakeCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadByte()
	}))
	defer server.Close()
	dialer, err := newProxyDialer(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if conn, err := dialer.DialContext(ctx, "tcp", "mongo.test:27017"); err == nil {
		conn.Close()
		t.Fatal("cancelled CONNECT unexpectedly succeeded")
	}
}
