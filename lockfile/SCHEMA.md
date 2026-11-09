# Project file schemas

windlock writes two files in a project, and each has its own format version in a `schema` field:

| File | Version | Definition |
| --- | --- | --- |
| `windlock.lock.yml` | 1 | [`schema/lockfile-v1.json`](../schema/lockfile-v1.json) |
| `windlock.yml` | 1 | [`schema/config-v1.json`](../schema/config-v1.json) |

The JSON Schema files are the definition of each version. The Go types in `schema_gen.go` are generated from the current versions with `go generate ./lockfile` (the generator is pinned in `generate.go`). Every written file starts with a `# yaml-language-server: $schema=…` line, so editors with YAML support validate it.

## Rules

1. `schema` versions the file **format**. Any change to fields or their meaning bumps it, including an optional addition, because windlock decodes strictly and an older windlock would reject the new field.
2. windlock reads every schema from `MinSchema` to `Schema` (`MinConfigSchema` to `ConfigSchema` for `windlock.yml`). An older file decodes into its own frozen types in `internal/vN` and is migrated in memory by pure `migrateN→N+1` functions.
3. A file newer than `Schema` gets `… is schema N; this windlock reads up to schema M; upgrade windlock`. One older than `MinSchema` gets `… rerun windlock init to rewrite it`.
4. **Migration:** read-only commands (`sync`, `build`, `flash`, `doctor`, …) migrate in memory, never rewrite the file, and print a one-line hint. Writing commands (`init`, `target add`, …) write the current schema. `windlock migrate` rewrites the file as the current schema without re-resolving anything, so every pin stays as it is. It ships with the first schema 2.
5. Before 1.0, a release may raise `MinSchema`, with a release-notes entry. From 1.0 on, only a major release may.
6. `windlock.yml` follows the same rules with its own numbers. A `windlock.yml` without `schema` is schema 1.
7. A released schema file never changes; a change means a new version file. `TestReleasedSchemasAreFrozen` holds the sha256 of every released file, and each release adds its new ones. Until the first tagged release, v1 is edited in place.
8. Temporary: a lockfile with `schema: 3` comes from a build before the first release, before the numbering restarted at 1, and gets `rerun windlock init`. Remove that case before a real schema 3 ships.

## Bumping a version

1. Copy `schema/lockfile-vN.json` to `lockfile-vN+1.json`, change `$id`, the `schema` const and the fields.
2. Point `generate.go` at the new file and move the old one's types into `lockfile/internal/vN` (the same generator, with `-p vN`).
3. Raise `Schema` and `SchemaURL`, add the `case N:` decode and `migrateN→N+1` in `readChecked`, and add a vN fixture with a test that it migrates.
4. If `encode`'s field order changes, update `orderFields`.
5. Add an entry below.

## Changelog

- **lockfile 1, config 1** (unreleased): first versioned formats. The lockfile's fields are unchanged from the pre-release `schema: 3`; `windlock.yml` gains `schema`.
