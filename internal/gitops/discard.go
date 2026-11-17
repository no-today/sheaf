package gitops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"sheaf/internal/gitcli"
)

// ErrNotUncommitted: the file has no uncommitted change in the worktree (any more), so there is nothing to discard.
var ErrNotUncommitted = errors.New("the file has no uncommitted change")

// ErrBusy: the worktree is in the middle of a merge, rebase, cherry-pick or revert, whose files are left alone.
var ErrBusy = errors.New("a merge, rebase, cherry-pick or revert is in progress")

// Discard drops the uncommitted changes of one file of worktree, as the user asked: an untracked file is deleted, any
// other is put back to HEAD in the index and the working directory (a new file that was staged goes). The file is looked
// up in a fresh git status first, so only a file that is uncommitted now is touched, and only one file.
func Discard(ctx context.Context, worktree, path string) error {
	if Busy(worktree) {
		return fmt.Errorf("discard %s: %w", path, ErrBusy)
	}
	changes, err := gitcli.Status(ctx, worktree)
	if err != nil {
		return fmt.Errorf("discard %s: %w", path, err)
	}
	i := slices.IndexFunc(changes, func(c gitcli.Change) bool { return c.Path == path })
	if i < 0 || changes[i].Submodule {
		return fmt.Errorf("discard %s: %w", path, ErrNotUncommitted)
	}
	if changes[i].Area == gitcli.Untracked {
		err = gitcli.RemoveUntracked(ctx, worktree, path)
	} else {
		old := ""
		for _, c := range changes {
			if c.Path == path && c.OldPath != "" {
				old = c.OldPath
			}
		}
		err = gitcli.DiscardFile(ctx, worktree, path, old)
	}
	if err != nil {
		return fmt.Errorf("discard %s: %w", path, err)
	}
	return nil
}
