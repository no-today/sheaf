package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"sheaf/internal/session"
)

func helpOutput(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if exit := Main(append([]string{"sheaf"}, args...), &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, exit, &stdout, &stderr)
	}
	return stdout.String()
}

func TestHelpCommandTable(t *testing.T) {
	t.Setenv("SHEAF_HOME", filepath.Join(t.TempDir(), "absent"))
	general := helpOutput(t, "--help")
	for _, args := range [][]string{nil, {"-h"}, {"help"}} {
		if got := helpOutput(t, args...); got != general {
			t.Errorf("%v differs from --help", args)
		}
	}
	texts := []string{general, helpOutput(t, "help", "json")}
	seen := map[string]bool{}
	for _, c := range commands {
		if seen[c.name] {
			t.Errorf("duplicate command %s", c.name)
		}
		seen[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			text := helpOutput(t, "help", c.name)
			texts = append(texts, text)
			if got := helpOutput(t, c.name, "--help"); got != text {
				t.Error("command --help differs from help command")
			}
			if got := helpOutput(t, c.name, "-h"); got != text {
				t.Error("command -h differs from help command")
			}
			for _, option := range c.options {
				if !strings.Contains(text, "--"+option.name) {
					t.Errorf("help missing --%s", option.name)
				}
				args := []string{c.name, "--help", "--" + option.name}
				if option.value != "" {
					value := "example"
					if option.name == "limit" {
						value = "1"
					}
					args = append(args, value)
				}
				if _, err := parse(args); err != nil {
					t.Errorf("documented option rejected: %v: %v", args, err)
				}
			}
			out := decode[response](t, []byte(helpOutput(t, "help", c.name, "--json")))
			if got := decode[string](t, out.Result); got != text {
				t.Error("JSON help text differs")
			}
		})
	}
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			if utf8.RuneCountInString(line) > 100 {
				t.Errorf("help line exceeds 100 columns: %s", line)
			}
		}
	}
}

func TestHelpReviewGuidance(t *testing.T) {
	t.Setenv("SHEAF_HOME", filepath.Join(t.TempDir(), "absent"))
	for _, tc := range []struct {
		name, topic string
		phrases     []string
	}{
		{"workspace discovery", "", []string{"sheaf workspaces --json", "choose a root with exists = true", "--workspace <root>"}},
		{"workspace JSON", "json", []string{"workspaces: {items: KnownWorkspace[]}", "KnownWorkspace:", "configFile: string", "Several roots may be current"}},
		{"general targets", "", []string{
			"name is a label, branch is the target",
			"config bases[repo], then origin/master, origin/main, master, main; never origin/HEAD",
			"empty branch: use each repo's main branch",
			"an unconfigured release branch is ignored",
			"Report stages[].ref (the actual ref read), not just the stage label",
		}},
		{"general review", "", []string{
			"show --json answers most review questions",
			"Use check only when you need a pass/fail exit code; there is no need to run it after show",
			"path is the main repo for history; rows[].worktree is for uncommitted changes",
			"git -C <path> show <sha>", "git -C <worktree> diff",
			"pass show <id> --files for the list; use git -C <worktree> diff -- <path> for content",
			"Changes on branches without the requirement number are excluded; run git status yourself",
			"dev takes priority over pending merge stages; inspect gaps[].next / missing",
		}},
		{"JSON targets", "json", []string{
			"name is only a label and the --stage selector; it is not a branch name",
			"branch is the target branch; \"\" uses each repository's main branch",
			"config bases[repo] first, else the first existing ref in order: origin/master, origin/main, master, main",
			"origin/HEAD is never used",
			"Report stages[].ref, the actual ref read, rather than just the stage label",
			"Branches outside the configured flow (e.g. release when not configured) do not participate",
		}},
		{"JSON own commits", "json", []string{
			"Non-merge commits whose subject contains the complete requirement number",
			"merge commits whose subject contains that number and which actually introduce their own changes",
			"Only the subject is matched, never the body",
			"Clean synchronization merges and unclassified merges do not count as own commits",
			"all commits whose subject contains the complete requirement number, including merge commits",
			"rows[].commits may exceed covered + missing for a target; coverage uses own commits",
		}},
		{"JSON paths and progress", "json", []string{
			"absolute main repository directory",
			"Read commits with git -C <path> show <sha>; path is not the worktree for uncommitted changes",
			"git -C <worktree> diff -- <path>, using each file's worktree/path",
			"uncommittedFiles?: UncommittedFile[], only with show --files; [] means no matching changes",
			"The list may span multiple worktrees; sorted by worktree, path, then area",
			"--files preserves the rows and every other row field",
			"UncommittedFile:", "worktree: string, absolute working directory for this file", "oldPath?: string",
			"whose branch name lacks the requirement number are excluded; run git status there if needed",
			"dev takes priority over pending merge stages; merge gaps may still exist",
			"gaps[].next / missing for the next target label and uncovered own commit count",
			"show --json answers most review questions; check is only needed for a pass/fail exit code",
			"There is no need to run check after show",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--help"}
			if tc.topic != "" {
				args = []string{"help", tc.topic}
			}
			text := strings.Join(strings.Fields(helpOutput(t, args...)), " ")
			for _, phrase := range tc.phrases {
				if !strings.Contains(text, phrase) {
					t.Errorf("help missing review guidance: %q", phrase)
				}
			}
		})
	}
}

func TestHelpJSONValues(t *testing.T) {
	t.Setenv("SHEAF_HOME", filepath.Join(t.TempDir(), "absent"))
	text := helpOutput(t, "help", "json")
	values := []string{
		session.ProblemUnclassifiedMerge, session.ProblemPatchPending, session.ProblemGitRead,
		session.ProblemFetchFailed, session.ProblemNoEvidence, session.ProblemFinalMissing,
		session.StageDev, session.StageUnknown, "merge:", session.StagePending, session.StageUAT,
		session.StageLanded, session.StageArchived, session.CoverAncestor, session.CoverPatch,
		session.SkipBaselineOnly, session.SkipNoTarget, "commit_body_unreadable",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	codes := regexp.MustCompile(`\.fail\("([a-z_]+)"`)
	errorCodes := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		for _, match := range codes.FindAllSubmatch(readFile(t, file), -1) {
			errorCodes[string(match[1])] = true
		}
	}
	if len(errorCodes) == 0 {
		t.Fatal("did not collect error codes from CLI source")
	}
	for code := range errorCodes {
		values = append(values, code)
	}
	for _, value := range values {
		if !strings.Contains(text, value) {
			t.Errorf("help json missing %q", value)
		}
	}
}

func TestHelpExamples(t *testing.T) {
	root, r := setup(t)
	r.Git("checkout", "-qb", "PAY-142")
	r.Commit("PAY-142 example\n\nExample body.", map[string]string{"example": "example\n"})
	r.Commit("PAY-142 second", map[string]string{"second": "second\n"})
	t.Chdir(root)
	// Each example gets its own app data and the same initial tracked requirement.
	config := session.Config{Root: root, Tracked: []string{"PAY-142"}, MergeFlow: []session.MergeStep{
		{Name: "release", Branch: "release"}, {Name: "production", Branch: "master"},
	}}
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	texts := []string{helpText(""), helpText("json")}
	for _, c := range commands {
		texts = append(texts, helpText(c.name))
	}
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			example := strings.TrimSpace(line)
			if !strings.HasPrefix(example, "sheaf ") {
				continue
			}
			t.Run(example, func(t *testing.T) {
				t.Setenv("SHEAF_HOME", t.TempDir())
				configure(t, root, config)
				var stdout, stderr bytes.Buffer
				args := strings.Fields(example)
				if exit := Main(args, &stdout, &stderr); exit < 0 || exit > 2 {
					t.Fatalf("exit %d\nstdout: %s\nstderr: %s", exit, &stdout, &stderr)
				}
				if jsonRequested(args[1:]) && !json.Valid(stdout.Bytes()) {
					t.Fatalf("invalid JSON: %s", &stdout)
				}
			})
		}
	}
}

func TestHelpAndVersionHaveNoSideEffects(t *testing.T) {
	data := filepath.Join(t.TempDir(), "absent")
	t.Setenv("SHEAF_HOME", data)
	t.Chdir(t.TempDir())
	bin := t.TempDir()
	log := filepath.Join(bin, "git-calls")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf called >> \"$SHEAF_TEST_GIT_LOG\"\nexit 99\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHEAF_TEST_GIT_LOG", log)
	t.Setenv("PATH", bin)
	args := [][]string{nil, {"--help"}, {"-h"}, {"help"}, {"help", "json"}, {"--version"}, {"version"}, {"--version", "--json"}, {"version", "--json"}}
	for _, c := range commands {
		args = append(args, []string{"help", c.name}, []string{c.name, "--help"})
	}
	for _, arg := range args {
		helpOutput(t, arg...)
	}
	for _, path := range []string{data, log} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("help/version created %s: %v", path, err)
		}
	}
}

func TestVersion(t *testing.T) {
	t.Setenv("SHEAF_HOME", filepath.Join(t.TempDir(), "absent"))
	for _, args := range [][]string{{"--version"}, {"version"}} {
		if got := helpOutput(t, args...); got != "sheaf "+version+"\n" {
			t.Errorf("%v: %q", args, got)
		}
		text := helpOutput(t, append(args, "--json")...)
		var out struct {
			Version int               `json:"version"`
			Result  map[string]string `json:"result"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil || out.Version != 1 || out.Result["sheaf"] != version {
			t.Fatalf("%s: %v", text, err)
		}
		if fields := decode[map[string]json.RawMessage](t, []byte(text)); len(fields) != 2 {
			t.Errorf("unexpected version envelope: %s", text)
		}
	}
}

func TestArgumentErrorUsage(t *testing.T) {
	t.Setenv("SHEAF_HOME", filepath.Join(t.TempDir(), "absent"))
	for _, tc := range []struct {
		args    []string
		command string
	}{
		{[]string{"show"}, "show"},
		{[]string{"show", "PAY-142", "extra"}, "show"},
		{[]string{"status", "--repo", "repo"}, "status"},
		{[]string{"--wat", "show", "PAY-142"}, "show"},
		{[]string{"commits", "PAY-142", "--limit", "oops"}, "commits"},
		{[]string{"commits", "PAY-142", "--cursor"}, "commits"},
		{[]string{"wat"}, ""},
		{[]string{"help", "wat"}, "help"},
		{[]string{"version", "extra"}, "version"},
	} {
		usage := rootUsage
		if c := findCommand(tc.command); c != nil {
			usage = "Usage: sheaf " + c.usage
		}
		for _, structured := range []bool{false, true} {
			args := append([]string{"sheaf"}, tc.args...)
			if structured {
				args = append(args, "--json")
			}
			var stdout, stderr bytes.Buffer
			if exit := Main(args, &stdout, &stderr); exit != 3 {
				t.Fatalf("%v: exit %d: %s", args, exit, &stdout)
			}
			message := stdout.String()
			if structured {
				out := decode[response](t, stdout.Bytes())
				if out.Error == nil || out.Error.Code != "invalid_input" {
					t.Fatalf("%v: %s", args, &stdout)
				}
				message = out.Error.Message
			}
			if !strings.Contains(message, usage) || !strings.Contains(message, "Run 'sheaf help") {
				t.Errorf("%v: missing usage or next step: %s", args, message)
			}
			if tc.command == "" || tc.command == "help" {
				for _, c := range commands {
					if !strings.Contains(message, c.name) {
						t.Errorf("%v: missing available command/topic %s: %s", args, c.name, message)
					}
				}
				if tc.command == "help" && !strings.Contains(message, "json") {
					t.Error("unknown help topic did not list json")
				}
			}
		}
	}
}
