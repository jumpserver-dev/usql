package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteAvailableDriversIncludesDameng(t *testing.T) {
	var output bytes.Buffer
	if err := writeAvailableDrivers(&output); err != nil {
		t.Fatalf("writeAvailableDrivers() error = %v", err)
	}

	drivers := strings.Fields(output.String())
	for _, name := range drivers {
		if name == "dameng" {
			return
		}
	}
	t.Fatalf("available drivers %q do not include dameng", drivers)
}
