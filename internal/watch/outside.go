package watch

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Outside remembers the subdirectories of watched directories that lie outside every repo,
// to tell a new clone, a move or a removal from noise like Finder's .DS_Store.
// The zero value is ready to use.
type Outside struct {
	mu   sync.Mutex
	seen map[string][]string
}

// Changed tells whether a directory gained or lost subdirectories since it was last seen
// (a repo may have been cloned, moved or removed). Files there need no rescan.
// A directory not seen before counts as changed.
func (o *Outside) Changed(dirs []string) bool {
	changed := false
	for _, d := range dirs {
		names := subdirs(d)
		o.mu.Lock()
		if o.seen == nil {
			o.seen = map[string][]string{}
		}
		old, ok := o.seen[d]
		o.seen[d] = names
		o.mu.Unlock()
		changed = changed || !ok || !slices.Equal(old, names)
	}
	return changed
}

// Reset forgets what was seen and records dir (the workspace root) as the
// watch reports it, with symlinks resolved.
func (o *Outside) Reset(dir string) {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	names := subdirs(dir)
	o.mu.Lock()
	o.seen = map[string][]string{dir: names}
	o.mu.Unlock()
}

func subdirs(dir string) []string {
	es, _ := os.ReadDir(dir) // sorted; a missing dir has none
	out := []string{}
	for _, e := range es {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
