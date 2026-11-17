package discover

import (
	"context"
	"strings"

	"sheaf/internal/gitcli"
)

// UserEmail is the global user.email, the default "me".
func UserEmail(ctx context.Context) string {
	out, _ := gitcli.Output(ctx, "", "config", "--global", "user.email")
	return strings.TrimSpace(string(out))
}

// RepoUserEmail is the user.email git uses in dir (repo config, includeIf, then global).
func RepoUserEmail(ctx context.Context, dir string) string {
	out, _ := gitcli.Output(ctx, dir, "config", "user.email")
	return strings.TrimSpace(string(out))
}

// RepoOriginURL is remote.origin.url of dir, "" when there is no origin. It may carry credentials: callers must not log or send it on.
func RepoOriginURL(ctx context.Context, dir string) string {
	out, _ := gitcli.Output(ctx, dir, "config", "remote.origin.url")
	return strings.TrimSpace(string(out))
}
