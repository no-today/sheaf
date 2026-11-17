package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sheaf/internal/appdir"
	"sheaf/internal/gitcli"
	"sheaf/internal/session"
)

func TestWorkspacesAbsentData(t *testing.T) {
	data := filepath.Join(t.TempDir(), "absent")
	t.Setenv("SHEAF_HOME", data)
	t.Chdir(t.TempDir())
	out := invoke(t, 0, "workspaces")
	result := decode[workspacesResult](t, out.Result)
	if !out.Complete || out.Error != nil || out.Workspace != "" || out.ConfigFile != "" || out.DataDir != data || out.Scope.Command != "workspaces" || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("unexpected response: %+v, %+v", out, result)
	}
	if got := human(t, 0, "workspaces"); got != "No known workspaces; use sheaf --workspace <root> status to register one.\n" {
		t.Fatal(got)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("absent data directory changed: %v", err)
	}
}

func TestWorkspacesReadOnly(t *testing.T) {
	data, base := t.TempDir(), t.TempDir()
	t.Setenv("SHEAF_HOME", data)
	root, missing := filepath.Join(base, "a-workspace"), filepath.Join(base, "b-missing")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	configFile := filepath.Join(data, "workspaces", session.WorkspaceID(root), "workspace.json")
	if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(session.Config{Root: root, Tracked: []string{"PAY-1", "PAY-2", "PAY-3"}, Archived: []string{"PAY-2", "PAY-4"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, b, 0o600); err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(map[string]any{"open": []string{missing, root}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "recent.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	// A held workspace lock must not block listing metadata.
	unlock, err := appdir.TryLock(t.Context(), data, session.WorkspaceID(root))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	before := workspaceFiles(t, data)
	want := []session.KnownWorkspace{
		{Root: root, Name: "a-workspace", ID: session.WorkspaceID(root), Exists: true, Current: true, Tracked: 2, Archived: 1, ConfigFile: configFile},
		{Root: missing, Name: "b-missing", ID: session.WorkspaceID(missing)},
	}
	recorder := &gitcli.Recorder{}
	var e envelope
	if exit := run(gitcli.WithRecorder(t.Context(), recorder), "sheaf", options{command: "workspaces"}, &e, &bytes.Buffer{}); exit != 0 {
		t.Fatalf("exit %d: %+v", exit, e)
	}
	if len(recorder.Events()) != 0 {
		t.Fatalf("workspaces called Git: %+v", recorder.Events())
	}
	out := invoke(t, 0, "workspaces")
	got := decode[workspacesResult](t, out.Result)
	if !reflect.DeepEqual(got.Items, want) || out.DataDir != data || out.Workspace != "" || !out.Complete || out.Error != nil || out.Scope.Command != "workspaces" {
		t.Fatalf("unexpected response: %+v, %+v", out, got)
	}
	wantText := "a-workspace  " + root + "  tracked 2, archived 1 (current)\nb-missing  " + missing + "  tracked 0, archived 0 (missing)\n"
	if got := human(t, 0, "workspaces"); got != wantText {
		t.Fatalf("got %q, want %q", got, wantText)
	}
	if after := workspaceFiles(t, data); !reflect.DeepEqual(before, after) {
		t.Fatalf("workspaces changed data files: before %v, after %v", before, after)
	}
}

func workspaceFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files[path] = string(readFile(t, path))
		} else {
			files[path] = "directory"
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestWorkspacesRejectsOtherOptionsAndArguments(t *testing.T) {
	t.Setenv("SHEAF_HOME", filepath.Join(t.TempDir(), "absent"))
	for _, args := range [][]string{
		{"workspaces", "--workspace", "x"}, {"--workspace", "x", "workspaces"},
		{"workspaces", "--fetch"}, {"workspaces", "--rescan"},
		{"workspaces", "--all"}, {"workspaces", "--archived"}, {"workspaces", "--full"},
		{"workspaces", "--search", "x"}, {"workspaces", "--repo", "x"},
		{"workspaces", "--cursor", "x"}, {"workspaces", "--stage", "x"},
		{"workspaces", "--limit", "1"}, {"workspaces", "--version"}, {"workspaces", "x"},
	} {
		out := invoke(t, 3, args...)
		if out.Error == nil || out.Error.Code != "invalid_input" || !strings.Contains(out.Error.Message, "Usage: sheaf workspaces") {
			t.Fatalf("%v: %+v", args, out)
		}
	}
}

func TestWorkspacesDefaultRoots(t *testing.T) {
	t.Setenv("SHEAF_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(filepath.Join(home, "Library"), []byte("unrelated home data"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		binary, data string
	}{{"sheaf", ".sheaf"}, {"sheaf-dev", ".sheaf-dev"}} {
		var stdout, stderr bytes.Buffer
		if exit := Main([]string{tc.binary, "workspaces", "--json"}, &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
			t.Fatalf("%s: exit %d, stdout %s, stderr %s", tc.binary, exit, &stdout, &stderr)
		}
		out := decode[response](t, stdout.Bytes())
		items := decode[workspacesResult](t, out.Result).Items
		data := filepath.Join(home, tc.data)
		if out.DataDir != data || !out.Complete || out.Error != nil || items == nil || len(items) != 0 {
			t.Fatalf("%s: %+v, %+v", tc.binary, out, items)
		}
		if _, err := os.Stat(data); !os.IsNotExist(err) {
			t.Fatalf("%s: absent data directory changed: %v", tc.binary, err)
		}
	}
	if got := string(readFile(t, filepath.Join(home, "Library"))); got != "unrelated home data" {
		t.Fatalf("unrelated home data changed: %q", got)
	}
}

func TestWorkspacesUnreadableData(t *testing.T) {
	data := filepath.Join(t.TempDir(), "not-a-directory")
	t.Setenv("SHEAF_HOME", data)
	if err := os.WriteFile(data, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := invoke(t, 2, "workspaces")
	if out.Complete || out.Error == nil || out.Error.Code != "data_directory" || out.DataDir != data {
		t.Fatal(out)
	}
}
