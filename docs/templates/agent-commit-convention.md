# Sheaf commit convention (for coding agents)

[简体中文](agent-commit-convention.zh-CN.md)

Sheaf finds requirement numbers in **branch names** and **commit subjects**, and groups every repository's changes under the same requirement; the subject without the number becomes the requirement's name in the left pane. When an agent commits this way, Sheaf finds everything it wrote.

Send your coding agent this one line:

> Read https://github.com/no-today/sheaf/blob/main/docs/templates/agent-commit-convention.md and follow "Set up in a workspace" to install the commit convention in this workspace.

## Set up in a workspace

For the agent, in order:

1. **Find the workspace root**: the folder the user opens in Sheaf, holding the repositories. If unsure, ask the user.
2. **Confirm what requirement numbers look like**: Sheaf recognizes `[A-Z][A-Z0-9]+-\d+` by default (`PAY-142`, `AB2-7`). Ask the user what their team's numbers look like; if different, tell the user to enter a matching regex in Sheaf's **Settings → My Commits → Pattern**, or Sheaf will not recognize them.
3. **Write it into the root's agent instruction file**: add everything between the two markers under "Convention" below (markers included) to `CLAUDE.md` at the workspace root; if the user runs another agent, use the file that agent reads (such as `AGENTS.md`). Create the file if missing.
   - If a `<!-- sheaf:commit-convention -->` block is already there, replace it whole; never add a second one.
   - If the user's numbers do not look like `PAY-142`, replace the examples with the team's real format.
4. **Do not edit the repositories on your own**: a tracked instruction file inside a repository has to be committed, so ask the user before adding a copy to each one.
5. **Tell the user** which files you changed, whether step 2 needs them to change the pattern in Sheaf, and that "Enforce on this Mac" below can make git check every commit, if they want it.

## Enforce on this Mac (optional)

`githooks/dispatch` checks every commit in the workspace: a subject without a leading number gets the branch's number added; with no number in the branch either, the commit is refused. A different number than the branch's is only noted. Merges, reverts and fixups are not checked. It first runs each repository's own hook of the same name, which git would otherwise skip once `core.hooksPath` is set. If the team's numbers are not like `PAY-142`, export `SHEAF_REQUIREMENT_PATTERN` (an extended regex) as in Sheaf's setting.

It changes the global git config, so an agent installs it only when the user asks:

```sh
WS=/path/to/workspace   # the folder opened in Sheaf
mkdir -p ~/.githooks
curl -fsSL https://raw.githubusercontent.com/no-today/sheaf/main/docs/templates/githooks/dispatch -o ~/.githooks/dispatch
chmod +x ~/.githooks/dispatch
(cd ~/.githooks && for h in applypatch-msg pre-applypatch post-applypatch pre-commit pre-merge-commit prepare-commit-msg commit-msg post-commit pre-rebase post-checkout post-merge pre-push post-rewrite pre-auto-gc push-to-checkout sendemail-validate; do ln -sf dispatch "$h"; done)
printf '[core]\n\thooksPath = ~/.githooks\n' > ~/.gitconfig-sheaf
git config --global includeIf."gitdir:$WS/".path ~/.gitconfig-sheaf
```

Check: in any repository under `$WS`, `git config core.hooksPath` prints `~/.githooks`. Skip once with `git commit --no-verify`.

## Convention

<!-- sheaf:commit-convention -->
## Commit convention

Every change belongs to a requirement, numbered like `PAY-142`. Use the same number in every repository.

### Must

- **Branch names contain the full requirement number**: `feat-PAY-142`, `fix-PAY-142-timeout`. Never a partial number (`PAY-14`), never split it.
- **Commit subjects start with the requirement number**, one space, then one sentence saying what changed: `PAY-142 Add payment capture to gateway`.
- **Keep git's default merge message** (`Merge branch 'feat-PAY-142' into ...`); do not remove the branch name from it. For squash merges, the subject still starts with the number.
- **If you do not know the number, ask.** Never invent one. For small changes that belong to no requirement (a typo, a dependency bump), ask the user whether the number can be left out.
- Do not put other requirement numbers in the subject; one commit belongs to one requirement.

### Should

- In the body, list **behaviour changes**, one per line, in product terms: who, in what situation, now gets what. For a pure refactor write "No behaviour change." Do not write "changed XxxService.java".
- Give each of these its own line: database scripts (which tables, how many rows), permissions (who gained or lost what), flags and config (name, default), and calls whose boolean arguments or argument order changed (every call site and its new values).
- One commit does one thing.

### Examples

```
PAY-142 Add payment capture to gateway

Orders now call the capture API; zero or negative amounts return 0
DML: unique index uk_trade_no added to pay_record
```

```
PAY-142 Extract captureOnce from Capture

No behaviour change.
```
<!-- /sheaf:commit-convention -->
