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

// Schema is the lockfile format version this windlock writes, and
// MinSchema the oldest it reads. schema/lockfile-v<N>.json defines each
// version, and the types in schema_gen.go are generated from the current
// one; see SCHEMA.md for the rules.
const (
	Schema    = 1
	MinSchema = 1
)

// SchemaURL is the $id of the current lockfile schema. Written files name
// it, so editors can validate them.
const SchemaURL = "https://raw.githubusercontent.com/matteomeneghetti/windlock/main/schema/lockfile-v1.json"

// CheckSchema fails for a lockfile this windlock cannot read.
func (l Lockfile) CheckSchema() error {
	switch {
	case l.Schema >= MinSchema && l.Schema <= Schema:
		return nil
	case l.Schema > Schema:
		return fmt.Errorf("%s is schema %d; this windlock reads up to schema %d; upgrade windlock", Filename, l.Schema, Schema)
	default:
		return fmt.Errorf("%s is schema %d; this windlock reads schema %d to %d; rerun windlock init to rewrite it", Filename, l.Schema, MinSchema, Schema)
	}
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
	if _, err := io.WriteString(w, schemaComment(SchemaURL)); err != nil {
		return err
	}
	return encode(w, lock)
}

// schemaComment is the first line of every written file. It names the
// file's JSON Schema for editors; readers ignore it.
func schemaComment(url string) string {
	return "# yaml-language-server: $schema=" + url + "\n"
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
	var node yaml.Node
	if err := node.Encode(v); err != nil {
		return err
	}
	orderFields(&node)
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return err
	}
	return enc.Close()
}

// The generated types list fields alphabetically. orderFields writes them
// in reading order instead: the version first, then each file's fields as
// the schema lists them. Map keys (tools, triples, targets) stay sorted.
func orderFields(root *yaml.Node) {
	order(root, "schema", "west", "container", "sources", "zephyr", "target", "targets", "platforms")
	if entries := mappingValue(root, "zephyr"); entries != nil {
		for _, e := range entries.Content {
			order(e, "name", "url", "revision", "commit", "sdk", "toolchains", "manifest_sha256", "projects", "python_inputs_sha256", "python_requirements")
		}
	}
	tools := func(m *yaml.Node) {
		for _, t := range mappingValues(m) {
			order(t, "version", "url", "sha256")
		}
	}
	tools(mappingValue(root, "sources"))
	for _, p := range mappingValues(mappingValue(root, "platforms")) {
		order(p, "build_tools", "flash_tools", "sdk")
		tools(mappingValue(p, "build_tools"))
		tools(mappingValue(p, "flash_tools"))
		for _, b := range mappingValues(mappingValue(p, "sdk")) {
			order(b, "filename", "url", "sha256")
		}
	}
}

// order moves keys, in that order, to the front of mapping node m. Other
// keys keep their order after them.
func order(m *yaml.Node, keys ...string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	var front, rest []*yaml.Node
	for _, key := range keys {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == key {
				front = append(front, m.Content[i], m.Content[i+1])
			}
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !contains(keys, m.Content[i].Value) {
			rest = append(rest, m.Content[i], m.Content[i+1])
		}
	}
	m.Content = append(front, rest...)
}

func contains(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// mappingValues returns the values of mapping node m.
func mappingValues(m *yaml.Node) []*yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	var out []*yaml.Node
	for i := 1; i < len(m.Content); i += 2 {
		out = append(out, m.Content[i])
	}
	return out
}

// mappingValue returns the value of key in mapping node m, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
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

// readChecked decodes a lockfile by its schema number, so a file of another
// schema gets a message naming it instead of an unknown-field error.
func readChecked(r io.Reader) (Lockfile, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Lockfile{}, err
	}
	var peek struct {
		Schema int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(raw, &peek); err != nil || peek.Schema == 0 {
		// Not a mapping, or no schema: the strict decode names the problem.
		return Read(bytes.NewReader(raw))
	}
	if err := (Lockfile{Schema: peek.Schema}).CheckSchema(); err != nil {
		return Lockfile{}, err
	}
	// Once MinSchema < Schema, an older schema N decodes here into its
	// frozen types in internal/vN and migrates to the current one
	// (SCHEMA.md, rule 4).
	return Read(bytes.NewReader(raw))
}
