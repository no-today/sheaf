// Package watch turns file system changes in a workspace into what to
// refresh: which repos' refs may have moved and which worktrees' uncommitted
// changes may differ. It only reads; Source implementations never touch the
// repositories.
package watch

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"sheaf/internal/discover"

	"github.com/egoist/mygo"
)

// Source reports directories (absolute) in or under which something changed.
type Source interface {
	Events() <-chan []string
	Close() error
}

// Parts of a git dir that change only together with refs or never matter.
var skipGitDirs = map[string]bool{"objects": true, "logs": true, "hooks": true, "lfs": true}

// ignore is asked about directories, relative to the watched root, slash separated.
func ignore(p string, isDir bool) bool {
	if !isDir {
		return false
	}
	base := path.Base(p)
	return discover.SkipDir(base) || skipGitDirs[base] && strings.Contains("/"+p, "/.git/")
}

// Files watches roots recursively with mygo's file watch (FSEvents on macOS:
// one stream per root, no descriptor per directory). It asks only for hints,
// so it never stats or reads the files.
func Files(ctx context.Context, roots []string) (Source, error) {
	ctx, cancel := context.WithCancel(ctx)
	f := &files{cancel: cancel, ch: make(chan []string, 64)}
	for _, r := range roots {
		w, err := mygo.WatchFiles(ctx, r, mygo.FileWatchOptions{Recursive: true, HintsOnly: true, Ignore: ignore})
		if err != nil {
			f.Close()
			return nil, err
		}
		f.wg.Add(1)
		go f.pump(ctx, w)
	}
	return f, nil
}

type files struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	ch     chan []string
	once   sync.Once
}

func (f *files) Events() <-chan []string { return f.ch }

// Close stops every watch and waits for their goroutines; Events is then closed.
func (f *files) Close() error {
	f.once.Do(func() {
		f.cancel()
		f.wg.Wait()
		close(f.ch)
	})
	return nil
}

func (f *files) pump(ctx context.Context, w *mygo.FileWatcher) {
	defer f.wg.Done()
	defer w.Close()
	root := w.Root() // symlinks resolved
	for {
		evs, err := w.Next(ctx)
		var dirs []string
		for _, e := range evs {
			dirs = append(dirs, filepath.Join(root, filepath.FromSlash(e.Path)))
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			dirs = append(dirs, root) // the root went away or the watch failed: look at everything
		}
		if len(dirs) > 0 {
			select {
			case f.ch <- dirs:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// Roots is the fewest directories that cover dirs: the workspace root, plus
// any worktree or git dir outside it.
func Roots(dirs []string) []string {
	var clean []string
	for _, d := range dirs {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			d = r
		}
		clean = append(clean, filepath.Clean(d))
	}
	slices.Sort(clean)
	var out []string
	for _, d := range clean {
		if len(out) > 0 && under(d, out[len(out)-1]) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func under(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}
