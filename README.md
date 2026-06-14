# Tue

This repository is being migrated from the legacy Go WebAssembly `vue`
runtime to Tue.

The old reflection/template interpreter runtime and its legacy WebAssembly
examples have been removed. Reviewable Tue implementation slices will land in
follow-up changes.

## Current State

- The GitHub repository and Go module path are still `github.com/norunners/vue`
  until the planned repository/module rename lands separately.
- There is intentionally no active runtime package in this slice after removing
  the legacy Vue implementation.
- Future changes will introduce Tue docs, the `.tue` compiler, the runtime, and
  rebuilt examples in smaller reviewable pieces.

## Verification

For this removal-only slice, the expected local checks are:

```bash
go fmt ./...
go test ./...
```

Both commands may report that `./...` matched no packages until the first Tue
implementation slice adds Go packages back to the repository.

License
-------

* [MIT License](LICENSE)
