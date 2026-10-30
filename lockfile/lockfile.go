// Package lockfile reads and writes windlock.yml and windlock.lock.yml.
package lockfile

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Filename is the lockfile name at an application root.
const Filename = "windlock.lock.yml"

// Schema is the version this package reads and writes. It keeps every
// host-specific archive under platforms, so every host writes the same bytes.
const Schema = 3

// Lockfile is windlock.lock.yml. Everything above platforms is the same on
// every host. Each platform lists the archives that host downloads.
type Lockfile struct {
	Schema int    `yaml:"schema"`
	West   string `yaml:"west"`
	// Container is the image every container-backend command runs in.
	Container Container `yaml:"container"`
	// HostTools maps each pinned host program to its version.
	HostTools map[string]string   `yaml:"host_tools"`
	Zephyr    []Zephyr            `yaml:"zephyr"`
	Platforms map[string]Platform `yaml:"platforms"`
}

// Container names the image by digest, so every host runs the same one.
type Container struct {
	Image string `yaml:"image"`
}

// Zephyr is one side-by-side revision entry. The first entry is the default.
type Zephyr struct {
	Name           string    `yaml:"name"`
	URL            string    `yaml:"url"`
	Revision       string    `yaml:"revision"`
	Commit         string    `yaml:"commit"`
	SDK            string    `yaml:"sdk"`
	Toolchains     []string  `yaml:"toolchains"`
	ManifestSHA256 string    `yaml:"manifest_sha256"`
	Projects       []Project `yaml:"projects"`
	// PythonInputsSHA256 hashes the requirement files the pins were resolved from.
	PythonInputsSHA256 string `yaml:"python_inputs_sha256"`
	// PythonRequirements are the venv's packages, "name==version[ ; marker]".
	PythonRequirements []string `yaml:"python_requirements"`
}

// Project is one west project from west manifest --freeze --active-only.
type Project struct {
	Name     string `yaml:"name"`
	Path     string `yaml:"path"`
	Revision string `yaml:"revision"`
}

// Platform is what one host triple downloads.
type Platform struct {
	HostTools map[string]Archive `yaml:"host_tools,omitempty"`
	SDK       map[string]Bundle  `yaml:"sdk,omitempty"`
}

// Bundle is one SDK archive for a host triple.
type Bundle struct {
	Filename string `yaml:"filename"`
	URL      string `yaml:"url"`
	SHA256   string `yaml:"sha256"`
}

// Archive is one download and its sha256.
type Archive struct {
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256"`
}

// CheckSchema fails for a lockfile this package does not write.
func (l Lockfile) CheckSchema() error {
	if l.Schema != Schema {
		return fmt.Errorf("%s is schema %d, this windlock reads schema %d; rerun windlock init to rewrite it", Filename, l.Schema, Schema)
	}
	return nil
}

// DefaultEntry returns the first Zephyr entry.
func (l Lockfile) DefaultEntry() (Zephyr, error) {
	if len(l.Zephyr) == 0 {
		return Zephyr{}, fmt.Errorf("%s has no zephyr entry", Filename)
	}
	return l.Zephyr[0], nil
}

// Platform returns the archives recorded for triple.
func (l Lockfile) Platform(triple string) (Platform, error) {
	p, ok := l.Platforms[triple]
	if !ok {
		return Platform{}, fmt.Errorf("%s has no platform %s (recorded: %s)", Filename, triple, joinKeys(l.Platforms))
	}
	return p, nil
}

// SDKs returns each SDK version the entries use, in entry order.
func (l Lockfile) SDKs() []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range l.Zephyr {
		if e.SDK != "" && !seen[e.SDK] {
			seen[e.SDK] = true
			out = append(out, e.SDK)
		}
	}
	return out
}

// MoveZephyrFirst makes the entry at index i the default.
func (l *Lockfile) MoveZephyrFirst(i int) {
	l.Zephyr = moveFirst(l.Zephyr, i)
}

// RemoveZephyr removes the entry at index i.
func (l *Lockfile) RemoveZephyr(i int) {
	l.Zephyr = append(l.Zephyr[:i:i], l.Zephyr[i+1:]...)
}

// Read decodes one lockfile document from r. Unknown fields are an error.
func Read(r io.Reader) (Lockfile, error) {
	var lock Lockfile
	if err := decode(r, Filename, &lock); err != nil {
		return Lockfile{}, err
	}
	return lock, nil
}

// Write encodes lock to w. Map keys are sorted, so equal lockfiles encode to
// equal bytes.
func Write(w io.Writer, lock Lockfile) error {
	return encode(w, lock)
}

// ReadFile reads a lockfile from path.
func ReadFile(path string) (Lockfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return Lockfile{}, err
	}
	defer f.Close()
	return readChecked(f)
}

// WriteFile replaces path with lock. The file is written beside path and
// renamed over it, so an interrupted write leaves the previous file.
func WriteFile(path string, lock Lockfile) error {
	return writeAtomic(path, func(w io.Writer) error { return Write(w, lock) })
}

func decode(r io.Reader, name string, v any) error {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		if err == io.EOF {
			return fmt.Errorf("%s: empty file", name)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func encode(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Close()
}

func writeAtomic(path string, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	err = write(f)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func joinKeys[V any](m map[string]V) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := keys[0]
	for _, k := range keys[1:] {
		out += ", " + k
	}
	return out
}

// readChecked decodes a schema 3 lockfile, naming the schema of any other
// file instead of failing on its unknown fields.
func readChecked(r io.Reader) (Lockfile, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Lockfile{}, err
	}
	var peek struct {
		Schema int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(raw, &peek); err == nil && peek.Schema != 0 && peek.Schema != Schema {
		return Lockfile{}, Lockfile{Schema: peek.Schema}.CheckSchema()
	}
	return Read(bytes.NewReader(raw))
}
