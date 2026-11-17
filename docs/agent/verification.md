# Verification

How to see and measure a change beyond `go vet ./... && go test ./...`.

## Screenshots

Regenerate the two README requirement images with `shots=$(mktemp -d "${TMPDIR:-/tmp}/sheaf-readme-shots.XXXXXX") && scripts/mkfixture.sh "$shots/fixture" && mkdir "$shots/out" && SHEAF_README_WS="$shots/fixture/ws" SHEAF_SHOTS="$shots/out" go test ./internal/native -run '^TestReadmeShots$' && for lang in zh-CN en; do sips -z 1000 1600 "$shots/out/requirement-$lang.png" --out "docs/images/requirement-$lang.png"; done`. The test follows PAY-142, configures the fixture's release → master flow, and captures the settled 1440×900 workspace in both languages at 2x before resizing to 1600×1000; it skips unless both environment variables are set.

Build a temporary binary, capture one 2x PNG, exit:

Export `SHEAF_BENCH_ROOT` with the path to a large local workspace before running captures or benchmarks that use it.

```sh
smoke_root=$(mktemp -d)
go build -o "$smoke_root/mg" . && SHEAF_HOME="$smoke_root/data" SHEAF_SMOKE=1 SHEAF_CAPTURE=/tmp/requirements.png SHEAF_WORKSPACE="$SHEAF_BENCH_ROOT" "$smoke_root/mg"
```

Scenario variables are parsed in `internal/native/smoke.go`; capture is in `internal/app/nativewin.go`.

| Variable | Effect |
| --- | --- |
| `SHEAF_NATIVE_VIEW=track` | the requirement page, also the default |
| `SHEAF_NATIVE_Q=<text>` | opens a requirement by number (tracked or not), otherwise a temporary branch result |
| `SHEAF_NATIVE_ROW=<n>` | scrolls the requirement stream to repo row n from 0 |
| `SHEAF_NATIVE_WIN=settings` | open and capture the settings window |
| `SHEAF_NATIVE_TAB=general\|me\|base\|index\|glab` | settings page, with `WIN=settings` |
| `SHEAF_NATIVE_WAIT=<ms>` | capture delay, default 1500 |

- The workspace window waits for its first paint (`h.Painted()`) and then the delay. Other windows wait only the delay, so raise `WAIT` when a capture is blank or still loading.
- Run one instance at a time. Scenario variables are read at startup.
- App data defaults to `~/.sheaf` (development builds: `~/.sheaf-dev`); indexes live under `<root>/cache/`. Set `SHEAF_HOME` to a fresh temporary directory for every fixture or smoke run, and clean only that directory.
- Parallel workers need separate `SHEAF_HOME` directories and capture paths.
- Language comes from `lang` in that directory's `settings.json`; window size from `window-state.json`. Compare before and after at the same window size.

Smoke cannot click. For hover, popover, menu and focus states, a package's `TestShots` drives the page with `ui.Tester` and writes PNGs at 2x (`viewtest.Harness.Shot`):

```sh
mkdir -p <dir> && SHEAF_SHOTS=<dir> go test ./internal/native/... -run 'TestShots$'
```

`Shot` advances past panels' entrance motion, or they would be drawn see-through; a test reading geometry or pixels of a panel must call `TT.Advance` too. Without the variable the scenes run as plain tests and write nothing. Real pointer drags still need a person.

For the complete workspace frame at 1440×900, run `SHEAF_SHOTS=<dir> go test ./internal/native -run '^TestShots$'`. It captures `workspace-requirement-1440-zh-Hans.png` and `workspace-requirement-1440-en-US.png`, including the title and status bars. The left icon strip and repo-scope control are gone; the requirement rail stays accessible even when an older saved layout hid it. Compare the remaining rail, tabs and diff content at the same size and locale; removing the strip gives the content more horizontal room.

For independent diffs, use `SHEAF_SHOTS=<dir> go test ./internal/native/diffwin -run '^TestShotsLoaded$'` after the window cleanup is merged. These scenes load commit, aggregate and uncommitted sources in both modes and languages. Requirement commit activation is covered by `track.TestCommitListKeepsSelectionAndOpensFromKeyboardOrDoubleClick`; `track.TestResolvedCommitOpensOnlyItsRemergeDiff` checks that a resolved merge opens only its own remerge changes, excluding the branch range and worktree.

For the requirement page, run `go test ./internal/native/track -run '^TestShots/requirement$'` with `SHEAF_SHOTS` set to an existing directory. These scenes use the five-repo fixture with 23 committed files and one separate worktree file:

| Scene | Capture |
| --- | --- |
| `req-stream` | first screen of the diff stream |
| `req-pinned` | a file scrolled under its pinned repo header |
| `req-uncommitted` | the last committed file of pay-core followed by its HEAD-to-worktree section |
| `req-outline-jump` | a file reached from the outline |
| `req-commits` | the commits tab, with edited merges marked |
| `req-landed` | per-commit diffs of a landed repo row |
| `req-empty` | a requirement with no matching changes |

Filenames end in `-<locale>.png` (`zh-Hans` or `en-US`). `req-stream`, `req-pinned` and `req-uncommitted` include widths `820`, `1180` and `1440` before the locale; `req-commits` includes `1180` and `1440`. Compare the title bar at the same width and locale when checking counts or punctuation. In `req-uncommitted`, the reminder-colour worktree label shares one pinned row with the repo name and fits at 820; its file header has no redundant uncommitted badge. The changes tab stays at 23 and +54 −39; the outline adds an Uncommitted subsection under pay-core. `TestUncommittedRepoRailJumpsToPreview` checks the lower-left repo links in both all-changes and uncommitted modes. `TestUncommittedRepoHeaderStaysPinned` captures long uncommitted files scrolled below their pinned repo and file headers in both languages at all three widths.

`TestRepoHeaderWithLongNames` uses a long project name, linked worktree directory and branch together. Its `repo-long-names-<width>-<locale>` captures cover both languages at 820, 1180 and 1440. Names truncate without overlapping; the uncommitted status and file count, stage and ahead/behind counts stay visible. Hover descriptions retain full names and the worktree path.

For the uncommitted page, run `SHEAF_SHOTS=<dir> go test ./internal/native/track -run '^TestShots/uncommitted$'`. The scenes cover two repos and three working directories, including staged and unstaged edits of the same file and an untracked file. Captures are `uncommitted-{stream,files,selected-files,selected-stream,empty}-<width>-<locale>.png`, at widths 820 (narrow; the tables must not scroll sideways), 1180 and 1440 in both languages.

## Fixture workspace

Review refresh is exercised by `TestReview*` in track, `TestStreamPauseKeepsReadContentAndStopsNewLoads` in diffview and `TestWorkspacePassesActualVisibilityToReview` in native. Cover repeated saves, external commits, unrelated directories, newly dirty repos, an empty uncommitted page, leaving the diff tab and hiding the window. Inactive but visible windows retain their review. `review-reminder-<width>-<locale>` captures the constant yellow reminder and change selector at 820, 1180 and 1440 in both languages; `review-changed-{requirement,uncommitted}` captures real file edits deferred until refresh.

Batch git, merge and MR change repos or remotes. Try them on a disposable workspace:

```sh
scripts/mkfixture.sh /tmp/sheaf-fx    # deletes the target directory first
SHEAF_HOME=$(mktemp -d) SHEAF_WORKSPACE=/tmp/sheaf-fx/ws go tool mygo dev
```

It creates `ws` (clones and worktrees) and `remotes` (bare origins): six demo repos named `payment-api`, `checkout-web`, `ledger-service`, `risk-engine`, `receipt-worker` and `gateway`, covering clean worktrees, uncommitted work, branches without a worktree, conflicts, merged into release and merged into master. The requirement defaults to `PAY-142` and the branch to `feat-PAY-142`; override with `SHEAF_FIXTURE_REQUIREMENT=` and `SHEAF_FIXTURE_BRANCH=`. The demo HTTPS origins are rewritten to local bare repositories, so fetch touches only local remotes.

Run the `internal/gitops` and `internal/session` fixture tests first. Real MR checks need an explicitly named test project.

## CLI

CLI checks never need a GUI. Run `go test ./internal/cli -count=1` for configuration round trips, scopes, history paging, check exit codes, partial fetch failure, structured errors, idempotent archive commands, and queries/writes while a separate process holds the workspace lock. Tests isolate Git configuration and use temporary `SHEAF_HOME` directories; default-root tests instead use a temporary `HOME`. The lock test kills its holder to verify automatic release.

After CLI changes, run `go test ./internal/cli -count=1` (including `TestHelp*`, `TestVersion` and `TestArgumentErrorUsage` for command-table consistency, executable help examples, JSON values, line widths and side effects), then `go vet ./... && go test ./...`.

Run the CLI block in `README.md` from a source checkout. It builds `cmd/sheaf`, creates a disposable workspace with `scripts/mkfixture.sh`, and points `SHEAF_HOME` at temporary app data. JSON stdout should parse as exactly one document; fetch progress appears only on stderr. Help examples run automatically on temporary fixture workspaces in `TestHelpExamples`.

Check human output with `status`, `check`, `check PAY-142`, `check --stage release`, and `show PAY-142` on the same fixture. Status and check show one line per requirement and gap; only targeted checks add missing commits with short SHAs. Show retains repository paths, flow and stage evidence. Verify that empty optional fields are omitted and a missing requirement prints only its error and next step. JSON retains the full evidence.

For a local release smoke check, run `scripts/release.sh` to build and verify the arm64 app and DMG. Check `lipo -archs` for the app executable and its `Contents/Resources/sheaf`: both must contain only arm64. Run the bundled CLI with `--version`, then against the disposable fixture, with temporary `SHEAF_HOME` app data. The CLI and zsh completion ship inside the app and are installed from Settings › General.

When `$SHEAF_BENCH_ROOT` is available, use a separate temporary `SHEAF_HOME` to prime the CLI cache, track representative requirements, and measure `status` / `check` (≤ 3 s with cache) and `show` / `commits` (≤ 1 s). Keep the workspace configuration equivalent to the benchmark's intended scope. CLI adapters do not read file diffs; the first `--fetch` also discovers repositories before selecting fetch targets.

## Benchmarks

```sh
go test -run Real -v ./internal/index ./internal/engine
```

Not part of normal test runs. Budgets are in `architecture.md`.

## Pitfalls

- **Old instance still running**: quit it before trusting a changed variable or state.
- **After a hard kill**: the instance stays in the open list and may be restored on next launch.
- **Background work and closing**: use `Host.Update` to return to the main thread; `Close` cancels loads and stops timers.
- **Auxiliary windows**: request through `view.Host.Windows()`. A page-made session indexes twice and escapes lifecycle management.
- **Narrow windows**: minimum workspace width is 1180. Check truncation, scrolling and selection after changing panes, columns or row height.
- **New views**: call `h.Painted()` on first paint; workspace captures wait for that signal.
