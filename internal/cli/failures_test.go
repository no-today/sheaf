package cli

import (
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/session"
)

func TestStatusReportsUnreadableRepo(t *testing.T) {
	root, _ := setup(t)
	if err := os.MkdirAll(filepath.Join(root, "bad", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	out := invoke(t, 2, "--workspace", root, "status")
	if out.Complete || out.Error != nil || len(out.Problems) != 1 {
		t.Fatalf("partial status: %+v", out)
	}
	p := out.Problems[0]
	if p.Code != session.ProblemGitRead || p.Repo != "bad" || p.Detail == "" {
		t.Fatalf("problem: %+v", p)
	}
}
