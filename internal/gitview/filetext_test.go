package gitview

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"sheaf/internal/gitcli"
	"sheaf/internal/testutil"
)

func TestFileText(t *testing.T) {
	testutil.Isolate(t)
	dir := filepath.Join(t.TempDir(), "r")
	r := testutil.NewRepo(t, dir)
	hash := r.Commit("files", map[string]string{
		"a.txt": "first\nsecond\n", "logo.bin": "PNG\x00\x01",
		"big.txt": strings.Repeat("0123456789\n", 300_000),
	})
	oid, err := gitcli.ParseOid(hash)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if lines, binary, cut, err := FileText(ctx, dir, oid, "a.txt"); err != nil || binary || cut || len(lines) != 2 || lines[0] != "first" || lines[1] != "second" {
		t.Errorf("text: %q binary=%v cut=%v err=%v", lines, binary, cut, err)
	}
	if lines, binary, cut, err := FileText(ctx, dir, oid, "logo.bin"); err != nil || !binary || cut || len(lines) != 0 {
		t.Errorf("binary: %q binary=%v cut=%v err=%v", lines, binary, cut, err)
	}
	if lines, binary, cut, err := FileText(ctx, dir, oid, "big.txt"); err != nil || binary || !cut || len(lines) == 0 || len(lines) >= 300_000 || lines[len(lines)-1] != "0123456789" {
		t.Errorf("large file: %d lines binary=%v cut=%v err=%v", len(lines), binary, cut, err)
	}
	if _, _, _, err := FileText(ctx, dir, oid, "missing.txt"); err == nil {
		t.Error("reading a missing file succeeded")
	}
}
