package lockfile

// The file types are generated from the JSON Schema of each file's current
// version, which is their definition; see SCHEMA.md.
//go:generate go run github.com/atombender/go-jsonschema@v0.24.1 --only-models --tags yaml -p lockfile -t --minimal-names --disable-custom-types-for-maps --capitalization URL,SDK,SHA -o schema_gen.go ../schema/lockfile-v1.json ../schema/config-v1.json
