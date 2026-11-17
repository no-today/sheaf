package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"sheaf/internal/appdir"
	"sheaf/internal/discover"
	"sheaf/internal/session"
)

func saveCompletionJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func completionFixture(t *testing.T) (data, root string) {
	t.Helper()
	data = t.TempDir()
	t.Setenv("SHEAF_HOME", data)
	t.Setenv("SHEAF_COMPLETE_SHELL", "")
	root = filepath.Join(t.TempDir(), "workspace with spaces:demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	id := session.WorkspaceID(root)
	saveCompletionJSON(t, filepath.Join(data, "workspaces", id, "workspace.json"), session.Config{
		Root: root, Tracked: []string{"PAY-9", "PAY-142", "PAY-200"}, Archived: []string{"PAY-9", "PAY-999"},
		RequirementNames: map[string]string{"PAY-142": "Payment: capture\\retry\nwith care"},
		MergeFlow:        []session.MergeStep{{Name: "release", Branch: "release"}, {Name: "production", Branch: "master"}},
		Ignored:          []string{"ignored"},
	})
	saveCompletionJSON(t, filepath.Join(data, "cache", "workspaces", id, "scan.json"), []discover.Repo{
		{Name: "api", Dir: filepath.Join(root, "api")},
		{Name: "ignored", Dir: filepath.Join(root, "ignored")},
		{Name: "nested", Dir: filepath.Join(root, "ignored", "nested")},
		{Name: "web", Dir: filepath.Join(root, "web")},
	})
	saveCompletionJSON(t, filepath.Join(data, "recent.json"), map[string]any{"open": []string{filepath.Join(root, "missing")}})
	return data, root
}

func completionOutput(t *testing.T, words ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if exit := Main(append([]string{"sheaf", "__complete"}, words...), &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("%v: exit %d, stderr %q", words, exit, &stderr)
	}
	return stdout.String()
}

func forbidCompletionGit(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	for _, name := range []string{"git", "glab", "gh"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf called >> \"$SHEAF_TEST_GIT_LOG\"\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SHEAF_TEST_GIT_LOG", log)
	t.Setenv("PATH", bin)
	t.Cleanup(func() {
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Fatalf("completion called Git: %v", err)
		}
	})
}

func TestCompletePositions(t *testing.T) {
	data, root := completionFixture(t)
	forbidCompletionGit(t)
	before := workspaceFiles(t, data)
	var commandNames []string
	for _, c := range commands {
		commandNames = append(commandNames, c.name)
	}
	for _, tc := range []struct {
		name  string
		words []string
		want  []string
	}{
		{"commands", []string{""}, commandNames},
		{"command prefix", []string{"sh"}, []string{"show"}},
		{"global options", []string{"--"}, []string{"--workspace", "--json", "--rescan", "--help", "--fetch", "--version"}},
		{"command option", []string{"check", "PAY-142", "--st"}, []string{"--stage"}},
		{"help", []string{"help", ""}, append(slices.Clone(commandNames), "json")},
		{"config", []string{"config", ""}, []string{"show", "apply"}},
		{"config files", []string{"config", "apply", ""}, nil},
		{"shells", []string{"completion", ""}, []string{"zsh", "bash"}},
		{"existing workspaces", []string{"--workspace", ""}, []string{root}},
		{"show", []string{"show", ""}, []string{"PAY-142", "PAY-200", "PAY-9"}},
		{"commits", []string{"commits", "PAY-1"}, []string{"PAY-142"}},
		{"check", []string{"check", ""}, []string{"PAY-142", "PAY-200", "PAY-9"}},
		{"untrack", []string{"untrack", ""}, []string{"PAY-142", "PAY-200", "PAY-9"}},
		{"archive", []string{"archive", ""}, []string{"PAY-142", "PAY-200"}},
		{"unarchive", []string{"unarchive", ""}, []string{"PAY-9"}},
		{"track new ids", []string{"track", ""}, nil},
		{"stages", []string{"check", "--stage", ""}, []string{"release", "production"}},
		{"repositories", []string{"commits", "PAY-142", "--repo", ""}, []string{"api", "web"}},
		{"explicit workspace", []string{"--workspace", root, "show", ""}, []string{"PAY-142", "PAY-200", "PAY-9"}},
		{"equals workspace", []string{"show", "--workspace=" + root, ""}, []string{"PAY-142", "PAY-200", "PAY-9"}},
		{"after option value", []string{"check", "--stage", "release", ""}, []string{"PAY-142", "PAY-200", "PAY-9"}},
		{"second operand", []string{"show", "PAY-142", ""}, nil},
		{"no command", nil, nil},
		{"unknown command", []string{"unknown", ""}, nil},
		{"unknown command options", []string{"unknown", "--"}, nil},
		{"unknown option", []string{"show", "--unknown", ""}, nil},
		{"unsupported option", []string{"status", "--repo", "api", ""}, nil},
		{"free text option", []string{"status", "--search", ""}, nil},
		{"literal", []string{"show", "--", ""}, []string{"PAY-142", "PAY-200", "PAY-9"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Join(tc.want, "\n")
			if len(tc.want) > 0 {
				want += "\n"
			}
			start := time.Now()
			got := completionOutput(t, tc.words...)
			elapsed := time.Since(start)
			if elapsed > 200*time.Millisecond {
				t.Errorf("completion took %s; budget 200ms", elapsed)
			}
			t.Logf("__complete %q: %s", tc.words, elapsed)
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
	if after := workspaceFiles(t, data); !reflect.DeepEqual(before, after) {
		t.Fatal("completion changed app data")
	}
	t.Setenv("SHEAF_COMPLETE_SHELL", "zsh")
	if got := completionOutput(t, "show", "PAY-1"); got != "PAY-142:Payment: capture\\retry with care\n" {
		t.Fatalf("saved zsh title: %q", got)
	}
}

func TestCompleteWorkspaceLookupAndErrors(t *testing.T) {
	data, root := completionFixture(t)
	forbidCompletionGit(t)
	t.Chdir(t.TempDir())
	if got := completionOutput(t, "show", ""); got != "" {
		t.Fatalf("unlocated workspace: %q", got)
	}
	if got := completionOutput(t, "show", "--workspace", root, ""); got != "PAY-142\nPAY-200\nPAY-9\n" {
		t.Fatalf("explicit workspace from outside: %q", got)
	}
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	saveCompletionJSON(t, filepath.Join(data, "workspaces", session.WorkspaceID(child), "workspace.json"), session.Config{Root: child})
	t.Chdir(child)
	if got := completionOutput(t, "show", ""); got != "" {
		t.Fatalf("ambiguous workspace: %q", got)
	}
	for _, file := range []string{
		filepath.Join(data, "workspaces", session.WorkspaceID(root), "workspace.json"),
		filepath.Join(data, "cache", "workspaces", session.WorkspaceID(root), "scan.json"),
	} {
		b := readFile(t, file)
		for _, contents := range []string{"{broken", "[]invalid"} {
			if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := completionOutput(t, "--workspace", root, "show", ""); got != "" {
				t.Fatalf("malformed data: %q", got)
			}
		}
		if err := os.WriteFile(file, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompletionCommandTable(t *testing.T) {
	t.Setenv("SHEAF_HOME", filepath.Join(t.TempDir(), "absent"))
	for _, c := range commands {
		got := strings.Fields(completionOutput(t, c.name, "--"))
		for _, option := range c.options {
			if !slices.Contains(got, "--"+option.name) {
				t.Errorf("%s missing --%s", c.name, option.name)
			}
		}
		if len(got) != len(c.options) {
			t.Errorf("%s has extra options: %v", c.name, got)
		}
	}
	for _, topic := range []string{"", "json"} {
		if strings.Contains(helpText(topic), "__complete") {
			t.Errorf("hidden command leaked into help %s", topic)
		}
	}
	if findCommand("__complete") != nil {
		t.Fatal("hidden command entered public table")
	}
}

func TestCompletionNoSideEffects(t *testing.T) {
	data := filepath.Join(t.TempDir(), "absent")
	t.Setenv("SHEAF_HOME", data)
	t.Chdir(t.TempDir())
	forbidCompletionGit(t)
	for _, words := range [][]string{{""}, {"show", ""}, {"--workspace", ""}, {"check", "--stage", ""}, {"commits", "--repo", ""}, {"--workspace", t.TempDir(), "show", ""}} {
		completionOutput(t, words...)
	}
	for _, shell := range []string{"zsh", "bash"} {
		if script := helpOutput(t, "completion", shell); script == "" {
			t.Fatalf("empty %s script", shell)
		}
	}
	for _, args := range [][]string{{"completion"}, {"completion", "fish"}, {"completion", "zsh", "extra"}, {"completion", "bash", "--workspace", "/missing"}, {"completion", "bash", "--json"}} {
		var stdout, stderr bytes.Buffer
		if exit := Main(append([]string{"sheaf"}, args...), &stdout, &stderr); exit != 3 || stderr.Len() != 0 {
			t.Fatalf("%q: exit %d, stderr %q", args, exit, &stderr)
		}
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("created app data: %v", err)
	}
	var stderr bytes.Buffer
	if exit := Main([]string{"sheaf", "__complete", ""}, failingCompletionWriter{}, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("completion write error escaped: %d, %q", exit, &stderr)
	}
}

type failingCompletionWriter struct{}

func (failingCompletionWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("closed") }

func TestCompletionShellSyntax(t *testing.T) {
	t.Setenv("SHEAF_HOME", t.TempDir())
	for _, shell := range []string{"zsh", "bash"} {
		t.Run(shell, func(t *testing.T) {
			bin, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s unavailable: %v", shell, err)
			}
			for _, name := range []string{"sheaf", "sheaf-dev", "sheaf with ' quote"} {
				var stdout, stderr bytes.Buffer
				if exit := Main([]string{name, "completion", shell}, &stdout, &stderr); exit != 0 || stderr.Len() != 0 || stdout.Len() == 0 {
					t.Fatalf("generate %s: exit %d, stderr %q", shell, exit, &stderr)
				}
				cmd := exec.Command(bin, "-n")
				cmd.Stdin = &stdout
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s -n: %v\n%s", shell, err, out)
				}
			}
		})
	}
}

func TestCompletionShellHelper(t *testing.T) {
	if os.Getenv("SHEAF_COMPLETION_HELPER") != "1" {
		t.Setenv("SHEAF_HOME", t.TempDir())
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(Main(os.Args[i+1:], os.Stdout, os.Stderr))
		}
	}
	os.Exit(99)
}

func TestCompletionShellForwarding(t *testing.T) {
	_, root := completionFixture(t)
	bin := t.TempDir()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\nexec '" + strings.ReplaceAll(testBinary, "'", "'\\''") + "' -test.run='^TestCompletionShellHelper$' -- \"$0\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "sheaf-dev"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SHEAF_COMPLETION_HELPER", "1")
	t.Setenv("BASH_ENV", "")
	if err := os.WriteFile(filepath.Join(root, "config fixture.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"zsh", "bash"} {
		t.Run(shell, func(t *testing.T) {
			executable, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s unavailable: %v", shell, err)
			}
			t.Setenv("HOME", t.TempDir())
			t.Setenv("ZDOTDIR", t.TempDir())
			var script string
			if shell == "bash" {
				script = `source "$1"
complete -p sheaf-dev
COMP_WORDS=(sheaf-dev sh); COMP_CWORD=1
_sheaf_complete
printf 'command=%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(sheaf-dev show ''); COMP_CWORD=2
_sheaf_complete
printf 'id=%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(sheaf-dev --workspace ''); COMP_CWORD=2
_sheaf_complete
printf 'workspace=%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(sheaf-dev config apply 'config '); COMP_CWORD=3
_sheaf_complete
printf 'file=%s\n' "${COMPREPLY[@]}"
`
			} else {
				// Site completion dirs hold whatever this machine installed (a stale _sheaf link included); only zsh's own count.
				script = `fpath=(${fpath:#*/site-functions}); autoload -U compinit && compinit -D
source <(sheaf-dev completion zsh)
[[ ${_comps[sheaf-dev]} == _sheaf_complete ]] || exit 99
_describe() {
  [[ -n ${candidates[1]} ]] || { print -ru2 -- 'empty candidate reached compadd'; return 1; }
  print -rl -- "${candidates[@]}"
}
_files() { print -r -- 'file=config fixture.json'; }
words=(sheaf-dev sh); CURRENT=2; PREFIX=sh
_sheaf_complete
words=(sheaf-dev show ''); CURRENT=3; PREFIX=''
_sheaf_complete
words=(sheaf-dev --workspace ''); CURRENT=3; PREFIX=''
_sheaf_complete
words=(sheaf-dev config apply 'config '); CURRENT=4; PREFIX='config '
_sheaf_complete
words=(sheaf-dev track ''); CURRENT=3; PREFIX=''
_sheaf_complete
words=(sheaf-dev unknown ''); CURRENT=3; PREFIX=''
_sheaf_complete
`
			}
			completionFile := filepath.Join(bin, "completion."+shell)
			if err := os.WriteFile(completionFile, []byte(completionScript("sheaf-dev", shell)), 0o600); err != nil {
				t.Fatal(err)
			}
			flags := []string{"--norc", "-c", script, "completion-test", completionFile}
			if shell == "zsh" {
				flags = []string{"-f", "-c", script}
			}
			cmd := exec.Command(executable, flags...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil || stderr.Len() != 0 {
				t.Fatalf("%s forwarding: %v\nstdout: %s\nstderr: %s", shell, err, &stdout, &stderr)
			}
			want := []string{"command=show", "id=PAY-142\nid=PAY-200\nid=PAY-9", "workspace=" + root, "file=config fixture.json"}
			if shell == "zsh" {
				want = []string{"show:", "PAY-142:Payment: capture\\retry with care\nPAY-200:\nPAY-9:", strings.ReplaceAll(root, ":", "\\:") + ":", "file=config fixture.json"}
			}
			for _, value := range want {
				if !strings.Contains(stdout.String(), value) {
					t.Errorf("missing %q in %q", value, stdout.String())
				}
			}
		})
	}
}

func TestCompletionUnderLock(t *testing.T) {
	data, root := completionFixture(t)
	unlock, err := appdir.TryLock(t.Context(), data, session.WorkspaceID(root))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if got := completionOutput(t, "show", "PAY-1"); got != "PAY-142\n" {
		t.Fatal(got)
	}
}
