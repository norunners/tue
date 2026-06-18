# Tue Examples

These examples are intentionally small internal-app screens that exercise Tue's
current compiler and runtime surface.

Run commands from the repository root:

```bash
go run ./cmd/tue check examples/todo
go run ./cmd/tue dev examples/todo
go run ./cmd/tue build examples/todo
```

Replace `todo` with any example directory:

- `todo`: refs, events, `v-model`, keyed lists, dynamic classes, and filtered list state
- `user-table`: text filtering, checkbox state, derived list state, and table rendering
- `settings-form`: string and boolean `v-model` controls with save/dirty feedback
- `dashboard`: component props, default slots, scoped styles, and child components
- `router`: explicit hash routes, `router.Href` links, route params, and not-found state

The router example intentionally uses Tue's small hash router, not a Vue Router
replacement. It matches normalized paths only, does not expose query helpers
yet, and creates the router in `Init(ctx)` so the browser hash listener can be
cleaned up when the component unmounts.

`tue dev` serves the generated `dist/` directory and rebuilds as files change.
`tue build` writes production output to the example's `dist/` directory.
