package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxExtractBytes = 512 << 20

// extractTar extracts a .tar.gz, .tgz or .tar.bz2 into dest, dropping the
// first strip path components.
func extractTar(archive, dest string, strip int) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader
	if strings.HasSuffix(archive, ".tar.bz2") {
		r = bzip2.NewReader(f)
	} else {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}
	tr := tar.NewReader(r)
	var written int64
	var links []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return checkLinks(archive, dest, links)
		}
		if err != nil {
			return err
		}
		rel, ok, err := stripName(hdr.Name, strip)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		target, err := joinUnder(dest, rel)
		if err != nil {
			return err
		}
		if err := noSymlinkParents(dest, rel); err != nil {
			return fmt.Errorf("%s: %w", archive, err)
		}
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			// Go's reader surfaces the pax global header. It carries archive
			// metadata, not a file to install.
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			n, err := writeTarFile(tr, target, hdr, maxExtractBytes-written)
			if err != nil {
				return err
			}
			written += n
			if written > maxExtractBytes {
				return fmt.Errorf("%s expands past %d bytes", archive, maxExtractBytes)
			}
		case tar.TypeSymlink:
			if err := writeTarLink(target, rel, hdr.Linkname); err != nil {
				return fmt.Errorf("%s: %w", archive, err)
			}
			links = append(links, rel)
		default:
			return fmt.Errorf("%s: unsupported entry %q type %d", archive, hdr.Name, hdr.Typeflag)
		}
	}
}

func writeTarFile(r io.Reader, target string, hdr *tar.Header, budget int64) (int64, error) {
	if hdr.Size < 0 || hdr.Size > budget {
		return 0, fmt.Errorf("%s is too large", hdr.Name)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	mode := os.FileMode(hdr.Mode) & 0o777
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(out, io.LimitReader(r, hdr.Size))
	closeErr := out.Close()
	if copyErr != nil {
		return n, copyErr
	}
	if closeErr != nil {
		return n, closeErr
	}
	if !hdr.ModTime.IsZero() {
		_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)
	}
	return n, nil
}

func writeTarLink(target, rel, linkname string) error {
	if path.IsAbs(linkname) || filepath.IsAbs(linkname) {
		return fmt.Errorf("absolute symlink %s", rel)
	}
	resolved := path.Clean(path.Join(path.Dir(rel), linkname))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("symlink %s escapes via %s", rel, linkname)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(linkname, target)
}

// noSymlinkParents fails when a directory on rel's path under dest is a
// symlink, so no entry is written through a link the archive made.
func noSymlinkParents(dest, rel string) error {
	parts := strings.Split(rel, "/")
	cur := dest
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("entry %s goes through the symlink %s", rel, filepath.ToSlash(strings.TrimPrefix(cur, dest+string(filepath.Separator))))
		}
	}
	return nil
}

// checkLinks resolves each extracted link on disk, following links, since
// a link added later can make an earlier one point outside dest.
func checkLinks(archive, dest string, links []string) error {
	for _, rel := range links {
		if err := resolveInside(dest, rel); err != nil {
			return fmt.Errorf("%s: symlink %s: %w", archive, rel, err)
		}
	}
	return nil
}

// resolveInside follows rel under root one component at a time, through
// symlinks as the OS would, and fails if it leaves root. A missing component
// ends the physical walk; the rest is resolved by name.
func resolveInside(root, rel string) error {
	var stack []string
	queue := strings.Split(filepath.ToSlash(rel), "/")
	hops := 0
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			if len(stack) == 0 {
				return fmt.Errorf("resolves outside %s", root)
			}
			stack = stack[:len(stack)-1]
			continue
		}
		p := filepath.Join(append([]string{root}, append(stack, c)...)...)
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			stack = append(stack, c)
			continue
		}
		if hops++; hops > 40 {
			return fmt.Errorf("too many levels of symlinks")
		}
		target, err := os.Readlink(p)
		if err != nil {
			return err
		}
		if path.IsAbs(target) || filepath.IsAbs(target) {
			return fmt.Errorf("points at the absolute path %s", target)
		}
		queue = append(strings.Split(filepath.ToSlash(target), "/"), queue...)
	}
	return nil
}

// extractNinja installs the ninja binary, the only member of every official
// Ninja zip (the macOS one is universal).
func extractNinja(archive, binDir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	if len(r.File) != 1 || r.File[0].Name != "ninja" {
		return fmt.Errorf("%s: expected a single ninja file", archive)
	}
	if r.File[0].UncompressedSize64 > maxExtractBytes {
		return fmt.Errorf("%s: ninja is too large", archive)
	}
	rc, err := r.File[0].Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	return copyToMode(rc, filepath.Join(binDir, "ninja"), 0o755)
}

func stripName(name string, strip int) (string, bool, error) {
	cleaned := path.Clean(name)
	if cleaned == "." {
		return "", false, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false, fmt.Errorf("path escapes: %s", name)
	}
	parts := strings.Split(cleaned, "/")
	if len(parts) <= strip {
		return "", false, nil
	}
	rel := path.Join(parts[strip:]...)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false, fmt.Errorf("path escapes: %s", name)
	}
	return rel, true, nil
}

func joinUnder(root, rel string) (string, error) {
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path %s", rel)
	}
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes: %s", rel)
	}
	return filepath.Join(root, cleaned), nil
}

func mergeTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// The link must stay inside src, or it would point outside dst.
			if err := resolveInside(src, rel); err != nil {
				return fmt.Errorf("symlink %s: %w", rel, err)
			}
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return err
			}
			return os.Symlink(link, target)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return copyToMode(in, target, mode)
	})
}

func copyToMode(r io.Reader, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, r)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return copyToMode(in, dst, mode)
}
