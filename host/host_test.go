package host

import (
	"strings"
	"testing"
)

func TestDetectHost(t *testing.T) {
	cases := map[[2]string]string{
		{"darwin", "arm64"}: "macos-aarch64",
		{"darwin", "amd64"}: "macos-x86_64",
		{"linux", "amd64"}:  "linux-x86_64",
		{"linux", "arm64"}:  "linux-aarch64",
	}
	for in, want := range cases {
		got, err := Detect(in[0], in[1])
		if err != nil || got != want {
			t.Errorf("Detect(%q, %q) = %q, %v; want %q", in[0], in[1], got, err, want)
		}
	}
}

func TestDetectHostUnsupported(t *testing.T) {
	for _, in := range [][2]string{{"windows", "amd64"}, {"Darwin", "arm64"}, {"linux", "x86_64"}} {
		_, err := Detect(in[0], in[1])
		if err == nil || !strings.Contains(err.Error(), in[0]) || !strings.Contains(err.Error(), in[1]) {
			t.Fatalf("Detect(%q, %q) err %v", in[0], in[1], err)
		}
	}
}
