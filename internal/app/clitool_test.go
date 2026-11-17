package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"sheaf/internal/native/settingswin"
)

func cliToolFixture(t *testing.T, kinds [2]string) []cliToolLink {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SHEAF_HOME", filepath.Join(root, "data"))
	var links []cliToolLink
	for i, name := range []string{"sheaf", "_sheaf"} {
		source := filepath.Join(root, "App's folder", "Sheaf.app", "Contents", "Resources", name)
		target := filepath.Join(root, "User's "+name, name)
		if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("bundled "+name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		dest := ""
		switch kinds[i] {
		case "absent":
		case "ours-this-bundle":
			dest = source
		case "ours-relative":
			var err error
			dest, err = filepath.Rel(filepath.Dir(target), source)
			if err != nil {
				t.Fatal(err)
			}
		case "ours-other-bundle", "ours-broken":
			dest = filepath.Join(root, "Old app", "Sheaf.app", "Contents", "Resources", name)
			if kinds[i] == "ours-other-bundle" {
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, []byte("older "+name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
		case "foreign-file":
			if err := os.WriteFile(target, []byte("keep this file"), 0o755); err != nil {
				t.Fatal(err)
			}
		case "foreign-directory":
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
		case "foreign-symlink":
			dest = filepath.Join(root, "Another.app", "Contents", "Resources", name)
		case "foreign-similar-bundle":
			dest = filepath.Join(root, "NotSheaf.app", "Contents", "Resources", name)
		case "foreign-similar-resource":
			dest = filepath.Join(root, "Sheaf.app", "Other", "Contents", "Resources", name)
		case "foreign-wrong-name":
			dest = filepath.Join(filepath.Dir(source), "another-resource")
		default:
			t.Fatalf("unknown fixture: %s", kinds[i])
		}
		if dest != "" {
			if err := os.Symlink(dest, target); err != nil {
				t.Fatal(err)
			}
		}
		links = append(links, cliToolLink{source, target})
	}
	return links
}

func fakeCLIToolPrivileges(t *testing.T, run func(context.Context, string) (bool, error)) {
	t.Helper()
	previous := cliToolPrivileged
	cliToolPrivileged = run
	t.Cleanup(func() { cliToolPrivileged = previous })
}

func TestCLIToolHiddenInDevelopment(t *testing.T) {
	links := cliToolFixture(t, [2]string{"absent", "absent"})
	previousApp, previousTarget, previousCompletion := mygo.App, cliToolTarget, cliCompletionTarget
	mygo.App, cliToolTarget, cliCompletionTarget = &mygo.Application{}, links[0].target, links[1].target
	t.Cleanup(func() { mygo.App, cliToolTarget, cliCompletionTarget = previousApp, previousTarget, previousCompletion })
	mygo.App.SetPath(mygo.PathResources, filepath.Dir(links[0].source))
	wins := &Windows{}
	// These dependencies must exist even without a workspace session.
	d := wins.settingsDeps(nil)
	if d.CLITool == nil || d.SetCLITool == nil {
		t.Fatal("global settings lack command line tool dependencies")
	}
	for _, name := range []string{"Sheaf", "Sheaf Dev", "sheaf-dev"} {
		mygo.App.SetName(name)
		tool := d.CLITool()
		if tool.Available != (name == "Sheaf") || tool.Installed || tool.Path != links[0].target {
			t.Fatalf("%s: state = %+v", name, tool)
		}
	}
}

func TestCLIToolStatesAndActions(t *testing.T) {
	cases := []struct {
		kind    string
		state   cliToolState
		current bool
	}{
		{"absent", cliToolAbsent, false},
		{"ours-this-bundle", cliToolOurs, true},
		{"ours-relative", cliToolOurs, true},
		{"ours-other-bundle", cliToolOurs, false},
		{"ours-broken", cliToolOurs, false},
		{"foreign-file", cliToolForeign, false},
		{"foreign-directory", cliToolForeign, false},
		{"foreign-symlink", cliToolForeign, false},
		{"foreign-similar-bundle", cliToolForeign, false},
		{"foreign-similar-resource", cliToolForeign, false},
		{"foreign-wrong-name", cliToolForeign, false},
	}
	fakeCLIToolPrivileges(t, func(context.Context, string) (bool, error) {
		t.Fatal("unexpected privileged runner")
		return false, nil
	})
	for _, first := range cases {
		for _, second := range cases {
			for _, install := range []bool{true, false} {
				name := first.kind + "/" + second.kind + "/" + strconv.FormatBool(install)
				t.Run(name, func(t *testing.T) {
					links := cliToolFixture(t, [2]string{first.kind, second.kind})
					tool := commandLineToolState(links)
					if !tool.Available || tool.Installed != (first.current && second.current) {
						t.Fatalf("state = %+v", tool)
					}
					var infos [2]os.FileInfo
					var dests [2]string
					for i, want := range []cliToolState{first.state, second.state} {
						state, dest, err := inspectCLITool(links[i].target)
						if err != nil || state != want {
							t.Fatalf("link %d: state = %v, %v; want %v", i, state, err, want)
						}
						infos[i], _ = os.Lstat(links[i].target)
						dests[i] = dest
					}
					refused := install && (first.state == cliToolForeign || second.state == cliToolForeign)
					changed, err := changeCLITool(context.Background(), links, install)
					if refused {
						if changed || !errors.Is(err, settingswin.ErrForeignCLITool) {
							t.Fatalf("foreign target: changed=%v, err=%v", changed, err)
						}
					} else if err != nil || changed != (install || first.state == cliToolOurs || second.state == cliToolOurs) {
						t.Fatalf("changed=%v, err=%v", changed, err)
					}
					for i, prior := range []cliToolState{first.state, second.state} {
						state, dest, err := inspectCLITool(links[i].target)
						if err != nil {
							t.Fatal(err)
						}
						if refused || prior == cliToolForeign {
							after, statErr := os.Lstat(links[i].target)
							if state != prior || dest != dests[i] || (infos[i] == nil && !errors.Is(statErr, os.ErrNotExist)) || (infos[i] != nil && (statErr != nil || !os.SameFile(infos[i], after))) {
								t.Fatalf("link %d modified despite foreign occupant", i)
							}
							if prior == cliToolForeign && infos[i].Mode().IsRegular() {
								if contents, err := os.ReadFile(links[i].target); err != nil || string(contents) != "keep this file" {
									t.Fatalf("foreign contents=%q, err=%v", contents, err)
								}
							}
						} else if install {
							if state != cliToolOurs || dest != links[i].source {
								t.Fatalf("link %d: state=%v, dest=%q", i, state, dest)
							}
						} else if state != cliToolAbsent {
							t.Fatalf("link %d not removed: %v", i, state)
						}
					}
				})
			}
		}
	}
}

func TestCLIToolMissingResource(t *testing.T) {
	for i := range 2 {
		links := cliToolFixture(t, [2]string{"absent", "absent"})
		if err := os.Remove(links[i].source); err != nil {
			t.Fatal(err)
		}
		if tool := commandLineToolState(links); tool.Available || tool.Installed {
			t.Fatalf("missing resource: %+v", tool)
		}
		if changed, err := changeCLITool(context.Background(), links, true); changed || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing resource: changed=%v, err=%v", changed, err)
		}
		for _, link := range links {
			if _, err := os.Lstat(link.target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created a link for a missing resource: %v", err)
			}
		}
	}
}

func TestCLIToolPrivilegeRetry(t *testing.T) {
	for _, tt := range []struct {
		name, kind            string
		install, cancel, fail bool
	}{
		{name: "install", kind: "absent", install: true},
		{name: "replace", kind: "ours-broken", install: true},
		{name: "uninstall", kind: "ours-this-bundle"},
		{name: "cancel-install", kind: "absent", install: true, cancel: true},
		{name: "cancel-replace", kind: "ours-broken", install: true, cancel: true},
		{name: "cancel-uninstall", kind: "ours-this-bundle", cancel: true},
		{name: "failure", kind: "absent", install: true, fail: true},
	} {
		for blocked := range 2 {
			t.Run(tt.name+strconv.Itoa(blocked), func(t *testing.T) {
				links := cliToolFixture(t, [2]string{tt.kind, tt.kind})
				before, err := inspectCLIToolLinks(links, tt.install)
				if err != nil {
					t.Fatal(err)
				}
				dir := filepath.Dir(links[blocked].target)
				if err := os.Chmod(dir, 0o555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
				calls := 0
				runnerErr := errors.New("privileged command failed")
				fakeCLIToolPrivileges(t, func(ctx context.Context, command string) (bool, error) {
					calls++
					for i, link := range links {
						_, dest, err := inspectCLITool(link.target)
						if err != nil || dest != before[i] {
							t.Fatalf("partial direct attempt was not restored: %q, %v", dest, err)
						}
						if !strings.Contains(command, shellQuote(link.target)) || (tt.install && !strings.Contains(command, shellQuote(link.source))) {
							t.Fatalf("paths not quoted: %s", command)
						}
					}
					if tt.cancel {
						return true, nil
					}
					if tt.fail {
						return false, runnerErr
					}
					if err := os.Chmod(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					if out, err := exec.CommandContext(ctx, "/bin/sh", "-c", command).CombinedOutput(); err != nil {
						t.Fatalf("quoted command failed: %s: %v", out, err)
					}
					return false, nil
				})
				changed, err := changeCLITool(context.Background(), links, tt.install)
				if calls != 1 || changed != (!tt.cancel && !tt.fail) || (tt.fail && !errors.Is(err, runnerErr)) || (!tt.fail && err != nil) {
					t.Fatalf("retry: calls=%d, changed=%v, err=%v", calls, changed, err)
				}
				for i, link := range links {
					state, dest, err := inspectCLITool(link.target)
					want, wantDest := cliToolAbsent, ""
					if tt.cancel || tt.fail {
						wantDest = before[i]
						if wantDest != "" {
							want = cliToolOurs
						}
					} else if tt.install {
						want, wantDest = cliToolOurs, link.source
					}
					if err != nil || state != want || dest != wantDest {
						t.Fatalf("after retry: state=%v, dest=%q, err=%v", state, dest, err)
					}
				}
			})
		}
	}
}

func TestCLIToolQuoting(t *testing.T) {
	path := "/tmp/App's folder/$HOME `echo no`/\"sheaf\""
	want := "'/tmp/App'\"'\"'s folder/$HOME `echo no`/\"sheaf\"'"
	if got := shellQuote(path); got != want {
		t.Fatalf("shell quote = %q; want %q", got, want)
	}
	out, err := exec.Command("/bin/sh", "-c", "printf '%s' "+shellQuote(path)).Output()
	if err != nil || string(out) != path {
		t.Fatalf("shell round trip = %q, %v", out, err)
	}
	command := cliToolLinksShellCommand([]cliToolLink{{path, path + " target"}, {path + " completion", path + " completion target"}}, []string{"", ""}, true)
	script := cliToolAppleScript(command)
	quoted := strings.TrimSuffix(strings.TrimPrefix(script, "do shell script "), " with administrator privileges")
	if decoded, err := strconv.Unquote(quoted); err != nil || decoded != command {
		t.Fatalf("AppleScript quote = %q, %v", decoded, err)
	}
}

func TestCLIToolPrivilegedCommandRejectsChangedTarget(t *testing.T) {
	for _, kind := range []string{"absent", "ours-broken"} {
		for _, install := range []bool{true, false} {
			for replaced := range 2 {
				links := cliToolFixture(t, [2]string{kind, kind})
				dests, err := inspectCLIToolLinks(links, install)
				if err != nil {
					t.Fatal(err)
				}
				command := cliToolLinksShellCommand(links, dests, install)
				if dests[replaced] != "" {
					if err := os.Remove(links[replaced].target); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(links[replaced].target, []byte("replacement"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := exec.Command("/bin/sh", "-c", command).Run(); err == nil && (install || kind != "absent") {
					t.Fatal("privileged command accepted a foreign replacement")
				}
				if out, err := os.ReadFile(links[replaced].target); err != nil || string(out) != "replacement" {
					t.Fatalf("foreign replacement modified: %q, %v", out, err)
				}
				other := 1 - replaced
				_, dest, err := inspectCLITool(links[other].target)
				if err != nil || dest != dests[other] {
					t.Fatalf("other link changed before preflight finished: %q, %v", dest, err)
				}
			}
		}
	}
}
