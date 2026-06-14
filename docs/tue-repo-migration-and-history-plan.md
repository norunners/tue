# Tue Repository Migration and Audit History Plan

Status: proposed

Date: 2026-06-13

This plan orders three related goals:

- rename the GitHub repository and Go module from `vue` toward `tue`
- turn the current large Tue implementation into reviewable commits or PRs
- delete the old Vue-style runtime and decide what happens to examples

The main constraint is that deletion, renaming, and history reconstruction are
all easy to make noisy. The chosen direction is Tue-first: preserve the current
work, delete the old Vue implementation before adding more Tue surface area,
then handle repository naming and reviewable history from that cleaner base.

## Recommended Order

1. Preserve the current full Tue work exactly as-is.
2. Delete the existing legacy Vue runtime and retire examples that depend on it.
3. Decide the canonical Tue naming contract.
4. Rename the GitHub repository, then update local remotes.
5. Make the Go module/import rename as its own mechanical PR.
6. Rebuild the Tue implementation as a stack of reviewable PRs from a clean
   base.
7. Rebuild examples as Tue examples when the compiler/runtime supports each
   concept.

The important rule is: do not preserve legacy Vue code just because it already
exists. It was temporarily isolated behind `legacy && js && wasm` build tags,
but the target product is Tue and the old runtime should stop shaping examples,
README guidance, or new review slices.

## Phase 0: Verify the Existing Snapshot

Goal: confirm the current 10k+ line working set is recoverable from the existing
`vue` snapshot branch before deleting legacy code.

Actions:

- Verify the `vue` branch exists locally or remotely.
- Fetch it if it exists only on the remote.
- Confirm it contains the intended full snapshot, including files that were
  untracked before the snapshot.
- Record the current base commit, branch, and remote.
- Run a file inventory before slicing:
  - `git status --short`
  - `git diff --stat`
  - `git diff --name-only`
  - `git ls-files --others --exclude-standard`
- Run `tokensave sync` after any branch fetch, checkout, or verification commit.
- If TokenSave branch comparison is needed, track the Tue branch explicitly
  before relying on branch graph output.

Useful checks:

```bash
git branch --list vue
git branch -r --list '*/vue'
git log --oneline --decorate --max-count=5 vue
git diff --stat vue...tue
```

Exit criteria:

- The `vue` snapshot branch is visible and recoverable.
- The snapshot is known to contain the full current implementation.
- The clean base for reviewable PRs is identified.
- No deletion, rename, or history surgery has happened yet.

## Phase 1: Delete Legacy Vue Runtime and Retire Legacy Examples

Goal: make Tue the only active implementation direction before adding more Tue
surface area.

Candidate legacy runtime files:

- `bus.go`
- `component.go`
- `context.go`
- `event.go`
- `option.go`
- `render.go`
- `subcomponent.go`
- `template.go`
- `vnode.go`
- `vue.go`

Legacy example directories:

- `examples/01-declarative-rendering/`
- `examples/02-attribute-data-binding/`
- `examples/03-conditionals-with-methods/`
- `examples/04-loops/`
- `examples/05-handling-user-input/`
- `examples/06-two-way-data-binding/`
- `examples/07-composing-with-components/`
- `examples/08-raw-html/`
- `examples/09-computed-properties/`
- `examples/10-watchers/`
- `examples/11-class-binding/`
- `examples/12-style-binding/`

Recommended scope:

- Delete the old reflection/template interpreter runtime files.
- Delete legacy examples that require `vue.New(...)`, `wasmgo`, or old template
  interpreter APIs.
- Keep or add small `.tue` fixtures under `testdata/fixtures/` as the near-term
  learning surface.
- Update README so it does not advertise deleted Vue APIs or old `wasmgo`
  examples as the main path.
- Keep historical mentions only when they explain migration context.

Out of scope:

- Rewriting all examples immediately.
- Moving runtime internals into `runtime/`.
- Changing module path or repository slug.
- Changing component contracts beyond what deletion requires.

Verification:

- Search for references to deleted APIs:
  `rg "vue\\.(New|El|Template|Data)\\(|wasmgo|legacy && js && wasm" --glob '!docs/**'`
- `go fmt ./...`
- `go test ./...`
- `GOOS=js GOARCH=wasm go test -c` for the runtime package.
- Generated fixture smoke build if generated fixtures exist in the slice.
- Lint if configured.
- `tokensave sync`

Exit criteria:

- The old Vue runtime files are gone.
- Legacy examples that require deleted APIs are gone or clearly moved out of
  the active examples path.
- The default repo build and tests do not depend on old Vue APIs.
- README points at Tue concepts or intentionally minimal WIP guidance.

Review:

- Implemented 2026-06-13:
  - Verified the remote `vue` snapshot branch and fetched it as `origin/vue`.
  - Deleted the legacy reflection/template interpreter runtime files.
  - Deleted legacy `wasmgo` example files that depended on `vue.New(...)`.
  - Replaced README guidance with current Tue compiler/runtime status and
    commands.
  - Updated migration and implementation docs so legacy Vue is no longer the
    active baseline.

## Phase 2: Decide the Naming Contract

Goal: separate product naming, repository naming, module path, and package name
so the rename does not become a mixed behavior change.

Decisions to make before repository/module rename work:

- GitHub repository slug: likely `norunners/tue`.
- Go module path: likely `github.com/norunners/tue`.
- Public root package name: intended to become `tue`.
- Temporary compatibility posture:
  - no compatibility guarantee for the old `vue.New(...)` API
  - old import path may rely only on GitHub redirects during transition
  - generated code should use the final module path once the module rename lands

Recommended split:

- Repository slug rename can happen outside code as an administrative step.
- Go module path/import rewrite should be one small mechanical PR.
- Package declaration rename from `package vue` to `package tue` should be
  separate if it would otherwise obscure runtime/compiler behavior changes.

Exit criteria:

- The canonical import path is written down.
- The generated-code import path is updated in one place or has a clear follow-up
  task.
- The remaining uses of `vue` are either historical documentation, language
  comparisons, or known transitional package names.

## Phase 3: Rename the GitHub Repository

Goal: put future PRs under the final repository name while keeping the code diff
small.

Actions:

- Rename the GitHub repository from `norunners/vue` to `norunners/tue`.
- Update the local remote:

  ```bash
  git remote set-url origin git@github.com:norunners/tue.git
  git remote -v
  ```

- Verify GitHub redirects from the old URL still work, but do not rely on them
  for generated code or documentation.
- Check repository settings after rename:
  - branch protection rules
  - default branch
  - GitHub Actions status checks
  - secrets and environments
  - deploy keys and webhooks
  - issue and PR templates
  - external badges
  - package documentation links

Exit criteria:

- Local `origin` points at `git@github.com:norunners/tue.git`.
- New PRs are opened against the renamed repository.
- Existing old URLs are treated as redirects only.

## Phase 4: Mechanical Module and Reference Rename PR

Goal: change names that must be globally consistent without mixing in behavior.

Scope:

- `go.mod` module path.
- Internal imports under `cmd/`, `internal/`, tests, and generated-code tests.
- Compiler constants that recognize the Tue runtime import path.
- Fixtures and `.tue` scripts that import the runtime.
- README install snippets, badges, and examples.
- Docs that still describe `github.com/norunners/vue` as the active path.
- Any generated files or golden-test expectations that embed the old import
  path.

Out of scope:

- Rewriting example behavior beyond removing old Vue references.
- Moving packages into `runtime/`.
- Changing component contracts.

Verification:

- `go fmt ./...`
- `go mod tidy` if imports or dependencies changed.
- `go test ./...`
- Lint if configured.
- Search for unintentional old-path references:
  `rg "github.com/norunners/vue|git@github.com:norunners/vue"`
- `tokensave sync`

Exit criteria:

- The repository builds under `github.com/norunners/tue`.
- Any remaining `github.com/norunners/vue` references are intentional historical
  notes or compatibility notes.

## Phase 5: Rebuild the Tue Work as Reviewable PRs

Goal: turn the existing large implementation into a readable audit trail without
pretending the work happened chronologically.

Recommended mechanics:

- Keep the preserved `vue` snapshot branch untouched.
- Create a clean slicing worktree from the target base.
- Pull files or hunks from the `vue` snapshot branch one slice at a time.
- Prefer one conceptual PR per implementation task.
- Keep generated fixtures and tests in the same PR as the feature they verify.
- After each PR branch, compare it against the `vue` snapshot branch to make sure
  no intended files were lost.

Useful commands while slicing:

```bash
git worktree add ../tue-slice main
git restore --source vue -- path/to/file
git add -p
git diff --cached --stat
git diff --cached
go fmt ./...
go test ./...
tokensave sync
```

Recommended PR stack:

1. Delete legacy Vue runtime and retire legacy examples.
2. Naming, skeleton docs, and `.tue` fixtures.
3. Repository/module/import rename if it is not already landed separately.
4. CLI shell for `tue check`, `tue build`, `tue dev`, and `tue fmt`.
5. SFC parser with source spans.
6. Template parser and template AST.
7. Script parser, component contracts, props, callbacks, and lifecycle method
   discovery.
8. Template checker and diagnostics.
9. Jennifer-backed Go generator and build cache.
10. Runtime foundation.
11. Reactivity core.
12. DOM patch boundary and static VNode mounting.
13. Reactive component rerenders.
14. Native DOM events and counter fixture generation.
15. Parent/child component support.
16. Core directives.
17. CSS/scoped styles.
18. Resource primitive.
19. Asset pipeline.
20. Production build.
21. Dev server and overlay.
22. Formatter.
23. Realistic Tue examples.
24. Router, only after core examples justify it.

Review rules:

- Each PR should explain what changed, what is intentionally deferred, and which
  fixtures prove the behavior.
- Avoid mixing mechanical rename, generated output, and behavioral runtime
  changes in the same PR.
- Prefer small PRs even if several are merged back-to-back.
- Do not squash all work into one final commit if the audit history is the goal.

Exit criteria:

- The final reconstructed branch is functionally equivalent to the preserved
  implementation for completed features.
- Each PR can be reviewed by topic.
- The implementation plan and high-level design stay aligned with the landed
  code.

## Phase 6: Rebuild Examples as Tue Examples

Goal: restore useful learning coverage without keeping dead Vue API examples.

Current legacy examples teach concepts that still matter:

- declarative rendering
- attribute binding
- conditionals
- loops
- event handling
- two-way binding
- component composition
- raw HTML
- computed values
- watchers
- class binding
- style binding

Recommended policy:

- Do not keep examples that require deleted legacy APIs in the active examples
  tree.
- Rewrite useful examples as `.tue` examples only when the compiler/runtime
  supports the relevant feature.
- Treat the old examples as a concept checklist, not source code to preserve.
- Prefer fewer working Tue examples over many stale examples.

Suggested replacement order:

1. Static hello and interpolation.
2. Counter with native click handling.
3. Parent/child props.
4. Conditionals and loops.
5. Class and style binding.
6. `v-model` or equivalent two-way binding.
7. Computed and watchers.
8. Raw HTML with explicit trusted-html semantics.
9. Realistic apps: todo, user table, settings form, dashboard.

Exit criteria:

- New examples build with documented `tue` commands.
- At least one browser smoke test covers interaction.
- Any intentionally removed old example has an explicit replacement or an
  explicit decision that it is no longer part of Tue.

## Potential Missing Steps

- Add or update CI before relying on PR checks for audit history.
- Decide merge strategy:
  - merge commits preserve PR grouping
  - squash merges preserve PR records but flatten commit details
  - rebase merges keep linear commits but can make PR boundaries less visible
- Add a release note or migration note explaining the rename.
- Update package documentation and badges after the repository rename.
- Update any local scripts, hooks, or TokenSave configuration that mention the
  old repository URL.
- Check whether consumers, examples, or docs rely on `go get
  github.com/norunners/vue`.
- Decide whether `package vue` becomes `package tue` in the naming PR or stays
  transitional until a later package-layout cleanup.
- Decide whether the final package layout stays as a root package or moves
  runtime internals under `runtime/`.

## Suggested Near-Term Next Step

Start with Phase 0 by verifying the existing `vue` snapshot branch is available.
Then make Phase 1, legacy Vue runtime and example retirement, the first
audit-history PR. Naming and repository/module rename work should follow from
that cleaner Tue-only baseline.
