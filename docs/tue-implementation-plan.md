# Tue Implementation Plan

This plan turns the high-level Tue design into ordered implementation tasks.
Tasks are intended to be executed one at a time, with review and adjustment after
each task before starting the next.

## Working Assumptions

- Tue is the target product. The current `vue` API is not a compatibility
  constraint unless a later review explicitly chooses to preserve part of it.
- The desired developer loop drives the architecture:

  ```text
  edit .tue file
  -> get useful source-mapped errors
  -> rebuild quickly enough
  -> browser updates predictably
  -> production build emits static assets
  ```

- The first valuable milestone is not feature breadth. It is one `.tue`
  component compiling through the same path that later features will use.
- Generated Go should be readable, but generated Go errors are not the primary
  user interface. Diagnostics must point back to `.tue` source locations.
- The runtime should stay small and boring: explicit reactivity, VDOM first,
  keyed patching where required, and no router/HMR/plugin system until the core
  compiler and runtime loop are proven.
- Prefer Go standard library packages first. Any dependency addition should be
  reviewed as part of the task that needs it.
- Use tokensave before source exploration, and run `tokensave sync` after every
  file-changing task.

## Execution Loop

Every task should follow this loop:

1. Use tokensave context/search/files before reading or scanning source files.
2. State the exact success criteria for the task.
3. Make the smallest code/doc change that satisfies the task.
4. Run `go fmt ./...`.
5. Run `go test ./...`.
6. Run `golangci-lint run` if linting is configured.
7. Run `go test -race ./...` only when concurrency changed.
8. Run `tokensave sync` after the final file change for the task.
9. Review the diff and test output.
10. Stop for review before starting the next task.

If a command cannot run, report the exact failure and do not treat the task as
complete.

Generated Go policy:

- Use Jennifer (`github.com/dave/jennifer/jen`) for generated Go.
- Keep Jennifer isolated to `internal/compiler/gogen`; other compiler stages
  should exchange typed Tue IR and diagnostics, not Jennifer statements.
- Use Jennifer render/save APIs for generated files. `%#v` rendering is only for
  small unit tests.
- Continue running `gofmt` or `go/format` on generated files as the final
  normalization/validation step.
- Do not concatenate Go declarations or statements by hand except for file
  headers, comments, and literal text values.

## Target Repository Shape

This is a proposed end-state shape. It should be introduced gradually.

```text
cmd/tue/
  main.go

internal/compiler/
  sfc/
  template/
  script/
  symbols/
  typecheck/
  gogen/        # Jennifer-backed Go generator
  css/
  assets/
  build/

internal/devserver/
  server/
  watcher/
  overlay/

runtime/
  component/
  dom/
  reactivity/
  resource/
  style/
  vdom/

tue.go
docs/
examples/
testdata/
```

The existing root package can either become the public `tue` package or be
replaced by a new package layout. That decision should happen before broad code
rewrites, because the current module is `github.com/norunners/vue` while the new
design uses `tue`.

## Review Gates

Each gate exists to avoid building too far on a wrong assumption.

- Gate 1: command/package naming and repository layout accepted.
- Gate 2: SFC parser and source spans accepted.
- Gate 3: template diagnostics shape accepted.
- Gate 4: generated Go strategy accepted.
- Gate 5: runtime reactivity and VDOM contracts accepted.
- Gate 6: component contracts, props, and events accepted.
- Gate 7: dev server reload model accepted.
- Gate 8: production output accepted.

## Ordered Tasks

### 1. Establish Product Skeleton and Naming

Goal: make the repository express Tue as the product before implementation
details harden.

Scope:

- Decide whether the module path changes from `github.com/norunners/vue` to a
  Tue path now or later.
- Decide public package names for root API and runtime subpackages.
- Add a short architecture note if the chosen naming differs from the high-level
  design.
- Add initial `.tue` fixtures under `testdata/fixtures/`:
  - static hello component
  - interpolation component
  - counter component
  - parent/child props component
  - invalid prop type component

Verification:

- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm naming, module path, and fixture shape before parser work starts.

### 2. Add CLI Shell

Goal: make the user-facing command shape real without implementing all behavior.

Scope:

- Add `cmd/tue`.
- Implement command dispatch for `check`, `build`, `dev`, and `fmt`.
- Return clear "not implemented" errors for commands that are only stubbed.
- Wire `tue check` to accept a project root and discover `.tue` files, even if
  it only reports them at first.

Out of scope:

- `tue create`
- dev server
- WASM build
- production assets

Verification:

- CLI unit tests for command dispatch and project-root handling.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm command UX, flags, and error style before compiler internals depend on
  them.

### 3. Implement SFC Block Parser

Goal: parse `.tue` files into source-spanned blocks.

Scope:

- Parse `<template>`, `<script lang="go">`, `<style>`, and `<style scoped>`.
- Reject unsupported blocks with source locations.
- Enforce required blocks:
  - `<script lang="go">` required
  - `<template>` required unless a later handwritten-render mode is explicitly
    introduced
- Preserve byte offsets, line/column positions, block attributes, and raw block
  content.
- Support multiple style blocks only if the design explicitly needs it; keep the
  first version minimal.

Out of scope:

- Template AST
- Go type checking
- CSS transform

Verification:

- Table-driven parser tests for valid blocks, missing blocks, duplicate blocks,
  malformed tags, and unsupported blocks.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm block model and diagnostic formatting before template parsing starts.

### 4. Implement Template AST Parser

Goal: turn the template block into a Tue-owned AST with source spans.

Scope:

- Parse elements, text, comments if needed, static attributes, bound attributes,
  events, interpolation, and component-looking tags.
- Recognize MVP directives syntactically:
  - `v-if`
  - `v-else`
  - `v-for`
  - `v-model`
  - `v-html`
  - `:prop`
  - `@event`
- Record exact source spans for expressions and directive names.
- Detect syntax-only errors early, such as malformed interpolation or invalid
  directive forms.

Out of scope:

- Type checking expressions
- Code generation
- DOM rendering

Verification:

- AST golden tests for representative templates.
- Diagnostic tests for malformed interpolation, invalid directive shape, and
  unsupported syntax.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm AST shape is sufficient for type checking and code generation without
  overgeneralizing.

### 5. Extract Script Symbols and Component Contracts

Goal: understand the Go script block enough to type-check templates.

Scope:

- Parse the script block with Go parser/types.
- Extract package name, imports, `<ComponentName>`, `tue.Prop[T]` fields, and
  optional `Init(ctx tue.Context)` method signature.
- Identify prop fields, state fields, methods/function fields, refs, computed
  values, and resources.
- Accept exported or unexported prop fields. Generated code for a component is
  emitted into that component's package so it can fill unexported prop fields.
- Treat any method named `Init` with a non-`func (c *T) Init(tue.Context)`
  signature as a diagnostic.
- Build a component symbol table for one component at a time.
- Create a stable internal representation for component contracts:
  - props
  - emitted events or callback fields
  - generated allocation shape
  - optional init method shape

Out of scope:

- Cross-file component import resolution
- Full generated render code
- Resource runtime behavior

Verification:

- Tests against script-only fixtures.
- Tests for invalid `Init` signatures, optional props handling, prop field
  validation including unexported prop fields, and missing component contracts.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Event model decision: model component events as callback/function fields first.
  Do not add an explicit emits API until callback fields prove insufficient.

### 6. Build Template Type Checking Vertical Slice

Goal: make `tue check` useful before rendering exists.

Scope:

- Type-check interpolation expressions against component fields, including
  `tue.Prop[T]` prop fields and local state fields.
- Type-check static/bound attributes for native elements where possible.
- Type-check component prop existence and basic prop type compatibility for a
  single-file or explicitly registered component.
- Validate event handler existence for native events and component events.
- Validate that `v-model` targets are writable.
- Validate that `v-for` has a `:key` when rendering dynamic lists.
- Map all diagnostics back to `.tue` source spans.

Out of scope:

- Complete Vue expression language.
- Runtime execution.
- Perfect CSS or DOM validation.

Verification:

- Positive and negative fixture tests.
- Golden diagnostic tests for source snippets and carets.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Implemented as a narrow compiler checker in `internal/compiler/checker`.
- `tue check` now parses the project first, then runs template checks only when
  SFC, template, and script parsing all succeeded to avoid cascaded diagnostics.
- The checker currently supports Go-expression parsing, component fields,
  `tue.Prop[T]`, `tue.Ref[T]`, computed/resource read types, callback fields,
  methods, simple literal/binary/unary expressions, and `v-for` locals.
- Component lookup is project-local by generated component name. Kebab-case
  component tags, imported component aliases, slots, and full Go package type
  checking remain deferred.
- Diagnostic source spans are covered by line/column and caret tests before
  generated Go is introduced.

### 7. Generate Go for Static Render Slice

Goal: compile one `.tue` component into generated Go that renders static DOM in
the browser.

Scope:

- Add `github.com/dave/jennifer/jen` as the Go code-generation dependency.
- Implement `internal/compiler/gogen` around Jennifer files/statements rather
  than handwritten Go source strings.
- Generate `*_tue.go` and `*_render_tue.go` into `.tue-cache/`.
- Generate code for static elements, static attributes, escaped text, and simple
  interpolation.
- Use Jennifer qualified identifiers for runtime references so imports are
  generated predictably.
- Use Jennifer render/save APIs for generated files; reserve `%#v` rendering for
  focused tests only.
- Run `gofmt` or `go/format` on generated files after Jennifer output.
- Keep generated code readable enough for debugging.
- Introduce a minimal generated manifest.
- Preserve source-span metadata beside generated nodes so later diagnostics can
  map generated Go errors back to `.tue` source.

Out of scope:

- Events
- Reactive updates
- Components
- CSS scoping
- Production dist polish

Verification:

- Golden generated-code tests.
- Tests proving import generation/qualification is deterministic.
- Tests proving generator output parses with `go/parser` and formats cleanly.
- A minimal WASM build command for the fixture app.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Implemented `internal/compiler/gogen` around Jennifer v1.7.1.
- `tue build` now parses and checks the project, then writes generated files to
  `.tue-cache/`.
- Each source file produces:
  - `<source>_tue.go`: a formatted generated copy of the `<script lang="go">`
    block so the component type and methods exist in the cache package.
  - `<source>_render_tue.go`: Jennifer-generated `New<Component>()` component
    glue and a VNode render function.
- `.tue-cache/manifest.json` records component names, generated files,
  template spans, and generated node source spans.
- The static render slice supports native elements, static attributes, escaped
  text, and simple interpolation expressions over component fields. It rejects
  components, events, directives, and other dynamic template constructs at build
  time with source-mapped diagnostics.
- Generated static/interpolation cache output compiles under
  `GOOS=js GOARCH=wasm` in a temporary fixture module using the current runtime.
- Constructor/runtime shape is intentionally provisional; Task 8 owns the final
  runtime contracts for `Context`, props, refs, mount, and lifecycle behavior.

### 8. Replace Runtime Foundation

Goal: establish the runtime contracts generated code will call.

Scope:

- Introduce minimal public root API:
  - `Context`
  - `Prop[T]`
  - generated-code helpers for concrete prop values
  - `Ref[T]` and `RefOf`
  - `Computed[T]` and `ComputedOfFunc`
  - `Watch`
  - `Mount`
- Introduce runtime subpackages or internal equivalents:
  - component lifecycle ordering for generated allocation, optional `Init`,
    `OnMounted`, `OnUpdated`, and `OnUnmounted`
  - VNode type
  - DOM mount/patch boundary
- Implement static VDOM mount for generated nodes.
- Do not keep the old reflection/template interpreter in the active tree.
  It was removed in the Tue-first repository migration once the snapshot branch
  was verified.

Out of scope:

- Full patch algorithm
- Scheduler
- Resource loading
- `Resource[T]` and `ResourceOfFunc`; resource loading is Task 14, not part of the
  foundation slice
- Router

Verification:

- Runtime unit tests that can run outside js/wasm where possible.
- WASM smoke build for static fixture.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Implemented 2026-06-12:
  - The legacy reflection/template interpreter was first isolated behind the
    `legacy && js && wasm` build tag, then removed from the active tree during
    the Tue-first repository migration.
  - The root runtime foundation includes `Context`, `Prop[T]`, `Ref[T]`,
    `Computed[T]`, `Watch`, `Mount`, `VNode`, static attributes, and the
    generated-code helpers `CompOf`, `PropOf`, and `PropOfFunc`.
  - `CompOf` receives the generated component pointer and render function,
    calls optional `Init(ctx)` immediately, and keeps `OnMounted`, `OnUpdated`,
    and `OnUnmounted` hooks separate from initialization.
  - Static mount is implemented through a small DOM boundary: pure Go tests
    render through static HTML, while `js/wasm` `Mount` finds the DOM target and
    sets its `innerHTML`.
  - The Jennifer generator now emits `vue.VNode` trees and returns
    `*vue.Comp` descriptors through `vue.CompOf` instead of using the old
    option/template pipeline.
  - Value helpers follow the access-interface/concrete-return rule:
    component fields use interfaces such as `Ref[T]` and `Prop[T]`, while
    helpers return concrete values such as `*RefValue[T]` and `*PropValue[T]`.
- Still deferred to later tasks:
  - dependency tracking, computed caching, batching, and scheduler semantics
  - DOM patching and native event handling
  - parent/child component VNodes and prop update propagation
  - `Resource[T]` and `ResourceOfFunc`

### 9. Implement Reactivity Core

Goal: support the counter demo without overbuilding.

Scope:

- Implement `Ref[T]`.
- Implement `RefOf[T](initial T) *RefValue[T]`.
- Implement `Computed[T]` with caching.
- Implement `ComputedOfFunc[T](compute func() T) *ComputedValue[T]`.
- Implement `Watch`.
- Implement dependency tracking.
- Implement batched updates.
- Add component-scoped effect cleanup.
- Define the scheduler semantics explicitly.

Out of scope:

- Resource loading
- State preservation across reloads
- Advanced effect scopes

Verification:

- Pure Go tests for refs, computed caching, watcher invalidation, batching, and
  cleanup.
- `go test -race ./...` if scheduler/concurrency uses goroutines.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Implemented 2026-06-12:
  - `RefValue` now participates in dependency tracking; every `Set` invalidates
    dependents.
  - `ComputedValue` is lazy and cached through `ComputedOfFunc`; dependency
    writes mark it dirty and notify downstream watchers without recomputing
    immediately.
  - `Watch` now runs immediately, tracks reactive reads, reruns after
    invalidation, and returns a stop function. Existing call sites may ignore
    the return value.
  - `Batch` defines scheduler coalescing: outside a batch, watcher reruns flush
    synchronously before `Set` returns; inside nested batches, reruns are
    deduplicated and flushed after the outermost batch returns.
  - `Prop.Get` participates in dependency tracking, and `Prop.Watch` observes
    reactive prop getters using the same stop-function convention as `Watch`.
  - Watchers and computed dependency subscriptions created during component
    `Init` are registered against the component scope and stopped during
    `Unmount` before `OnUnmounted` hooks run.
- Still deferred to later tasks:
  - Reactive component rerender wiring through DOM patching.
  - Parent/child prop update propagation.
  - Async `Resource[T]` effect lifecycle.

### 10. Implement VDOM Patching and Events

Goal: make a stateful counter render and update in the browser.

This task is intentionally split into reviewable implementation slices. Treat
10A, 10B, and 10C as ordered mini-tasks; do not try to implement the whole
section in one pass.

#### 10A. Add the DOM Patch Boundary and Static VNode Patching

Goal: replace the innerHTML-only mount path with a patchable DOM boundary while
keeping behavior equivalent for static generated output.

Scope:

- Introduce a small runtime DOM boundary that can be implemented by
  `syscall/js` in browser builds and by a deterministic fake in pure Go tests.
- Store the previously rendered `VNode` tree on mounted components.
- Implement the first patch rules:
  - same type and same key patches in place
  - different type or key replaces
  - text node content updates in place
  - element attributes add, update, and remove
  - unkeyed children patch positionally
- Preserve `RenderHTML` as the static string renderer used by tests, generated
  output inspection, and non-browser fallbacks.

Out of scope:

- Event listeners
- Reactive rerender scheduling
- Keyed reordering
- Component nodes
- `v-model`

Verification:

- Pure Go patch tests against the fake DOM boundary.
- Existing static fixture WASM compile still passes.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Implemented 2026-06-12:
  - `Comp` now stores the mounted VNode tree and patches that tree during
    explicit `Update` calls instead of rewriting `innerHTML`.
  - The runtime has a small DOM boundary with browser `syscall/js` and pure-Go
    fake implementations.
  - Static patching handles text updates, element replacement by type/tag/key,
    attribute add/update/remove, root fragments, and positional unkeyed
    children.
  - `RenderHTML` remains the static string renderer for tests, generated output
    inspection, and non-browser fallback behavior.
- Still deferred to 10B and 10C:
  - reactive render-effect scheduling
  - native event listener attachment and cleanup
  - generated event-handler code for the counter fixture

#### 10B. Wire Reactive Component Rerenders

Goal: make mounted components rerender when reactive values read during render
change.

Scope:

- Run mounted component renders inside a component-scoped reactive effect.
- Schedule component updates through the existing synchronous scheduler so
  multiple invalidations inside `Batch` coalesce into one DOM patch.
- Ensure updates do not run before the component is mounted.
- Run `OnUpdated` after a reactive patch completes.
- Stop the render effect during `Unmount`.

Out of scope:

- Event listeners
- Generator support for event attributes
- Component child VNodes
- Cross-component prop propagation

Verification:

- Pure Go component tests proving reactive refs cause one rerender per flush.
- Tests proving `OnUpdated` runs after reactive rerender and stops after
  `Unmount`.
- Existing static fixture WASM compile still passes.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Implemented 2026-06-12:
  - Mounted components now create a component-scoped render effect during
    `mount`.
  - Reactive values read by the render function schedule that render effect
    through the existing synchronous scheduler, so invalidations inside `Batch`
    coalesce into one DOM patch.
  - Components do not render before mount; the initial mount render establishes
    the dependency set.
  - `OnUpdated` runs after the DOM patch completes, and lifecycle hooks are run
    outside render dependency tracking.
  - `Unmount` stops the render effect before removing DOM and before running
    unmount hooks.
- Still deferred to 10C:
  - native event listener attachment and cleanup
  - generated event-handler code for the counter fixture

#### 10C. Add Native DOM Events and Counter Generation

Goal: support the counter fixture's `@click="increment"` path end to end.

Scope:

- Define the first native event binding helper and handler shape in the root
  runtime API.
- Attach native DOM event listeners in the browser patcher.
- Clean up event listeners when nodes are replaced or unmounted.
- Extend the Jennifer-backed generator for simple event-handler expressions:
  - method/function field identifier, such as `@click="increment"`
  - no-argument call, such as `@click="increment()"`
- Keep checker/generator behavior aligned with the existing native-event
  validation.
- Add a generated counter fixture smoke compile.

Out of scope:

- Event modifiers
- Event object payloads
- Component events
- Keyed reordering
- `v-model`

Verification:

- Runtime tests for event listener attachment and cleanup.
- Generator golden tests for simple native event handlers.
- WASM smoke fixture for counter.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Implemented 2026-06-12:
  - Root runtime now exposes `EventBinding` with `func()` handlers,
    `On(name, handler)`, and `ElementWithEvents`.
  - DOM patching attaches native event listeners, updates handlers in place
    when event names stay stable, and cleans listeners recursively when nodes
    are replaced or unmounted.
  - The browser `syscall/js` adapter now bridges no-argument handlers to
    `addEventListener` and releases `js.Func` values during cleanup.
  - The Jennifer-backed generator emits native event bindings for callable
    identifiers and no-argument calls.
  - Checker and generator diagnostics now reject argument-bearing event calls
    and handlers whose signature is not `func()`.
  - Counter fixture generation compiles through the WASM smoke path.
- Still deferred to later tasks:
  - event payloads and event modifiers
  - component event payload compatibility
  - `v-model`

### 11. Implement Parent/Child Components

Goal: make typed component composition work.

Scope:

- Resolve component references from `.tue` files.
- Extend the Jennifer-backed generator for component allocation and prop-field
  initialization.
- Generate typed `tue.Prop[T]` values for child component fields through the
  same `internal/compiler/gogen` path.
- Support callback-style events or the reviewed event model from Task 5.
- Type-check prop existence and prop compatibility across components.
- Type-check event handler payload compatibility.
- Implement component VNode mount/update/unmount.

Out of scope:

- Named slots
- Async components
- Provide/inject

Verification:

- Parent/child positive fixture.
- Invalid prop and invalid event negative fixtures.
- WASM smoke fixture for parent/child interaction.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm component API and diagnostics before filling out directives.

### 12. Implement Core Template Directives

Goal: make internal-tool templates viable.

Scope:

- `v-if` and `v-else`
- `v-for` with required keys
- keyed child reordering
- `v-model` for:
  - text input
  - checkbox
  - select
- class binding
- style binding
- default slots
- trusted `v-html`

Suggested order inside this task:

1. `v-if` and `v-else`
2. `v-for` plus keyed patching
3. `v-model`
4. class/style binding
5. default slots
6. trusted `v-html`

Out of scope:

- named slots
- custom component `v-model`
- v-model modifiers
- generic third-party directives

Verification:

- Fixture tests for each directive.
- Negative tests for missing keys, invalid v-model targets, and untrusted
  `v-html`.
- WASM smoke fixtures for todo list, settings form, and basic dashboard.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Review after each directive subgroup if the diff becomes large.

### 13. Implement CSS and Scoped Styles

Goal: support global and scoped styles from `.tue` files.

Scope:

- Extract `<style>` and `<style scoped>`.
- Generate stable scoped attributes such as `data-tue-c-a13f`.
- Rewrite scoped selectors.
- Add scope attributes to generated VNodes.
- Emit combined `style.css`.

Out of scope:

- CSS modules
- PostCSS
- Sass/Less
- critical CSS

Verification:

- CSS rewrite tests.
- Render/codegen tests proving scope attributes are applied.
- WASM/browser smoke fixture with scoped and global styles.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm selector transform limitations are documented.

### 14. Implement Resource Primitive

Goal: provide one boring data-loading primitive for internal apps.

Design position:

- `Resource[T]` is Tue-specific. It should be aligned with official Vue patterns
  for async state, but not presented as an official Vue core primitive.
- In Vue terms, this is closer to a built-in composable that packages refs,
  watchers/effects, lifecycle cleanup, and a reload function.
- It is not the historical `vue-resource` HTTP plugin and must not grow HTTP
  client responsibilities.
- `Ref[T]` remains the normal local reactive state primitive. `Resource[T]` is
  only for async load state with value/error/loading/reload/cancellation
  semantics.

Scope:

- Implement constructor `ResourceOfFunc[T](ctx Context, load func() (T, error))
  *ResourceValue[T]`.
- Implement `Resource[T]`:
  - `Value() (T, bool)`
  - `Error() error`
  - `Loading() bool`
  - `Reload()`
- Use context cancellation on unmount.
- Make resource state reactive.
- Add simple error/loading helper patterns if needed by generated code.
- Document that `Value() (T, bool)` is intentionally not `Get() T`, because a
  resource may be unloaded, loading, or failed.

Out of scope:

- HTTP client APIs
- request/response interceptors
- cache deduping
- stale-while-revalidate
- optimistic mutations
- route prefetch
- Suspense integration

Verification:

- Pure Go tests for loading, success, error, reload, and cancellation.
- Race tests if goroutines are used.
- API tests or examples proving callers handle `Value()`'s missing-value case.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm API is enough for dashboard/data-entry examples before dev server.
- Confirm docs do not describe `Resource[T]` as official Vue API or as a
  replacement for fetch/Axios.

### 15. Implement Asset Pipeline

Goal: make referenced static assets work in development and production.

Scope:

- Resolve relative asset references from templates and CSS.
- Copy assets to output.
- Hash filenames for production.
- Rewrite template and CSS URLs.
- Support a `public/` directory.

Out of scope:

- image metadata
- inlining
- preload manifest
- advanced plugins

Verification:

- Asset resolution tests.
- Build output tests for hashed filenames and CSS `url()` rewrites.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm asset semantics before production build polish.

### 16. Implement Production Build

Goal: make `tue build` emit static deployable assets.

Scope:

- Emit:
  - `dist/index.html`
  - `dist/app.wasm`
  - `dist/tue_loader.js`
  - `dist/style.css`
  - `dist/assets/`
  - `dist/manifest.json`
- Build with `GOOS=js GOARCH=wasm`.
- Remove dev-only overlay/HMR code.
- Add content hashes where appropriate.
- Add a binary size report.
- Document compression recommendations without promising tiny bundles.

Out of scope:

- route-level code splitting
- lazy WASM chunks
- minifying arbitrary user JS

Verification:

- End-to-end fixture build.
- Dist file assertions.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm output structure and deploy story before dev server complexity.

### 17. Implement Dev Server and Error Overlay

Goal: make development tolerable and honest.

Scope:

- `tue dev` static server.
- File watcher.
- Compiler daemon.
- WebSocket reload channel.
- CSS hot update.
- WASM rebuild/reload.
- Error overlay for:
  - SFC parse errors
  - template parse errors
  - type errors mapped to `.tue`
  - CSS parse errors
  - Go/WASM build failures
  - WASM load failure
  - runtime panic
- State-preserving WASM reload for simple serializable refs.

Out of scope:

- true JavaScript-style module HMR
- browser devtools extension
- route-level HMR

Verification:

- Unit tests for reload classification.
- Integration test for style-only update if practical.
- Manual smoke test in browser for CSS update, template update, build error, and
  recovery.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm reload behavior is documented as state-preserving WASM reload, not
  full HMR.

### 18. Add `tue fmt`

Goal: format `.tue` files predictably.

Scope:

- Format Go script block with `gofmt`.
- Normalize block ordering if explicitly accepted.
- Format template conservatively.
- Leave CSS formatting alone unless a small formatter is already available or
  justified.

Out of scope:

- Prettier-compatible formatting
- custom style rules

Verification:

- Idempotence tests.
- Fixture formatting tests.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm formatting is conservative enough before encouraging use.

### 19. Build Realistic Example Apps

Goal: prove Tue on the target product surface.

Scope:

- Todo app.
- User table.
- Settings form.
- Dashboard page.
- Document the exact commands:
  - `tue check`
  - `tue dev`
  - `tue build`

Out of scope:

- consumer-style marketing examples
- router examples until router exists

Verification:

- All examples build.
- Browser smoke tests for core interactions.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Decide whether the product is useful enough before router work.

### 20. Add Small Explicit Router

Goal: make internal multi-page apps comfortable after the compiler/runtime are
stable.

Scope:

- Explicit route table.
- Static routes.
- `:id` params.
- links.
- not-found route.
- reactive current route state.

Out of scope:

- filesystem routing
- nested layouts
- route guards
- typed query helpers
- lazy chunks

Verification:

- Router unit tests.
- Example with `/`, `/users/:id`, and `/settings`.
- `go fmt ./...`
- `go test ./...`
- `tokensave sync`

Review:

- Confirm router is small enough and does not distort the component compiler.

## Current Migration Order

The repository has moved to a Tue-first migration. The old Vue-style
reflection/template interpreter and its `wasmgo` examples are no longer the
active compatibility baseline.

Current order:

1. Verify the `vue` snapshot branch before deleting old code.
2. Delete legacy Vue runtime files and retire examples that depend on them.
3. Decide the final Tue naming contract.
4. Rename the GitHub repository and Go module in mechanical PRs.
5. Rebuild the Tue implementation as reviewable PRs.
6. Rebuild examples as `.tue` projects when the compiler/runtime supports each
   concept.

Detailed migration steps live in `docs/tue-repo-migration-and-history-plan.md`.
