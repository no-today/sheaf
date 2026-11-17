---
name: Bug report
about: Report a reproducible problem in Sheaf
labels: bug
---

## What happened?

Describe the problem and what you expected to happen.

## Steps to reproduce

1.
2.
3.

## Environment

- Sheaf version (release number, or commit hash for a source build):
- macOS version:
- Git version (`git --version`):
- glab version, if related to merge requests (`glab --version`):

## Command log

- Command log file location:
- Relevant entries, including timestamps and errors:

Use the application's “Show Command Log” menu item to reveal the file. Release
builds store it at
`~/Library/Application Support/Sheaf/workspaces/<id>/commands.jsonl`; development
builds use `Sheaf Dev` in place of `Sheaf`. Each workspace has its own log, and
write commands are retained for 30 days. If no file exists, say so.

Before posting, redact private paths, repository URLs, branch names, credentials,
and other sensitive content. Include only the entries needed to reproduce the
problem.

## Additional context

Screenshots, a small fixture repository, or other details that help reproduce the
problem. Remove private project information from screenshots.
