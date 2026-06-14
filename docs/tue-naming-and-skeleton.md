# Tue Naming and Skeleton

Status: proposed for Gate 1 review.

Date: 2026-06-11

## Decisions

- The product, command, and generated-component API name is Tue.
- The CLI command name is `tue`.
- The final public root package name is `tue`.
- The current module path stays `github.com/norunners/vue` for Task 1.
- Legacy Vue-style runtime files have been removed from the active tree.
  Remaining root files are the current Tue runtime, but they still declare
  `package vue` until the package/module rename lands as a separate mechanical
  change.
- Until the module path changes, `.tue` fixture scripts import the current module
  as `tue`:

  ```go
  import tue "github.com/norunners/vue"
  ```

- The module path should be revisited as a dedicated migration step. The legacy
  examples no longer block the rename, but the module/import rewrite should stay
  separate from compiler and runtime behavior changes.

## Package Names

The intended public root API is package `tue`. It owns the component-author
surface:

- `Prop[T]`
- `Ref[T]`
- `RefOf`
- `Computed[T]`
- `ComputedOfFunc`
- `Context`
- `Mount`
- `Resource[T]`
- `ResourceOfFunc`
- `Watch`

Generated code also uses root runtime helpers:

- `Comp`
- `CompOf`
- `VNode`
- `Attribute`
- `PropOf`
- `PropOfFunc`

`Comp`, `CompOf`, `PropOf`, and `PropOfFunc` are generated-code contract
helpers. Component authors normally declare concrete component structs and use
access interfaces such as `Prop[T]`, `Ref[T]`, `Computed[T]`, `Context`, and
lifecycle hooks directly.

The interface/type names are distinct from constructor names. Go cannot expose
both `type Ref` and `func Ref` in the same package, so construction uses
`RefOf`, `ComputedOfFunc`, and `ResourceOfFunc`. Component fields should use
access interfaces such as `tue.Ref[T]`, `tue.Computed[T]`, and
`tue.Resource[T]`.
Constructor/helper functions return concrete values such as `*tue.RefValue[T]`,
`*tue.ComputedValue[T]`, and `*tue.PropValue[T]`.

Generated component constructors such as `NewUserBadge()` keep the
`New<Component>` shape because they are concrete component package entry points,
not root runtime value helpers.

`Ref[T]` is intentionally aligned with Vue's `ref()` concept: it is the normal
mutable reactive state primitive. `Resource[T]` is different. It is a Tue
async-state helper for value/loading/error/reload/cancellation, closer to a
built-in composable than to Vue core API. It should not be described as official
Vue compatibility, and it is not related to the old `vue-resource` HTTP plugin.

Runtime implementation packages should use concrete package names that match
their role:

- `component`
- `dom`
- `reactivity`
- `resource`
- `style`
- `vdom`

These packages should live under `runtime/` in this repository until there is a
clear reason to make them part of the public import surface. This intentionally
differs from the high-level design shorthand such as `tue/reactivity`; that
shorthand describes conceptual runtime modules, not a committed Go import path.

Compiler and development-server packages remain internal:

- `internal/compiler/sfc`
- `internal/compiler/template`
- `internal/compiler/script`
- `internal/compiler/symbols`
- `internal/compiler/typecheck`
- `internal/compiler/gogen`
- `internal/compiler/css`
- `internal/compiler/assets`
- `internal/compiler/build`
- `internal/devserver/server`
- `internal/devserver/watcher`
- `internal/devserver/overlay`

## Fixture Shape

Initial `.tue` fixtures live under `testdata/fixtures/`.

Flat fixtures cover single-component behavior:

- `static_hello.tue`
- `interpolation.tue`
- `counter.tue`

Directory fixtures cover component-resolution behavior:

- `parent_child_props/`
- `invalid_prop_type/`

Each fixture script uses this provisional component contract:

- The component type is named `<ComponentName>`.
- `<ComponentName>` is the PascalCase component name, usually derived from the
  `.tue` file basename.
- Props are optional and are declared as fields on the component type using
  `tue.Prop[T]`. Prop fields may be unexported because generated code is emitted
  into the component package and fills them before initialization.
- Components with no props omit `tue.Prop[T]` fields.
- Component-local initialization is optional and uses
  `func (c *<ComponentName>) Init(ctx tue.Context)`.
- A method named `Init` with any other signature is an error.
- Generated code allocates the component, fills prop fields, then calls `Init`
  if the method exists.

For example, `UserBadge.tue` defines `UserBadge` with fields such as
`name tue.Prop[string]`. No parser, code generator, or runtime behavior is
implied by these fixtures yet; they define the source shape that later tasks
must parse, type-check, and diagnose.
