package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxArchiveBytes = 128 << 20

var httpClient = &http.Client{
	Timeout: 10 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-https redirect to %s", req.URL)
		}
		if len(via) > 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

func cachePath(root, sum, filename string) string {
	return filepath.Join(root, "cache", "downloads", sum, filename)
}

// ensureArchive returns the cached file. A file already in the cache whose
// sha256 matches sum is returned without a download.
func ensureArchive(root, rawURL, sum, filename string) (string, bool, error) {
	if err := requireHTTPS(rawURL); err != nil {
		return "", false, err
	}
	sum = strings.ToLower(strings.TrimSpace(sum))
	dest := cachePath(root, sum, filename)
	match, err := fileSHA256(dest, sum)
	if err != nil {
		return "", false, err
	}
	if match {
		return dest, true, nil
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", false, err
	}
	partial := dest + ".partial"
	_ = os.Remove(partial)
	if err := download(rawURL, partial, sum); err != nil {
		_ = os.Remove(partial)
		return "", false, err
	}
	if err := os.Rename(partial, dest); err != nil {
		return "", false, err
	}
	return dest, false, nil
}

func requireHTTPS(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return fmt.Errorf("refusing non-https URL %s", rawURL)
	}
	return nil
}

func fileSHA256(path, want string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == want, nil
}

func download(rawURL, dest, want string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "windlock")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	if resp.ContentLength > maxArchiveBytes {
		return fmt.Errorf("%s is %d bytes", rawURL, resp.ContentLength)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return err
	}
	if n > maxArchiveBytes {
		return fmt.Errorf("%s exceeds %d bytes", rawURL, maxArchiveBytes)
	}
	if err := f.Close(); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("%s sha256 %s, want %s", rawURL, got, want)
	}
	return nil
}

// FetchText downloads a small text file over HTTPS, at most limit bytes.
func FetchText(rawURL string, limit int64) (string, error) {
	if err := requireHTTPS(rawURL); err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "windlock")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > limit {
		return "", fmt.Errorf("%s exceeds %d bytes", rawURL, limit)
	}
	return string(b), nil
}
