# Tue

Tue is an experimental `.tue` single-file component compiler and small Go
WebAssembly runtime.

The old Vue-style reflection/template interpreter has been removed from the
active tree. Tue now moves through the ahead-of-time compiler path documented in
`docs/`.

## Status

This repository is mid-migration:

- `tue check` discovers `.tue` files and reports parser/type diagnostics.
- `tue build` generates Go files under `.tue-cache`.
- `tue dev` and `tue fmt` are command stubs for later implementation phases.
- The repository and module still use `github.com/norunners/vue` until the
  planned repository/module rename lands.

## Current Commands

Run diagnostics for the fixtures:

```bash
go run ./cmd/tue check ./testdata/fixtures
```

Generate Go for a fixture project:

```bash
go run ./cmd/tue build ./testdata/fixtures
```

Run the Go test suite:

```bash
go test ./...
```

## Project Shape

- `cmd/tue/` contains the CLI entry point.
- `internal/compiler/` contains the SFC parser, template parser, script parser,
  checker, and Jennifer-backed Go generator.
- Root package files contain the current runtime primitives used by generated
  components.
- `testdata/fixtures/` contains current `.tue` compiler fixtures.
- `docs/` contains the high-level design, implementation plan, naming notes, and
  repository migration plan.

## Examples

Legacy examples were retired with the old runtime. New examples should be
rebuilt as `.tue` projects when the compiler/runtime supports each feature.

License
-------

* [MIT License](LICENSE)
