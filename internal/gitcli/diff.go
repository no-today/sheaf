package gitcli

import "context"

// DiffMergeBasePatch is the base...head diff of one file (both paths given to follow a rename).
func DiffMergeBasePatch(ctx context.Context, dir string, base, head Oid, path, oldPath string) (Patch, error) {
	args := patchArgs(ctx, "diff", base.String()+"..."+head.String(), "--", path)
	if oldPath != "" && oldPath != path {
		args = append(args, oldPath)
	}
	out, err := Output(ctx, dir, args...)
	if err != nil {
		return Patch{}, err
	}
	return ParsePatch(string(out)), nil
}

// DiffMergeBaseWorktree is the diff of one file from the merge base of base and HEAD to the working tree of worktree:
// a branch's commits and its uncommitted changes in one (git diff --merge-base, git 2.30+). Untracked files are not in it.
func DiffMergeBaseWorktree(ctx context.Context, worktree string, base Oid, path, oldPath string) (Patch, error) {
	args := patchArgs(ctx, "diff", "--merge-base", base.String(), "--", path)
	if oldPath != "" && oldPath != path {
		args = append(args, oldPath)
	}
	out, err := Output(ctx, worktree, args...)
	if err != nil {
		return Patch{}, err
	}
	return ParsePatch(string(out)), nil
}

func ShowBlob(ctx context.Context, dir string, oid Oid, path string) ([]byte, error) {
	return Output(ctx, dir, "show", "--no-textconv", oid.String()+":"+path)
}

// ShowHeadOrIndex is path's content at HEAD of the worktree at dir (head true) or in its index.
func ShowHeadOrIndex(ctx context.Context, dir string, head bool, path string) ([]byte, error) {
	rev := ":"
	if head {
		rev = "HEAD:"
	}
	return Output(ctx, dir, "show", "--no-textconv", rev+path)
}
