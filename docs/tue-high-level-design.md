# Tue: A Vue-Inspired Typed UI Framework

## 0. Positioning

Tue is not trying to replace Vue, React, Svelte, or the JavaScript frontend ecosystem.

Tue is for Go-heavy teams that want to build browser-based internal tools, dashboards, admin panels, infrastructure consoles, embedded-device UIs, and B2B workflow apps without maintaining a full JavaScript application stack.

The core promise:

Write Vue-like single-file components, type-check them with Go, compile them ahead of time, and ship a static Go/WASM app.

Tue should be judged against these alternatives:

- Go backend + React frontend
- Go backend + HTMX
- server-rendered Go templates
- go-app / Vugu / Vecty-style Go/WASM apps
- internal tools built with low-code platforms

Tue should not be judged against:

- Vue for consumer web apps
- React Native-style ecosystems
- SEO-heavy public sites
- animation-heavy marketing pages
- JavaScript module-HMR performance
- massive third-party frontend plugin ecosystems

## 1. Product Focus

### Primary Audience

Tue is for teams where:

- the backend is already Go
- the app is mostly CRUD, forms, tables, dashboards, filters, charts, and workflows
- type safety matters more than pixel-perfect consumer UX
- the team wants fewer languages and fewer moving parts
- deployment as static assets is attractive
- initial WASM size is acceptable for authenticated/internal apps

### Initial Use Cases

Good fit:

- admin panels
- infrastructure dashboards
- internal SaaS consoles
- observability UIs
- embedded appliance dashboards
- internal approval/workflow tools
- data-entry applications
- Go-based desktop/webview apps later

Bad fit:

- public marketing sites
- SEO-first content sites
- ecommerce storefronts
- mobile-first consumer apps
- animation-heavy apps
- apps requiring huge JS library ecosystems
- apps where sub-100 KB initial payload is mandatory

## 2. Core Philosophy

The architectural stance:

Templates are source code, not runtime data.

Tue templates compile into typed Go render code.

There is no runtime template interpreter and no string expression engine.

Example:

```vue
<UserCard :name="user.Name" :isAdmin="user.Role == 'admin'" />
```

compiles into generated Go that passes typed prop bindings to a generated
component adapter:

```go
components.TueUserCard(
	ctx,
	tue.PropOfFunc(func() string {
		return state.user.Get().Name
	}),
	tue.PropOfFunc(func() bool {
		return state.user.Get().Role == "admin"
	}),
)
```

The adapter is generated code, not a public author API. It can construct the
child component inside the child package so unexported prop fields remain usable.

The reason Tue exists is not "Go can render DOM."

The reason Tue exists is:

Vue-like authoring with Go-level type checking and Go-native deployment.

## 3. Explicit Non-Goals

The first serious version of Tue will not include:

- SSR
- hydration
- devtools browser extension
- plugin API
- Sass/Less integration
- critical CSS extraction
- route-level code splitting
- lazy WASM chunks
- advanced transitions
- Teleport
- KeepAlive
- large standard component library
- accessibility linter
- generic third-party directive system
- full Vite-equivalent HMR

These can be revisited only after the core loop is excellent.

The core loop is:

```text
edit .tue file
-> get useful type errors
-> rebuild quickly enough
-> browser updates predictably
-> app state is preserved when safe
-> production build emits static assets
```

## 4. File Format

Use `.tue` files.

Minimal supported blocks:

```vue
<template>
	<main class="page">
		<UserCard
			:user="user"
			:isAdmin="user.Role == 'admin'"
			@select="selectUser"
		/>

		<input v-model="query" />

		<ul>
			<li v-for="todo in filteredTodos" :key="todo.ID">
				{{ todo.Text }}
			</li>
		</ul>
	</main>
</template>

<script lang="go">
package pages

type Dashboard struct {
	userID tue.Prop[string] `prop:"userID,required"`

	todos         tue.Resource[[]Todo]
	user          tue.Ref[User]
	query         tue.Ref[string]
	filteredTodos tue.Computed[[]Todo]
	selectUser    func(User)
}

func (d *Dashboard) Init(ctx tue.Context) {
	d.user = tue.RefOf(User{})
	d.query = tue.RefOf("")

	d.todos = tue.ResourceOfFunc(ctx, func() ([]Todo, error) {
		return api.LoadTodos(d.userID.Get())
	})

	d.filteredTodos = tue.ComputedOfFunc(func() []Todo {
		todos, ok := d.todos.Value()
		if !ok {
			return nil
		}
		return filterTodos(todos, d.query.Get())
	})

	d.selectUser = func(u User) {
		d.user.Set(u)
	}
}
</script>

<style scoped>
.page {
	padding: 2rem;
}
</style>
```

Supported blocks for v0.1:

```text
<template>         required unless render is handwritten
<script lang=go>   required
<style>            optional
<style scoped>     optional
```

Deferred blocks:

```text
<style module>     later
<route>            later
<docs>             later
<i18n>             later
custom blocks      later
```

## 5. Public Component Model

Tue components should feel familiar to Vue users but compile into plain Go concepts.

Every `.tue` file defines a component with:

- a concrete component type
- optional `tue.Prop[T]` fields on that component type
- optional `Init(ctx tue.Context)` method
- generated render function
- optional scoped styles

Public API:

```go
type Prop[T any] interface {
	Get() T
	Watch(func(T)) func()
}

type Ref[T any] interface {
	Get() T
	Set(T)
}

type Computed[T any] interface {
	Get() T
}

type Resource[T any] interface {
	Value() (T, bool)
	Error() error
	Loading() bool
	Reload()
}

type Context interface {
	OnMounted(func())
	OnUnmounted(func())
	OnUpdated(func())
}

type VNode struct { ... }
type Attribute struct { ... }
type PropValue[T any] struct { ... }
type RefValue[T any] struct { ... }
type ComputedValue[T any] struct { ... }
type ResourceValue[T any] struct { ... }

func RefOf[T any](initial T) *RefValue[T]
func ComputedOfFunc[T any](compute func() T) *ComputedValue[T]
func ResourceOfFunc[T any](ctx Context, load func() (T, error)) *ResourceValue[T]
func Batch(fn func())
func Watch(effect func()) func()
func Mount(selector string, component *Comp) error
```

Generated code also needs runtime helpers that create concrete `Prop[T]`
values and runtime component descriptors:

```go
func CompOf(instance any, render func() VNode) *Comp
func PropOf[T any](value T) *PropValue[T]
func PropOfFunc[T any](get func() T) *PropValue[T]
```

`CompOf`, `PropOf`, and `PropOfFunc` are generated-code helpers, not the
normal component-author surface. `CompOf` owns the common creation sequence:
generated code allocates the component, fills prop fields, then passes the
component pointer and generated render function to the runtime. `CompOf` calls
optional `Init(ctx)` before any DOM is attached.

Component fields should prefer access interfaces such as `Prop[T]`, `Ref[T]`,
`Computed[T]`, and `Resource[T]`. Constructor/helper functions return concrete
runtime values such as `*PropValue[T]`, `*RefValue[T]`, and
`*ComputedValue[T]`; those concrete values satisfy the corresponding access
interfaces.

Vue alignment:

- `Ref[T]` is Tue's Go-shaped counterpart to Vue's `ref()` primitive. It is the
  core mutable reactive state type and remains part of the public component
  model.
- `Computed[T]`, `Watch`, and lifecycle callbacks are likewise Go-shaped
  versions of established Vue concepts.
- `Resource[T]` is intentionally Tue-specific. Official Vue core does not expose
  a stable `Resource` primitive; Vue applications usually model async data with
  refs, computed values, watchers, lifecycle hooks, and reusable composables.
  Vue also has `<Suspense>` for coordinating async dependencies, but that API is
  experimental and broader than Tue's first data-loading need.
- Tue `Resource[T]` should be documented as a standard async-state composable for
  Tue apps, not as Vue API compatibility. It is also not the old `vue-resource`
  HTTP plugin: it must not become an HTTP client, interceptor system, or request
  library. The caller supplies the loader function.

Official Vue reference points for this decision:

- Reactivity fundamentals: `https://vuejs.org/guide/essentials/reactivity-fundamentals.html`
- Composables: `https://vuejs.org/guide/reusability/composables`
- Watchers and cleanup: `https://vuejs.org/guide/essentials/watchers`
- Suspense: `https://vuejs.org/guide/built-ins/suspense`
- Historical `vue-resource` retirement note:
  `https://medium.com/the-vue-point/retiring-vue-resource-871a82880af4`

The value-construction functions use `...Of` or `...OfFunc` names because Go
cannot declare a public type/interface and a package-level constructor function
with the same name. Component fields should use the access interface names
(`tue.Ref[T]`, `tue.Computed[T]`, `tue.Resource[T]`); initialization code should
use `tue.RefOf`, `tue.ComputedOfFunc`, and `tue.ResourceOfFunc`.

Generated component constructors such as `NewApp()` keep `New<Component>` names
because they are package entry points for concrete components, not root runtime
value helpers.

Script contracts:

- `type <ComponentName> struct { ... }` stores props, component-local state, and
  handlers.
- `<ComponentName>` is the PascalCase component name, usually derived from the
  `.tue` file basename.
- Fields with type `tue.Prop[T]` are parent-provided props. Prop fields may be
  unexported; generated code is emitted into the component package and can fill
  them before initialization. The compiler reads their field names and `prop`
  tags to build the component prop contract.
- Components with no props omit `tue.Prop[T]` fields.
- `func (c *<ComponentName>) Init(ctx tue.Context)` is optional. If present,
  generated code calls it after allocating the component and filling prop
  fields.
- If a method named `Init` exists with any other signature, `tue check` must
  report a diagnostic instead of silently ignoring it.
- `Init` is for component-local state, refs, computed values, handlers, and
  lifecycle registrations. It must not assume rendered DOM is attached.
- `ctx.OnMounted` runs after generated runtime code attaches the component DOM.
- `ctx.OnUpdated` runs after a reactive update patches the component DOM.
- `ctx.OnUnmounted` runs when the component leaves the tree and is the cleanup
  point for timers, subscriptions, resources, and DOM-dependent effects.

Generated component creation follows this shape:

```go
c := &UserBadge{
	name:    tue.PropOfFunc(func() string { return parent.name }),
	isAdmin: tue.PropOf(false),
}
return tue.CompOf(c, func() tue.VNode {
	return userBadgeRender(c)
})
```

Prop fields are readable during `Init`, render, computed values, and effects.
Component authors do not construct props manually; generated code and the
runtime provide the concrete `tue.Prop[T]` values.

For cross-package components, parent generated code should call a generated
adapter in the child package instead of setting child fields directly.

The public `tue.Mount` API is separate from component initialization. `Mount`
attaches the root component tree to a DOM target. Component `Init` runs while the
runtime is constructing component instances inside that tree; `OnMounted` is the
first component hook that may rely on attached DOM.

Deliberately omitted from the first version:

- Provide / Inject
- complex emit interfaces
- advanced slot prop inference
- component refs
- global app plugins

Events can start simple:

```go
type UserList struct {
	selectUser func(User)
}
```

Template:

```vue
<UserCard @select="selectUser" />
```

The compiler checks that `UserCard` emits a compatible payload.

For the first component contract, callback fields are the event model. A field
with a function type on the component struct is a callable event/handler surface
that templates can reference. Tue should not add an explicit emits declaration
or emits API until callback fields prove insufficient.

## 6. Template Syntax for MVP

The first useful version supports only the core directives.

### Interpolation

```vue
<p>{{ user.Name }}</p>
```

Generated as escaped text.

### Static Attributes

```vue
<img src="/logo.svg" alt="Logo" />
```

### Bound Attributes

```vue
<img :alt="user.Name" />
```

### Events

```vue
<button @click="increment">+</button>
<button @click="select(todo)">Select</button>
```

The first runtime slice supports native DOM handlers that are Go `func()`
values:

```go
vue.On("click", c.increment)
```

Argument-bearing handlers such as `select(todo)` remain part of the later
component/directive work.

### Conditionals

```vue
<p v-if="user.Admin">Admin</p>
<p v-else>Member</p>
```

### Lists

```vue
<li v-for="todo in todos" :key="todo.ID">
	{{ todo.Text }}
</li>
```

Dynamic lists require keys.

### Form Binding

```vue
<input v-model="query" />
```

Supported first:

- text input
- checkbox
- select

Deferred:

- custom component `v-model`
- modifiers
- complex form normalization

### Components

```vue
<UserCard :user="user" @select="selectUser" />
```

### Slots

Slots are useful, but not required for v0.1.

Initial slot support can be limited to default slots:

```vue
<Card>
	<p>Body</p>
</Card>
```

Named slots come later.

## 7. Type Checking

This is the heart of Tue.

The compiler must type-check template expressions against Go symbols.

Required checks for the first serious release:

- prop existence
- prop type compatibility
- event existence
- event payload compatibility
- referenced state field existence
- method/function existence
- `v-for` item type inference
- key presence on dynamic lists
- `v-model` target is writable
- interpolation expression is valid Go
- component import resolution

Example failure:

```vue
<UserCard :isAdmin="'yes'" />
```

Should produce:

```text
src/components/UserCard.tue:4:12

<UserCard :isAdmin="'yes'" />
          ^^^^^^^^

Prop "isAdmin" expects bool, got string.
```

Generated Go-file errors are not acceptable as the primary diagnostic.

The compiler must map generated Go errors back to `.tue` source locations.

## 8. Compiler Architecture

Commands:

```bash
tue dev
tue build
tue check
```

Initial compiler stages:

1. discover `.tue` files
2. parse SFC blocks
3. parse template into AST
4. parse Go script block
5. build component symbol table
6. resolve component references
7. lower template directives
8. generate `*_tue.go` files with Jennifer
9. generate scoped CSS
10. run `gofmt`
11. run `go build` with `GOOS=js GOARCH=wasm`
12. emit `app.wasm`, `loader.js`, `style.css`, `manifest.json`

Generated structure:

```text
.tue-cache/
  components/
    UserCard_tue.go
    UserCard_render_tue.go
    UserCard_style.css
  manifest.json

dist/
  index.html
  app.wasm
  tue_loader.js
  style.css
  assets/
```

The generated code should be readable enough for debugging, but users should rarely need to inspect it.

### Go Code Generation

Generated Go should be emitted with Jennifer:

```text
github.com/dave/jennifer/jen
```

Rationale:

- Generated Go is core product surface for debugging and build errors, so it
  should be structured code, not hand-concatenated strings.
- Jennifer handles Go syntax construction and import qualification, including
  automatic import insertion for qualified identifiers.
- The generator can stay small and explicit while still producing readable Go
  files.

Generation rules:

- `internal/compiler/gogen` owns Jennifer usage. Parser, type-checker, runtime,
  and dev-server packages should not depend on Jennifer directly.
- Code generation consumes Tue compiler IR, not raw template strings. Earlier
  compiler stages own parsing, type checking, directive lowering, and source
  span tracking.
- Use Jennifer qualified identifiers for cross-package references so imports are
  generated instead of manually stitched.
- Use Jennifer render/save APIs for production generation. `%#v` rendering is
  acceptable in focused unit tests only.
- Still run `gofmt` or `go/format` over generated files as a final normalization
  and validation step.
- Preserve `.tue` source spans in Tue IR before code generation. Jennifer does
  not remove the need to map generated Go errors and type-check diagnostics back
  to `.tue` source locations.
- Keep raw string emission limited to file headers, comments, and literal text
  values. Do not build Go statements or declarations with ad hoc string
  concatenation.

Reference: `https://github.com/dave/jennifer`.

## 9. Runtime Architecture

The first runtime should be intentionally small.

Runtime modules:

- `tue/reactivity`
- `tue/vdom`
- `tue/dom`
- `tue/component`
- `tue/resource`
- `tue/style`

Deferred modules:

- `tue/router`
- `tue/devtools`
- `tue/ssr`
- `tue/transition`
- `tue/plugin`

### Reactivity

Supported primitives:

```go
count := tue.RefOf(0)

double := tue.ComputedOfFunc(func() int {
	return count.Get() * 2
})

tue.Watch(func() {
	fmt.Println(double.Get())
})
```

Required behavior:

- explicit dependency tracking
- computed caching
- batched updates
- component-scoped effects
- cleanup on unmount

Scheduler semantics:

- `Watch` runs once immediately to collect dependencies and returns a stop
  function. Ignoring the returned stop function is valid.
- `Prop.Watch` uses the same stop-function convention.
- Reads through `Ref.Get`, `Prop.Get`, and `Computed.Get` are tracked while a
  watcher or computed value is running.
- `Ref.Set` invalidates dependents every time it is called. Equality checks are
  intentionally not part of the first generic implementation.
- Outside `Batch`, queued watchers flush synchronously before `Set` returns.
- Inside `Batch`, watcher reruns are deduplicated and flush after the outermost
  `Batch` returns.
- `ComputedOfFunc` is lazy and cached. It recomputes on the next `Get` after one
  of its dependencies invalidates.
- Watchers and computed dependency subscriptions created during component
  `Init` are scoped to the component and stopped during `Unmount` before
  `OnUnmounted` hooks run.

Do not overbuild the reactive system. It needs to be predictable, not magical.

### Rendering

Use a VDOM first.

Direct DOM generation may be explored later only if benchmarks justify it.

VNode:

```go
type VNode struct {
	Type     VNodeType
	Tag      string
	Key      any
	Props    []Attribute
	Events   []EventBinding
	Children []VNode
	Text     string
	Component *Comp
}

type EventBinding struct {
	Name    string
	Handler func()
}
```

Patch rules:

- same type + same key: patch in place
- different type/key: replace
- keyed children: reorder
- unkeyed children: positional patch
- component node: update props and schedule child render

The runtime should optimize for boring CRUD apps, not benchmark heroics.

## 10. Styling

Supported in v0.1:

```vue
<style>
/* global */
</style>

<style scoped>
.card { padding: 1rem; }
</style>
```

Scoped CSS transform:

```css
.card {
	padding: 1rem;
}
```

becomes:

```css
.card[data-tue-c-a13f] {
	padding: 1rem;
}
```

Template:

```html
<div class="card">
```

becomes generated render code with:

```go
tue.Class("card")
tue.ScopeAttr("data-tue-c-a13f")
```

Deferred:

- CSS modules
- PostCSS
- Sass
- Less
- critical CSS
- CSS plugin API

Style HMR should be prioritized because it can be truly instant without WASM rebuilds.

## 11. Routing

Routing should not be part of the first version.

For v0.1, users can mount one root app and manage state manually.

For v0.2, add a small router.

Initial router scope:

```text
/
/users/:id
/settings
not found
```

Do not start with filesystem routing.

A simple explicit route table is easier to debug:

```go
tue.Router([]tue.Route{
	{
		Path: "/",
		Component: pages.Home,
	},
	{
		Path: "/users/:id",
		Component: pages.UserDetail,
	},
})
```

Filesystem routing can come later once the component compiler is stable.

Deferred router features:

- nested layouts
- typed query helpers
- route guards
- route-level data loading
- lazy chunks
- scroll restoration
- route block syntax

## 12. Data Loading

Keep one primitive:

```go
type UserDetail struct {
	userID tue.Prop[string] `prop:"userID,required"`
	user   tue.Resource[User]
}

func (u *UserDetail) Init(ctx tue.Context) {
	u.user = tue.ResourceOfFunc(ctx, func() (User, error) {
		return api.GetUser(u.userID.Get())
	})
}
```

`Resource[T]` is the standard Tue async-state helper, not an official Vue core
primitive. It exists to avoid hand-rolling the same `Ref` fields for value,
loading, error, reload, and cancellation in every dashboard/data-entry
component. It should feel familiar to Vue users because the same behavior could
be built with Vue refs, watchers, lifecycle hooks, and a composable, but Tue
chooses to ship one small built-in abstraction.

Required behavior:

- loading state
- error state
- success state
- cancellation on unmount
- manual reload

The value API is deliberately `Value() (T, bool)`, not `Get() T`, because a
resource can be loading, failed, or not yet loaded. Callers must handle the
missing-value case explicitly instead of treating async state like a normal
`Ref[T]`.

Initial API:

```go
type Resource[T any] interface {
	Value() (T, bool)
	Error() error
	Loading() bool
	Reload()
}
```

Deferred:

- Suspense component. This is the Vue-aligned tree-level async coordination
  concept, but it should stay deferred until the basic resource primitive and
  render lifecycle are stable.
- cache deduping
- stale-while-revalidate
- route prefetch
- optimistic mutations
- global query cache
- HTTP-client concerns such as request interceptors, base URLs, or Axios/fetch
  wrappers

Tue should not become React Query in the first year.

## 13. Dev Server and HMR

The honest HMR model remains:

```text
style update:
  true hot swap, no WASM rebuild

template/script update:
  rebuild WASM, reload WASM instance, restore safe state

unsafe shape update:
  remount affected component or full reload
```

Do not pretend Go/WASM can provide normal JavaScript module HMR.

Dev server responsibilities:

```text
tue dev
  |-- static HTTP server
  |-- WebSocket reload channel
  |-- file watcher
  |-- compiler daemon
  |-- Go build runner
  |-- CSS hot swap
  |-- WASM reload
  `-- error overlay
```

HMR classes:

1. Style-only update

   ```text
   edit <style>
   -> compile CSS
   -> send css-update
   -> replace style tag
   -> preserve state
   ```

2. Template update

   ```text
   edit <template>
   -> regenerate render Go
   -> rebuild WASM
   -> snapshot supported state
   -> reload WASM
   -> restore compatible state
   -> rerender
   ```

3. Script body update

   ```text
   edit function body
   -> rebuild WASM
   -> attempt state restore
   -> rerender
   ```

4. Shape update

   ```text
   edit prop / component state shape
   -> rebuild WASM
   -> remount affected subtree
   -> full reload if needed
   ```

State preservation should be modest.

Supported initially:

- `tue.Ref[string]`
- `tue.Ref[int]`
- `tue.Ref[bool]`
- `tue.Ref[float64]`
- `tue.Ref[structs/slices/maps that encode to JSON]`

Not preserved:

- DOM refs
- functions
- channels
- goroutines
- `syscall/js` values
- open resources
- in-flight requests
- non-serializable values

This limitation should be documented clearly.

## 14. Error Overlay

The error overlay is not optional. It is core product value.

Required diagnostics:

- template parse errors
- Go type errors mapped to `.tue`
- missing prop
- prop type mismatch
- missing event handler
- invalid event payload
- invalid `v-model`
- missing `v-for` key
- CSS parse errors
- WASM load failure
- runtime panic

Good error:

```text
src/components/UserCard.tue:4:14

<UserCard :isAdmin="'yes'" />
          ^^^^^^^^

Prop "isAdmin" expects bool, got string.
```

Bad error:

```text
.tue-cache/UserCard_render_tue.go:83: cannot use "yes" as bool
```

Tue should compete on diagnostics before it competes on features.

## 15. Assets

Initial asset support should be basic.

Supported:

```html
<img src="./logo.svg" />
```

Compiler emits:

```text
dist/assets/logo.8f3a.svg
```

Supported in v0.1:

- hashed filenames
- public directory
- CSS `url()` rewriting

Deferred:

- image dimension metadata
- preload manifest
- automatic inlining
- advanced asset plugins

## 16. Build Modes

Development:

- unminified WASM
- debug metadata
- source mapping
- runtime checks
- HMR client
- panic overlay

Production:

- optimized WASM
- CSS extracted
- HMR removed
- content hashes
- static dist output
- compressed asset guidance

Do not promise tiny bundles. Instead, document the tradeoff honestly.

Production command:

```bash
tue build
```

Output:

```text
dist/
  index.html
  app.wasm
  tue_loader.js
  style.css
  assets/
```

## 17. Security

Security rules should be simple and strict.

Default:

- text interpolation is escaped
- attribute values are escaped
- event handlers must be Go functions
- native DOM event handlers in the first runtime slice must have signature
  `func()`
- string JavaScript handlers are forbidden

Raw HTML requires an explicit trusted type:

```go
type TrustedHTML string
```

Template:

```vue
<div v-html="trustedHTML" />
```

Allowed only when:

```go
trustedHTML tue.TrustedHTML
```

Not allowed:

```go
string
```

URL sanitization can start with warnings, not a full policy engine.

## 18. CLI

Initial commands:

```bash
tue create my-app
tue dev
tue build
tue check
tue fmt
```

Deferred:

- `tue preview`
- `tue test`
- `tue generate`
- `tue inspect`
- `tue doctor`

`tue check` should run:

- SFC parse
- template type-check
- Go check
- CSS check
- component contract check

## 19. Project Structure

Recommended app:

```text
my-app/
  go.mod
  tue.config.go
  index.html
  src/
    main.go
    app.tue
    components/
      UserCard.tue
      DataTable.tue
    pages/
      Dashboard.tue
      Users.tue
    api/
      client.go
  public/
    favicon.svg
```

`src/main.go`:

```go
package main

import (
	"myapp/src"
	"tue"
)

func main() {
	tue.Mount("#app", src.NewApp())
}
```

`tue.config.go`:

```go
package main

import "tue/config"

func Configure(c *config.Config) {
	c.App.Root = "src/app.tue"
	c.Build.OutDir = "dist"
	c.Dev.Port = 5173
}
```

## 20. Implementation Phases

### Phase 1: Tiny Vertical Slice

Goal:

One `.tue` component renders in the browser.

Scope:

- parse `.tue`
- extract blocks
- compile template interpolation
- generate Go render file
- mount static DOM
- build `app.wasm`
- load with small JS loader

Phase 1 deliberately excludes reactivity; Task 9 starts the Phase 2 runtime
reactivity slice.

### Phase 2: Basic Reactivity and Patching

Goal:

A counter app works.

Scope:

- `Ref`
- `Computed`
- `Watch`
- component state
- VDOM patch boundary
- reactive render scheduling
- native event handlers
- lifecycle cleanup

Demo:

```vue
<button @click="increment">{{ count }}</button>
```

### Phase 3: Useful Component System

Goal:

Parent/child components work with typed props and events.

Scope:

- props
- event callbacks
- component imports
- prop type checking
- event signature checking
- source-mapped errors

Demo:

```vue
<UserCard :user="selectedUser" @select="selectUser" />
```

### Phase 4: Template Completeness for Internal Apps

Goal:

Forms, lists, conditionals, and basic dashboards are viable.

Scope:

- `v-if`
- `v-for`
- keyed lists
- `v-model`
- class binding
- style binding
- default slots
- scoped CSS

Demo:

- todo app
- user table
- settings form
- dashboard page

### Phase 5: Dev Server

Goal:

Development feels tolerable.

Scope:

- `tue dev`
- file watcher
- CSS hot update
- WASM rebuild/reload
- error overlay
- preserve simple `Ref` state when safe

Do not overpromise "true HMR."

Call it:

state-preserving WASM reload

### Phase 6: Production Build

Goal:

Static deployment works reliably.

Scope:

- `tue build`
- content hashes
- CSS extraction
- asset copying
- production loader
- documented compression recommendations
- binary size report

### Phase 7: Small Router and Resource Polish

Goal:

Internal multi-page apps are comfortable.

Scope:

- explicit router
- route params
- links
- not-found route
- resource loading
- cancellation
- reload
- error/loading helpers

Filesystem routing remains deferred.

## 21. Success Criteria

Tue is worth continuing only if the early version proves these things:

### Developer Experience

- A Go developer can understand a `.tue` file in minutes.
- Template errors point to the correct source location.
- Generated Go errors rarely leak through.
- The dev loop is predictable.
- State-preserving reload works for simple cases.

### Product Usefulness

- A realistic internal dashboard can be built without JavaScript application code.
- Forms are not painful.
- Lists and tables are not painful.
- API loading is straightforward.
- Static deployment is boring.

### Technical Viability

- WASM size is documented and acceptable for internal apps.
- DOM patching is fast enough for CRUD UIs.
- The runtime does not become huge.
- The compiler does not become impossible to maintain.
- The project can survive without imitating every Vue feature.

## 22. Final Architectural Stance

Tue should be:

- Vue-like at the authoring layer
- Go-like at the type-checking layer
- AOT-compiled at the build layer
- VDOM/reactive at the runtime layer
- WASM-aware at the dev-server layer
- honest about tradeoffs at the product layer

The most important product decision is:

Tue is not a general frontend framework. Tue is a Go-first UI compiler for internal applications.

The most important technical decision is:

AOT everything that benefits from Go's type system. Interpret nothing unless it is explicitly a dev-only fallback.

The most important scope decision is:

Build one excellent core loop before building an ecosystem.

The first version should not feel like a smaller Vue.

It should feel like the best way for a Go team to build a typed internal browser app without adopting a JavaScript frontend stack.
