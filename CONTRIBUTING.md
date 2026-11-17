# Contributing to Sheaf

Sheaf is a native macOS Git tool written in Go. Development requires macOS,
Apple's command line tools (`xcode-select --install`), the Go version declared
in [go.mod](go.mod), and Git 2.31 or later. Creating GitLab merge requests also
requires [glab](https://gitlab.com/gitlab-org/cli), authenticated for your host.

Read the [project rules](./CLAUDE.md) before making changes. The
[code map](docs/product/code-map.md) is the starting point for navigating the
codebase; the [product overview](docs/product/overview.md) explains its scope.

## Build and run

```sh
go mod download
go tool mygo dev
go tool mygo build -skip-dmg -skip-notarize
```

The development command rebuilds and restarts the app as Go files change. The
build command produces `build/darwin-arm64/Sheaf.app`. Sheaf supports Apple
Silicon only. Production builds use ad hoc
signing when no signing identity is configured; an Apple Developer account is
not required.

To build the release app and DMG:

```sh
./scripts/release.sh
```

The script requires macOS, explicitly uses ad hoc signing, skips notarization,
and verifies the app's architecture, bundled CLI, signature and DMG. Output goes
to `build/release/darwin-arm64/`, using the name and version in `mygo.json`. It
also copies the DMG to `build/release/sheaf-<version>.dmg`, the release upload
name, and writes a `.sha256` beside it. Install the bundled CLI and zsh
completion from **Sheaf → Settings → General → Install**.

## Check changes

```sh
go vet ./... && go test ./...
```

CI runs the same checks on macOS. Unit tests use temporary Git fixtures and UI
testers that do not open application windows. Tests against a real workspace
skip unless the benchmark environment variables are set. See
[verification](docs/agent/verification.md) for screenshot and benchmark commands.
UI changes require before and after screenshots; index or query changes require
the documented benchmarks. Update product documentation when a product decision
or view changes, and keep behavior rules in tests.

## Repository writes are a hard boundary

Every Git or glab write must pass through the command and argument whitelist in
`internal/gitcli/record.go` and the `gitcli.Write` path, with a persistent command
log. Do not bypass this boundary by starting a subprocess elsewhere. Architecture
and whitelist tests enforce it; they must not be weakened to make a change pass.

Writes to a user's repositories must follow an action the user confirmed. Never
auto-stash, force-push, or write to a directory with uncommitted changes, except
discarding the one file the user explicitly confirmed on the uncommitted page.
Use disposable fixture repositories when developing or testing write operations.

Keep pull requests focused, explain the resulting behavior, and include the
checks and any required screenshots or benchmarks. Report reproducible problems
with the bug template and discuss product additions with the feature template.
