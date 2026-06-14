# Tue

This repository is being migrated from the legacy Go WebAssembly `vue`
runtime to Tue.

The old reflection/template interpreter runtime and its legacy WebAssembly
examples have been removed. Reviewable Tue implementation slices will land in
follow-up changes.

The pre-migration Vue runtime and examples remain available on the
[`vue` branch](https://github.com/norunners/vue/tree/vue).

## Current State

- The GitHub repository and Go module path are still `github.com/norunners/vue`
  until the planned repository/module rename lands separately.
- The active root package is intentionally a placeholder until the Tue runtime
  is rebuilt in reviewable slices.
- Initial `.tue` source fixtures live under `testdata/fixtures/`; they define
  the intended component shape before parser/runtime work lands.

## Verification

For the current baseline, the expected local checks are:

```bash
go fmt ./...
go test ./...
```

License
-------

* [MIT License](LICENSE)
