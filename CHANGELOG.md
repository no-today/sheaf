# Changelog

## 0.1.0

- For Apple silicon Macs.
- Follow a requirement number across every repository and worktree in a folder:
  see each repository's merge stage and read all of their changes as one stream.
  A requirement whose branches were deleted is judged by whether its own commits
  reached each stage; when nothing can be checked it shows "Unconfirmed" instead
  of guessing.
- View all changes, uncommitted ones, or what is still awaiting each merge stage;
  open any commit in its own window, unified or side by side, with manual merge
  commits shown against a fresh merge. The diff you are reading stays put until
  you refresh, and a reminder appears only when what it shows has changed.
- Review uncommitted work from all working directories in one place, even before
  a requirement's first commit, and discard a single file's changes after
  confirming.
- Refresh remotes, update favourite branches (fast-forward only), and create
  GitLab MRs or GitHub PRs in batches, each planned and confirmed first; never
  force-push, never stash, never touch directories with uncommitted changes.
- A standalone `sheaf` command line for people and coding agents: list tracked
  requirements and their gaps (`status`), see one requirement across every
  repository (`show`, with `--files` for uncommitted files), check whether it
  has merged through the flow (`check`), and page through its commits
  (`commits`). Every command has `--json` with a versioned contract, stable
  problem codes and exit codes; `sheaf help json` documents each field.
  Install it, with zsh completion, from Settings › General; bash completion
  with `eval "$(sheaf completion bash)"`.
- App and command line share data under `~/.sheaf`; while the app is open the
  command line only reads.
- Keep an activity log of every write command for 30 days.
- English and Simplified Chinese, in everyday words.
- A commit convention template that coding agents set up in a workspace.
