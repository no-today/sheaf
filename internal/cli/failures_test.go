package cli

import (
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/session"
)

func TestConfigApplyPreservesBrokenFileUntilSave(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		exit        int
	}{
		{"missing", "", 3},
		{"invalid JSON", `{"master":`, 3},
		{"valid", `{"master":"custom","mergeFlow":[{"name":"release","branch":"release"}]}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := setup(t)
			path := invoke(t, 0, "--workspace", root, "config", "show").ConfigFile
			const broken = `{"tracked":`
			if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "replacement.json")
			if tc.input != "" {
				if err := os.WriteFile(file, []byte(tc.input), 0600); err != nil {
					t.Fatal(err)
				}
			}
			out := invoke(t, tc.exit, "--workspace", root, "config", "apply", file)
			if backups, err := filepath.Glob(path + ".broken-*"); err != nil || len(backups) != 0 {
				t.Errorf("unexpected backups %v, error %v", backups, err)
			}
			saved := readFile(t, path)
			if tc.exit == 0 {
				if cfg := decode[session.Config](t, saved); cfg.Master != "custom" || cfg.Root != root {
					t.Fatalf("saved config = %+v", cfg)
				}
			} else if out.Error == nil || out.Error.Code != "config_input" || string(saved) != broken {
				t.Fatalf("apply error = %+v, saved config = %q", out.Error, saved)
			}
		})
	}
}

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
