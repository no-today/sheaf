package gitview

import (
	"context"
	"strings"

	"sheaf/internal/gitcli"
	"sheaf/internal/index"
)

// CommitBodies preserves the body after the subject and its separating blank line, including trailing newlines.
func CommitBodies(ctx context.Context, r *index.Repo, oids []gitcli.Oid) (map[gitcli.Oid]string, error) {
	messages, err := gitcli.CommitMessages(ctx, r.Dir, oids)
	bodies := make(map[gitcli.Oid]string, len(messages))
	for oid, message := range messages {
		_, body, _ := strings.Cut(message, "\n")
		bodies[oid] = strings.TrimPrefix(body, "\n")
	}
	return bodies, err
}
