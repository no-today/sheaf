package gitview

import (
	"bytes"
	"context"
	"fmt"

	"sheaf/internal/gitcli"
)

// FileText is the content of path at commit oid, as lines. A binary file has no lines. Like an
// untracked file, only the first 2MB are kept (cut at a line end): a generated file can be huge.
func FileText(ctx context.Context, dir string, oid gitcli.Oid, path string) (lines []string, binary, cut bool, err error) {
	b, err := gitcli.ShowBlob(ctx, dir, oid, path)
	if err != nil {
		return nil, false, false, err
	}
	lines, binary, cut = textOf(b)
	return lines, binary, cut, nil
}

// textOf splits a file's bytes into lines as FileText does.
func textOf(b []byte) (lines []string, binary, cut bool) {
	if isBinary(b) {
		return nil, true, false
	}
	if len(b) > maxUntracked {
		b, cut = b[:maxUntracked], true
		if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
			b = b[:i+1]
		}
	}
	return splitLines(b), false, cut
}

// UncommittedText is one side of an uncommitted file of the worktree at dir: "head" (HEAD), "index" or "worktree"
// (the file on disk, read as an untracked file is).
func UncommittedText(ctx context.Context, dir, side, path string) (lines []string, binary, cut bool, err error) {
	var b []byte
	switch side {
	case "head", "index":
		b, err = gitcli.ShowHeadOrIndex(ctx, dir, side == "head", path)
	case "worktree":
		b, err = readUntracked(dir, path)
	default:
		err = fmt.Errorf("unknown side %q", side)
	}
	if err != nil {
		return nil, false, false, err
	}
	lines, binary, cut = textOf(b)
	return lines, binary, cut, nil
}
