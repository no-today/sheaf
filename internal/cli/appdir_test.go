package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/session"
	"sheaf/internal/testutil"
)

func TestCLIDataRootsAndDevIsolation(t *testing.T) {
	testutil.Isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHEAF_HOME", "")
	workspace := t.TempDir()
	cfg := session.Config{Root: workspace, Tracked: []string{"PAY-142"}, Archived: []string{"PAY-142"}, MergeFlow: []session.MergeStep{{Name: "production", Branch: "master"}}}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".sheaf", "workspaces", session.WorkspaceID(workspace), "workspace.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"sheaf", "--workspace", workspace, "config", "show", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s %s", code, &stdout, &stderr)
	}
	prod := decode[response](t, stdout.Bytes())
	if prod.DataDir != filepath.Join(home, ".sheaf") || prod.ConfigFile != configPath || !bytes.Equal(readFile(t, configPath), b) {
		t.Fatal("production config changed")
	}
	actual := decode[session.Config](t, prod.Result)
	if len(actual.Archived) != 1 || actual.Archived[0] != "PAY-142" {
		t.Fatal(actual)
	}
	stdout.Reset()
	if code := Main([]string{"sheaf-dev", "--workspace", workspace, "config", "show", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, &stdout)
	}
	dev := decode[response](t, stdout.Bytes())
	if dev.DataDir != filepath.Join(home, ".sheaf-dev") || len(decode[session.Config](t, dev.Result).Tracked) != 0 {
		t.Fatal("dev read production config")
	}
}
