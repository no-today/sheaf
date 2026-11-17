package watch

import (
	"os"
	"path/filepath"
	"testing"
)

// A file written outside every repo (Finder's .DS_Store) must not rescan the workspace; a new directory must.
func TestOutside(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	os.Mkdir(filepath.Join(root, "a"), 0o755)
	var o Outside
	other := filepath.Join(root, "a")
	if !o.Changed([]string{other}) {
		t.Error("a directory not seen before counts as changed")
	}
	o.Reset(root)
	os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("x"), 0o644)
	if o.Changed([]string{root}) {
		t.Error("a new file is not a change")
	}
	os.Mkdir(filepath.Join(root, "new-repo"), 0o755)
	if !o.Changed([]string{root}) {
		t.Error("a new directory is a change")
	}
	if o.Changed([]string{root}) {
		t.Error("the new listing is remembered")
	}
}
