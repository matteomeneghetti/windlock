package setup

import (
	"strings"
	"testing"
)

func TestSelectMinimalBundle(t *testing.T) {
	const sum = `
ca9bc0ff66fafca1dac9d592a36d953cf16d096a9d09b1c0357f021cf9f6a7eb  zephyr-sdk-1.0.1_linux-x86_64_minimal.tar.xz
867063901f39528a6175a80ebc20367bd6cb440593e7e2650eda30392f1f6b65  zephyr-sdk-1.0.1_macos-aarch64_minimal.tar.xz
`
	file, hash, err := selectMinimalBundle(sum, "1.0.1", "linux-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if file != "zephyr-sdk-1.0.1_linux-x86_64_minimal.tar.xz" {
		t.Fatalf("file %s", file)
	}
	if hash != "ca9bc0ff66fafca1dac9d592a36d953cf16d096a9d09b1c0357f021cf9f6a7eb" {
		t.Fatalf("hash %s", hash)
	}
	_, _, err = selectMinimalBundle(sum, "1.0.1", "macos-x86_64")
	if err == nil {
		t.Fatal("missing bundle was accepted")
	}
	if !strings.Contains(err.Error(), "macos-x86_64") || !strings.Contains(err.Error(), "1.0.1") {
		t.Fatalf("error %q", err)
	}
}

func TestSelectMinimalBundleWindowsIs7z(t *testing.T) {
	file, hash, err := selectMinimalBundle(sdk101Sum, "1.0.1", "windows-x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if file != "zephyr-sdk-1.0.1_windows-x86_64_minimal.7z" || hash != "17a557e7c6cea5e5c589508fe45a3f26aca4fa2c6153fe3e7b03aeb4b93fea4b" {
		t.Fatalf("windows bundle %s %s", file, hash)
	}
	bad := "zz  zephyr-sdk-1.0.1_macos-aarch64_minimal.tar.xz\n" + sdk101Sum
	if _, _, err := selectMinimalBundle(bad, "1.0.1", "macos-aarch64"); err == nil {
		t.Fatal("a malformed row was accepted")
	}
}

func TestCommitFromLSRemote(t *testing.T) {
	const out = "" +
		"671f64aa79924606253238f801c494c84b02c2a0\trefs/tags/v4.4.2\n" +
		"dccb09599635bdff17633fa7e9dab014b91dce90\trefs/tags/v4.4.2^{}\n" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/heads/v4.4.2\n" +
		"cccccccccccccccccccccccccccccccccccccccc\trefs/tags/x/v4.4.2^{}\n"
	got, err := commitFromLSRemote(out, "v4.4.2")
	if err != nil || got != "dccb09599635bdff17633fa7e9dab014b91dce90" {
		t.Fatalf("peeled tag %s err %v", got, err)
	}
	got, err = commitFromLSRemote("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\trefs/tags/v1\n", "v1")
	if err != nil || got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("lightweight tag %s err %v", got, err)
	}
	got, err = commitFromLSRemote("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/heads/main\n", "main")
	if err != nil || got != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("branch %s err %v", got, err)
	}
	if _, err := commitFromLSRemote(out, "v4.4"); err == nil {
		t.Fatal("a ref that only ends in the name was accepted")
	}
}

func TestParseWestVersion(t *testing.T) {
	got, err := parseWestVersion("West version: v1.2.0\n")
	if err != nil || got != "1.2.0" {
		t.Fatalf("got %s err %v", got, err)
	}
	got, err = parseWestVersion("West version: 0.14.0\n")
	if err != nil || got != "0.14.0" {
		t.Fatalf("got %s err %v", got, err)
	}
}

func TestIsRateLimit(t *testing.T) {
	if !isRateLimit("HTTP Error 403: rate limit exceeded") {
		t.Fatal("expected rate limit")
	}
	if isRateLimit("clone succeeded") {
		t.Fatal("false rate limit")
	}
}
