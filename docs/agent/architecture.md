# Architecture

Why the modules are split this way, where data lives, and the budgets. For the package map see `docs/product/code-map.md`; the allowed dependency graph is `archtest.TestDependencies`.

## Process and windows

One process; one window and one `session` per workspace (index, filters, config, watcher, command recorder). Closing the window releases the index. Diff and settings are auxiliary windows that share their workspace's `session` and are owned by `internal/app`, so pages never hold an index of their own.

## Why these boundaries

- `session` holds all business state for a workspace, so windows do not each keep a copy.
- `view` is the contract between frame and pages. Pages do not import each other and navigate by intent, so each can be tested and replaced alone.
- `query` is pure logic with no internal imports, so filter rules are testable without git or UI.
- `gitcli` is the only git entry. It forces `--no-optional-locks`, `-c core.quotePath=false`, `LC_ALL=C` so reads leave the user's index alone and output stays stable.
- `gitview` holds on-demand reads that do not fit the index (diff and uncommitted changes).
- `gitops` separates planning a write from running it, so the user sees commands first and the rules can be tested on fixture repos.
- The command recorder belongs to the `session` and travels on the context, like the git concurrency limit; neither is package state.

## Git, glab and gh

- **CLI only, no go-git**: measured about 100x slower, misses commits, cannot open repos with worktreeConfig.
- **Concurrency cap is `NumCPU + 4`**, shared app-wide. First-index time matches no cap at all; a fixed 8 doubled the wait for opening a file during indexing.
- **Main branch detection**: manual setting, else `origin/master`, `origin/main`, `master`, `main`. `origin/HEAD` is not used: some repos point it at `master-yunxin` or `develop`, and it is written once at clone and goes stale.
- **Fast-forward without checkout uses `fetch refs/heads/<b>:refs/heads/<b>`**: full refs avoid a same-named tag, and no `+` means git rejects a non-fast-forward.
- **Git ≥ 2.36**: commit reads use `--diff-merges=first-parent`; telling a merge edited by hand from an automatic one uses `show --remerge-diff`, which re-merges in a temporary object directory and writes nothing to the repo (older git: merges stay unclassified, hidden).
- **glab and gh follow the same rules as git**: only `gitcli` starts them, commands and flags have their own whitelists, writes are logged, they share the concurrency cap, and each run has its own timeout. `gitops` selects gh only for origin's `github.com` host, glab otherwise; GitHub Enterprise is not supported. Both tools must pass `--repo=<host/group/project>` derived from origin, so multi-remote repos cannot silently select another project; an unrecognizable origin is skipped. glab's `mr create` requires `--push=false`; gh's `pr create` requires explicit `--head` and `--base`, because `--head` disables gh's implicit push/fork path. Only the explicit git push may publish a branch.
- **Tool availability is per row**: a missing gh must not prevent a GitLab row from running, or vice versa. The workspace's glab configuration is checked only when executable GitLab rows need it; gh uses its own login.
- **Known gap**: glab and gh themselves run read-only git that bypasses `gitcli` and the command log.
- **Finder launches have no Homebrew on PATH**, so glab and gh lookup also tries `/opt/homebrew/bin` and `/usr/local/bin`.
- **Credentials stay out of the UI and the log**: web URLs derived from origin are stripped of credentials before reaching the UI, and the command log records no environment. The user's environment is passed to glab and gh, so `GITLAB_TOKEN` and `GH_TOKEN` work. Prompts and colour are disabled for both tools.

## Index

- Columnar: 20-byte hashes, parents as per-repo indexes, changed files in one array, interned strings. Peak memory is under half of the naive structure.
- Immutable in memory: updates build a new version and swap a pointer, so readers take no lock.
- Branch relations and ahead / behind are computed from the index (ahead / behind for all worktrees is about 6 ms, against 500 ms+ through git). Diffs call git on demand.
- Incremental: each repo remembers its ref tips and reads only what is new. After a force-push or rebase, old commits stay in the index but results are taken from the reachable set.
- Added / removed line counts are not indexed; they are fetched when details open, to keep first index fast.
- Changing the untracked list filters a kept copy of the last scan (`scanned`) instead of rescanning. The cost: repos created while something is untracked appear at the next refresh.
- File watching uses mygo's watcher, since fsnotify on macOS takes one descriptor per file. `watch` only classifies changes; updates are debounced, and counts and the uncommitted page share one `git status` (`TestDirtyStatusOnce`).

## Syntax colouring

Native diffs and file text are coloured by mygo's editor languages.

## Data

App data is shared by the GUI and CLI, never stored in a user's repo.

```text
~/.sheaf/                                          dev build: ~/.sheaf-dev; isolated from each other
  settings.json  recent.json  window-state.json
  workspaces/<id>/workspace.json                    id = hash of the workspace's absolute path
  workspaces/<id>/native-layout.json                dock sizes, open state and each view's own divider sizes
  workspaces/<id>/commands.jsonl                    git / glab / gh write log, JSON lines, kept 30 days
  workspaces/<id>/lock                              kernel-held exclusive workspace lock
  cache/workspaces/<id>/index.bin                   safe to delete; carries a format version
  cache/workspaces/<id>/scan.json                    safe to delete; last discovered repos
  cache/workspaces/<id>/derived.bin                  safe to delete; any process may write, merging commit facts before atomic rename
  cache/workspaces/<id>/derived.bin.lock             serializes only derived cache merges, independent of the workspace lock
```

`SHEAF_HOME` overrides the root for either build. Names ending in ` Dev` or `-dev` select the development root. Workspace settings are written to a temp file and renamed, so an interruption cannot leave half a file.

The GUI acquires the workspace's exclusive file lock before opening a session, waiting at most 10 seconds. It releases the lock after the session's background work stops. Overlapping sessions for the same root share a process-owned reference count. The kernel releases locks on process exit; leftover lock files do not signal ownership. CLI configuration writes use the same lock without waiting; queries skip index and scan writes while the lock is held.

Derived facts depend only on commit IDs, so CLI and app sessions may share them while the workspace lock is held. Writers acquire a separate cache lock around reading, merging and atomically replacing `derived.bin`; rename alone would allow simultaneous writers to lose one another's facts. App computations coalesce saves within two seconds, and closing a session flushes pending facts. Unchanged facts do not rewrite the file. Immutable merge classifications and patch IDs remain reusable; line-tip comparisons expire after seven days without use, unless used by the current session. Merging preserves other sessions' recent comparisons without retaining obsolete line tips forever.

## Budgets

The app budget covers one workspace window with its auxiliary windows. The large-workspace runs used 35 repos, 31 linked worktrees and 230,019 commits. Each test ran twice with writes confined to temporary directories.

| Metric | Budget | App measurement 2026-11-05 | Large-workspace test |
| --- | --- | --- | --- |
| Memory | ≤ 500 MB | about 447 MB peak (`footprint`) | Index test 296–302 MB peak. Query test 361–362 MB peak. |
| First index | ≤ 60 s | about 13.5 s with app cache removed | Scan and build 13.73–15.47 s with an empty index. |
| Filter results | ≤ 50 ms | Regex about 55 ms, an accepted exception for opt-in queries that scan every message. | Slowest non-regex median 11.7–12.0 ms. Regex median 46.9–49.3 ms, max 57.1–58.5 ms. |
| Requirement switch | ≤ 100 ms | not measured in the app | 2026-11-13, six tracked requirements: overview, files and line counts 28–90 ms after prefetch; 200–900 ms when nothing is cached. |
| CLI with a warm cache | status/check ≈ 1.5 s; show/commits ≤ 1 s | 2026-11-17, 18 included repos / 36 working directories, 17 repos excluded; 4 active / 7 tracked requirements match 12 / 17 working directories; separate temporary data roots, existing config copied read-only | Three runs each: status 0.92–1.19 s (175.3–191.8 MiB peak RSS), status --all 1.07–1.39 s (187.9–201.9 MiB), check 0.86–0.93 s (165.7–186.0 MiB), show YT03847 0.50–0.64 s (150.3–151.0 MiB). Main baseline, one JSON run each: 2.08 / 2.17 / 2.16 / 0.71 s respectively; all four JSON results match after ignoring observedAt and normalizing temporary data paths. No fetch. |

A switch is fast only from caches: `git status` per working directory (dropped by the watch) and numstat per branch range (keyed by oids). Once the shown requirement is in, the page prefetches the other tracked, unarchived ones two git calls at a time.

The 5d memory figures are the test process's peak footprint from `/usr/bin/time -l`. The heap after loading the index snapshot was about 71 MB.

Not measured: cold disk after reboot, multiple windows, long runs. The index dominates the heap and lives with the window, so the app uses `SetGCPercent(50)` (`GOGC` wins) and releases free memory after first index settles.

## Test layers

- `query`, `engine`: table-driven unit tests; the cases are the rules.
- `gitcli`, `discover`, `index`, `gitops`, `session`: real fixture repos from `internal/testutil` in temp directories.
- `native`: `view/viewtest`, fixture sessions and mygo's UI test tools for interaction. Visuals are checked by screenshot.
- `archtest`: dependencies, git entry, UI text and colour rules.
