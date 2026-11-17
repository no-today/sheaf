package session

import (
	"context"
	"fmt"

	"sheaf/internal/gitops"
)

// Discard drops the uncommitted changes of one file in a known worktree, as the user confirmed: an untracked file is
// deleted, any other is put back to HEAD. Nothing can bring the changes back. The worktree's uncommitted count is read
// again before it returns.
func (s *Session) Discard(ctx context.Context, worktree, path string) error {
	if !s.worktreeKnown(worktree) {
		return fmt.Errorf("discard %s: unknown worktree %q", path, worktree)
	}
	ctx, cancel := writeContext(s.git(ctx, "discard"))
	defer cancel()
	task := s.startTask(TaskDiscard, path, nil, nil)
	s.writes.Lock()
	err := gitops.Discard(ctx, worktree, path)
	s.writes.Unlock()
	task.end(err, false)
	s.RefreshDirty(ctx, []string{worktree}, true)
	return err
}
