package mongoshell

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestConnectionAndDispatch(t *testing.T) {
	for _, args := range [][]string{{"--mongo", "--nodb"}, {"mongo", "--eval", "1+1"}, {"mongodb://localhost/test"}, {"mongodb+srv://example.test/test"}} {
		if !IsInvocation(args) {
			t.Fatal(args)
		}
	}
	if IsInvocation([]string{"postgres://localhost/test"}) {
		t.Fatal("SQL invocation was redirected")
	}
	o := cliOptions{username: "user@tenant", password: "p@ss:/?#%", authDB: "admin", tls: true, caFile: "/tmp/ca.pem"}
	uri, files, err := o.connection([]string{"mongodb://[::1]:27017/test", "task.js"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if u.User.Username() != o.username || password != o.password || u.Host != "[::1]:27017" || u.Query().Get("authSource") != "admin" || u.Query().Get("tlsCAFile") != "/tmp/ca.pem" || len(files) != 1 {
		t.Fatal("connection options were not preserved")
	}
	if _, _, err := (&cliOptions{noPassword: true}).connection([]string{"mongodb://host/test", "mongodb://other/test"}); err == nil {
		t.Fatal("duplicate endpoints accepted")
	}
	if err := New([]string{"--mongo", "--nodb", "--norc", "--eval", `assert.eq(2, 1+1)`}, "test").ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestIncompleteAndErrors(t *testing.T) {
	for _, source := range []string{"db.items.find({", "function test() {", "db.items.find()."} {
		if !incomplete(source) {
			t.Errorf("not treated as incomplete: %s", source)
		}
	}
	for _, source := range []string{"use test", "show collections", "db.items.find()", "var = invalid"} {
		if incomplete(source) {
			t.Errorf("incorrect continuation: %s", source)
		}
	}
	err := New([]string{"--mongo", "--nodb", "--norc", "--eval", `throw new Error("expected failure")`}, "test").ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "expected failure") {
		t.Fatal(err)
	}
}
