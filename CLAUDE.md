# Sheaf

A lightweight native macOS Git tool: the IDEA Git tool window without the IDE, merging the dozens of repos and worktrees of one workspace into a single log. Pure Go; UI is mygo's native `ui` / `ui/editor`.

## Source of truth

Code and tests are the only truth for behaviour and UI. Docs say only what code cannot: intent, why, boundaries, where to start. If code is hard to read, improve the code; never explain it in a doc.

| Doc | Owner | Content |
| --- | --- | --- |
| `docs/product/overview.md` | agent | what it is, what it will not do, what is not built |
| `docs/product/views.md` | agent | windows, views, navigation purpose (`TestIA` checks the list) |
| `docs/product/decisions.md` | agent | product choices in force and the rejections worth keeping |
| `docs/product/code-map.md` | agent | Chinese guide for reading and changing the code by hand |
| `docs/agent/architecture.md` | agent | module boundaries and why, data layout, budgets |
| `docs/agent/verification.md` | agent | screenshot scenarios, fixtures, benchmarks, pitfalls |
| `ISSUES.md` | both | known problems, delete an entry once fixed |
| `docs/plans/` | agent | active plans only, delete when done |

The agent keeps `docs/product/` in step with every change, in the same piece of work; the user should not have to maintain it. The user may still edit it: when a doc disagrees with the code and this work did not cause the gap, the doc may be what the user wants, so report the gap and ask. Keep each fact in one doc. Do not record decision history; git has it.

## Commands

```sh
go mod download                                  # first time
go tool mygo dev                                 # run; rebuilds and restarts on Go changes
go tool mygo build -skip-dmg -skip-notarize      # build/darwin-arm64/Sheaf.app
go vet ./... && go test ./...                    # all checks
```

## Rules enforced by tests

Change the test when the rule changes; do not work around it.

- Package dependency graph, views never import each other, UI reaches business logic only through `session`: `archtest.TestDependencies`.
- Only `gitcli` starts git or glab: `archtest.TestOnlyGitcliRunsGit`. Git flags: `gitcli.TestCmdFlags`.
- `session` and `app` use only `gitcli` types: reads through `gitview` / `discover`, writes through `gitops`: `archtest.TestSessionDoesNotCallGitcli`.
- Writes go through the whitelist in `internal/gitcli/record.go` and `gitcli.Write`, and are logged to a file: `gitcli.TestWriteWhitelist`, `gitcli.TestLogFileRecord`, `archtest.TestWritesOnlyInGitcli`.
- UI text comes from i18n tables, both languages complete; colours come from `theme`: `archtest.TestNativeHygiene`, `archtest.TestNativeWords`. Icon sizes come from `theme`: `archtest.TestIconSizes`. Text fields show their caret (`widgets.Caret`): `archtest.TestTextFieldsShowTheirCaret`. Text contrast: `theme.TestContrast`.
- Every registered view and window is listed in `docs/product/views.md`: `native.TestIA`.

## Rules not covered by tests

- Write to a user's repo only with whitelisted commands, only from an action the user confirmed. Never auto-stash, never force-push, never touch a directory with uncommitted changes, except discarding the one file the user confirmed on the uncommitted page. App data stays in the app directory (the two CLI symlinks, /usr/local/bin/sheaf and /usr/local/share/zsh/site-functions/_sheaf, installed from Settings › General, are the exceptions).
- Business computation lives in `session` and below. The UI pages rows by visible range; long lists and diffs use mygo's virtual controls.
- Reversible actions (delete, overwrite, remove) get a 5-second undo Toast, not a confirmation dialog; irreversible ones (discarding uncommitted changes) are confirmed first.
- Budgets (a large workspace: 35 repos, 230k commits): memory ≤ 500 MB, first index ≤ 60 s, filter results within 50 ms, requirement switch within 100 ms.

## Code conventions

- Readable code over explanatory comments; comments say why, never what. Rules are table-driven Go tests, not prose.
- Do not cite docs from code comments: a comment must stand alone.
- Functions that may be slow or call git take `context.Context` first. Wrap errors with `fmt.Errorf("...: %w", err)`. No mutable package state beyond caches.
- UI controls come from `internal/native/widgets`, docked panes from `shell`. Icon-only buttons use `widgets.IconButton`; shortcuts use `widgets.Shortcuts`.
- A view implements `view.View`, is registered in `internal/native/views.go`, navigates through `view.Host.Go` with an intent.

## Done means

- `go vet ./... && go test ./...` pass.
- UI change: screenshots before and after, every difference intended (`docs/agent/verification.md`).
- Index or query change: run the benchmark on the large workspace at `$SHEAF_BENCH_ROOT` and stay within budget.
- Behaviour change: tests updated; a changed product choice or view updates `docs/product/`.
