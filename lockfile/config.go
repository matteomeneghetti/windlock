package lockfile

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
)

// ConfigFilename is the project file beside the lockfile. windlock
// commands write it.
const ConfigFilename = "windlock.yml"

// ConfigSchema is the windlock.yml format version this windlock writes, and
// MinConfigSchema the oldest it reads. schema/config-v<N>.json defines each
// version; a file without a schema field is schema 1.
const (
	ConfigSchema    = 1
	MinConfigSchema = 1
)

// ConfigSchemaURL is the $id of the current config schema.
const ConfigSchemaURL = "https://raw.githubusercontent.com/matteomeneghetti/windlock/main/schema/config-v1.json"

// CheckSchema fails for a windlock.yml this windlock cannot read.
func (c Config) CheckSchema() error {
	switch {
	case c.Schema >= MinConfigSchema && c.Schema <= ConfigSchema:
		return nil
	case c.Schema > ConfigSchema:
		return fmt.Errorf("%s is schema %d; this windlock reads up to schema %d; upgrade windlock", ConfigFilename, c.Schema, ConfigSchema)
	default:
		return fmt.Errorf("%s is schema %d; this windlock reads schema %d to %d", ConfigFilename, c.Schema, MinConfigSchema, ConfigSchema)
	}
}

// NamedTarget is a target with its key in targets, which names build/<name>.
type NamedTarget struct {
	Name string
	Target
}

// TargetNamed returns the named build target. An empty name uses the target
// field.
func (c Config) TargetNamed(name string) (NamedTarget, error) {
	if len(c.Targets) == 0 {
		return NamedTarget{}, fmt.Errorf("%s has no targets", ConfigFilename)
	}
	if name == "" {
		name = c.Target
	}
	if name == "" {
		return NamedTarget{}, fmt.Errorf("%s target is not set", ConfigFilename)
	}
	target, ok := c.Targets[name]
	if !ok {
		return NamedTarget{}, fmt.Errorf("%s has no target %q", ConfigFilename, name)
	}
	if target.Board == "" || target.Source == "" {
		return NamedTarget{}, fmt.Errorf("%s target %q needs a board and source", ConfigFilename, name)
	}
	return NamedTarget{Name: name, Target: target}, nil
}

// Boards returns each distinct target board, sorted.
func (c Config) Boards() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range c.Targets {
		if t.Board != "" && !seen[t.Board] {
			seen[t.Board] = true
			out = append(out, t.Board)
		}
	}
	sort.Strings(out)
	return out
}

// ReadConfig decodes windlock.yml from r. Unknown fields are an error, and
// a file without a schema field is schema 1.
func ReadConfig(r io.Reader) (Config, error) {
	var cfg Config
	if err := decode(r, ConfigFilename, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Schema == 0 {
		cfg.Schema = 1
	}
	if err := cfg.CheckSchema(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ReadConfigFile reads windlock.yml at path.
func ReadConfigFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	return ReadConfig(f)
}

// WriteConfigFile writes a new windlock.yml at path.
func WriteConfigFile(path string, cfg Config) error {
	cfg.Schema = ConfigSchema
	return writeAtomic(path, func(w io.Writer) error {
		if _, err := io.WriteString(w, schemaComment(ConfigSchemaURL)+configHeader); err != nil {
			return err
		}
		return encode(w, cfg)
	})
}

const configHeader = `# Managed by windlock. Change it with windlock target ... and
# windlock zephyr ..., which keep windlock.lock.yml in step.
`

var targetNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidTargetName fails unless name can be a target: one path segment of
// letters, digits, dot, dash, or underscore, since it names build/<name>.
func ValidTargetName(name string) error {
	if !targetNameRE.MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("target name %q must be letters, digits, '.', '-', or '_'", name)
	}
	return nil
}

// AddTarget adds a target; the first one added becomes the default.
func (c *Config) AddTarget(name string, t Target) error {
	if err := ValidTargetName(name); err != nil {
		return err
	}
	if t.Board == "" || t.Source == "" {
		return fmt.Errorf("target %s needs a board and a source", name)
	}
	if _, ok := c.Targets[name]; ok {
		return fmt.Errorf("%s already has target %s", ConfigFilename, name)
	}
	if c.Targets == nil {
		c.Targets = map[string]Target{}
	}
	c.Targets[name] = t
	if c.Target == "" {
		c.Target = name
	}
	return nil
}

// RemoveTarget removes a target. Removing the default leaves none set.
func (c *Config) RemoveTarget(name string) error {
	if _, ok := c.Targets[name]; !ok {
		return fmt.Errorf("%s has no target %q", ConfigFilename, name)
	}
	delete(c.Targets, name)
	if len(c.Targets) == 0 {
		c.Targets = nil
	}
	if c.Target == name {
		c.Target = ""
	}
	return nil
}

// SetDefaultTarget makes name the target build uses when given none.
func (c *Config) SetDefaultTarget(name string) error {
	if _, ok := c.Targets[name]; !ok {
		return fmt.Errorf("%s has no target %q", ConfigFilename, name)
	}
	c.Target = name
	return nil
}

// RemoveZephyr removes the revision at index i. The last one stays.
func (c *Config) RemoveZephyr(i int) error {
	if i < 0 || i >= len(c.Zephyr) {
		return fmt.Errorf("%s has no zephyr revision %d", ConfigFilename, i)
	}
	if len(c.Zephyr) == 1 {
		return fmt.Errorf("%s lists only zephyr %s; add another before removing it", ConfigFilename, c.Zephyr[0])
	}
	c.Zephyr = append(c.Zephyr[:i:i], c.Zephyr[i+1:]...)
	return nil
}

// MoveZephyrFirst makes the revision at index i the default.
func (c *Config) MoveZephyrFirst(i int) error {
	if i < 0 || i >= len(c.Zephyr) {
		return fmt.Errorf("%s has no zephyr revision %d", ConfigFilename, i)
	}
	c.Zephyr = moveFirst(c.Zephyr, i)
	return nil
}

func moveFirst[T any](s []T, i int) []T {
	out := make([]T, 0, len(s))
	out = append(out, s[i])
	out = append(out, s[:i]...)
	return append(out, s[i+1:]...)
}
