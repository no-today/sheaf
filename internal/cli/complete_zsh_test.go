package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestZshCompletionAutoloadAndSource(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skipf("zsh unavailable: %v", err)
	}
	for _, name := range []string{"sheaf", "sheaf-dev"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("ZDOTDIR", root)
			t.Setenv("SHEAF_HOME", filepath.Join(root, "data"))
			script := completionScript(name, "zsh")
			file := filepath.Join(root, "_sheaf")
			if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			command := `fpath=("$1" $fpath)
autoload -Uz compinit
compinit -D -d "$1/dump"
print -r -- $_comps[$2]
functions[$2]='print -r -- show:command'
_describe() { print -r -- $candidates[1]; }
words=($2 sh); CURRENT=2; PREFIX=sh
_sheaf
source "$1/_sheaf"
print -r -- $_comps[$2]
`
			out, err := exec.Command(zsh, "-f", "-c", command, "completion-test", root, name).CombinedOutput()
			if err != nil || string(out) != "_sheaf\nshow:command\n_sheaf_complete\n" {
				t.Fatalf("zsh registration and invocation: %q, %v", out, err)
			}
		})
	}
}
