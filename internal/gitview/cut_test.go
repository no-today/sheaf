package gitview

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUncommittedWorktreeTextCut(t *testing.T) {
	dir := t.TempDir()
	line := append(bytes.Repeat([]byte("x"), 99), '\n')
	big := bytes.Repeat(line, maxUntracked/len(line)+10)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	lines, binary, cut, err := UncommittedText(context.Background(), dir, "worktree", "big.txt")
	if err != nil || binary || !cut {
		t.Fatalf("err=%v binary=%v cut=%v", err, binary, cut)
	}
	if got := len(lines) * len(line); got > maxUntracked || got < maxUntracked-len(line) {
		t.Errorf("kept %d bytes", got)
	}
}
