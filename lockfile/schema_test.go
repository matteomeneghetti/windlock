package lockfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// releasedSchemas holds the sha256 of every schema file a release has
// shipped. A released schema never changes (SCHEMA.md, rule 7); each
// release adds its new files here.
var releasedSchemas = map[string]string{}

const schemaDir = "../schema"

func TestReleasedSchemasAreFrozen(t *testing.T) {
	for name, want := range releasedSchemas {
		raw, err := os.ReadFile(filepath.Join(schemaDir, name))
		if err != nil {
			t.Fatalf("released schema %s: %v", name, err)
		}
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s changed after release (sha256 %s, released %s); add a new version instead", name, got, want)
		}
	}
}

func TestSchemaFilesMatchTheirNames(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(schemaDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no schema files: %v", err)
	}
	nameRE := regexp.MustCompile(`^(lockfile|config)-v([0-9]+)\.json$`)
	for _, path := range files {
		base := filepath.Base(path)
		m := nameRE.FindStringSubmatch(base)
		if m == nil {
			t.Errorf("%s is not named <lockfile|config>-v<N>.json", base)
			continue
		}
		var doc struct {
			ID         string `json:"$id"`
			Properties struct {
				Schema struct {
					Const int `json:"const"`
				} `json:"schema"`
			} `json:"properties"`
		}
		raw, _ := os.ReadFile(path)
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		if !strings.HasSuffix(doc.ID, "/schema/"+base) {
			t.Errorf("%s has $id %s", base, doc.ID)
		}
		if fmt.Sprint(doc.Properties.Schema.Const) != m[2] {
			t.Errorf("%s fixes schema to %d", base, doc.Properties.Schema.Const)
		}
	}
	for url, file := range map[string]string{
		SchemaURL:       fmt.Sprintf("lockfile-v%d.json", Schema),
		ConfigSchemaURL: fmt.Sprintf("config-v%d.json", ConfigSchema),
	} {
		if !strings.HasSuffix(url, "/schema/"+file) {
			t.Errorf("%s does not name the current schema %s", url, file)
		}
	}
}

// TestFilesMatchSchema checks real written files against the current
// schemas: the setup golden (assembleLock's output from the real pins) and
// windlock.yml as WriteConfigFile writes it. lockfile/testdata's lockfile
// uses placeholder hashes, so it tests the encoding, not the schema.
func TestFilesMatchSchema(t *testing.T) {
	lockSchema := compileSchema(t, fmt.Sprintf("lockfile-v%d.json", Schema))
	cfgSchema := compileSchema(t, fmt.Sprintf("config-v%d.json", ConfigSchema))

	golden, err := os.ReadFile("../setup/testdata/windlock.lock.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateYAML(lockSchema, golden); err != nil {
		t.Errorf("setup golden lockfile: %v", err)
	}
	// The schema must agree with the strict decoder about the same file.
	if _, err := Read(bytes.NewReader(golden)); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfigFile("testdata/windlock.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ConfigFilename)
	if err := WriteConfigFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(path)
	if err := validateYAML(cfgSchema, written); err != nil {
		t.Errorf("written windlock.yml: %v", err)
	}

	for name, bad := range map[string]string{
		"short sha256": strings.Replace(string(golden), "sha256: 83f8fd91", "sha256: 83f8", 1),
		"extra field":  strings.Replace(string(golden), "\nwest: ", "\nbogus: 1\nwest: ", 1),
		"newer schema": strings.Replace(string(golden), "\nschema: 1\n", "\nschema: 2\n", 1),
		"http url":     strings.Replace(string(golden), "url: https://github.com/Kitware", "url: http://github.com/Kitware", 1),
	} {
		if bad == string(golden) {
			t.Fatalf("%s: the golden changed, so the mutation no longer applies", name)
		}
		if err := validateYAML(lockSchema, []byte(bad)); err == nil {
			t.Errorf("%s: schema accepted it", name)
		}
	}
	if err := validateYAML(cfgSchema, []byte("targets:\n  ..:\n    board: b\n    source: s\n")); err == nil {
		t.Error("config target named .. was accepted")
	}
}

// TestGeneratedTypesAreCurrent reruns the pinned generator and compares its
// output with schema_gen.go. It downloads the generator, so it runs only
// with WINDLOCK_CHECK_GENERATE=1.
func TestGeneratedTypesAreCurrent(t *testing.T) {
	if os.Getenv("WINDLOCK_CHECK_GENERATE") != "1" {
		t.Skip("set WINDLOCK_CHECK_GENERATE=1 to rerun go generate")
	}
	src, err := os.ReadFile("generate.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^//go:generate (.+)$`).FindSubmatch(src)
	if m == nil {
		t.Fatal("generate.go has no go:generate line")
	}
	out := filepath.Join(t.TempDir(), "schema_gen.go")
	args := strings.Fields(strings.Replace(string(m[1]), "-o schema_gen.go", "-o "+out, 1))
	cmd := exec.Command(args[0], args[1:]...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, b)
	}
	want, _ := os.ReadFile(out)
	got, _ := os.ReadFile("schema_gen.go")
	if !bytes.Equal(want, got) {
		t.Fatal("schema_gen.go is stale; run go generate ./lockfile")
	}
}

func compileSchema(t *testing.T, file string) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(filepath.Join(schemaDir, file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(file, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(file)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return s
}

// validateYAML checks a YAML document against s. It goes through JSON, so
// numbers and strings have the types a JSON reader would see.
func validateYAML(s *jsonschema.Schema, raw []byte) error {
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return err
	}
	j, err := json.Marshal(v)
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		return err
	}
	return s.Validate(inst)
}
