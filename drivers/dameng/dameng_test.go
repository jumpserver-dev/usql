package dameng

import (
	"testing"

	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

func TestDamengRegistrationAndURL(t *testing.T) {
	parsed, err := dburl.Parse("dameng://user:p%40ss@127.0.0.1:5525/APP?loginEncrypt=false")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Driver != "dameng" || parsed.GoDriver != "dm" {
		t.Fatalf("unexpected driver mapping: driver=%q go_driver=%q", parsed.Driver, parsed.GoDriver)
	}
	if parsed.DSN != "dm://user:p%40ss@127.0.0.1:5525/APP?loginEncrypt=false" {
		t.Fatalf("unexpected Dameng DSN: %q", parsed.DSN)
	}
	if !drivers.Registered("dameng") {
		t.Fatal("Dameng usql driver was not registered")
	}
}

func TestDamengAlias(t *testing.T) {
	parsed, err := dburl.Parse("dm://user:password@db.example:5236")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Driver != "dameng" || parsed.GoDriver != "dm" {
		t.Fatalf("unexpected alias mapping: driver=%q go_driver=%q", parsed.Driver, parsed.GoDriver)
	}
}
