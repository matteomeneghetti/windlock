package tools

import (
	"reflect"
	"testing"
)

func TestDTCMakeArgsSkipYAML(t *testing.T) {
	got := dtcMakeArgs(4, "/cache/build-tools/bin")
	want := []string{
		"-j4",
		"dtc",
		"LEX=/cache/build-tools/bin/flex",
		"BISON=/cache/build-tools/bin/bison",
		"NO_YAML=1",
		"PKG_CONFIG=/usr/bin/false",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("make args %q, want %q", got, want)
	}
}
