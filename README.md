# Tue

This repository is being migrated from the legacy Go WebAssembly `vue`
runtime to Tue.

The old reflection/template interpreter runtime and its legacy WebAssembly
examples have been removed. Reviewable Tue implementation slices will land in
follow-up changes.

The pre-migration Vue runtime and examples remain available on the
[`vue` branch](https://github.com/norunners/tue/tree/vue).

## Current State

- The GitHub repository and Go module path are `github.com/norunners/tue`.
- Tue currently includes the runtime, compiler, CLI, production build, dev
  server, and formatter slices needed for small internal-app examples.
- Realistic `.tue` examples live under [`examples/`](examples/). They cover a
  todo queue, user table, settings form, dashboard, and small path-only hash
  router.

Try an example from the repository root:

```bash
go run ./cmd/tue check examples/todo
go run ./cmd/tue dev examples/todo
go run ./cmd/tue build examples/todo
```

## Verification

For the current baseline, the expected local checks are:

```bash
go fmt ./...
go test ./...
```

License
-------

* [MIT License](LICENSE)
