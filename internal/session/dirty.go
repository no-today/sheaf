package session

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"sheaf/internal/gitview"
)

// dirtyTracker caches the uncommitted file count of each worktree (git status).
// The zero value is ready to use.
type dirtyTracker struct {
	mu           sync.Mutex
	n            map[string]int    // worktree path -> uncommitted files
	at           map[string]uint64 // generation of the git status behind n[path]
	gen          uint64            // last generation handed out by ticket
	fingerprints map[string][sha256.Size]byte

	list  map[string]dirtyList // worktree path -> files of the last status run, until something changes there
	epoch map[string]uint64    // bumped by every invalidate: a run that started before it must not be stored

	// status is the last count run's git status of a directory whose files changed, so that the 未提交 page, which reads the
	// directory again right after the count is pushed, does not run the same git status a second time.
	status map[string]statusAt
}

type statusAt struct {
	st    gitview.WorktreeStatus
	epoch uint64
}

// putStatus keeps a count run's status for the list, unless the directory was invalidated since (epoch from listOf).
func (d *dirtyTracker) putStatus(path string, epoch uint64, st gitview.WorktreeStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.epoch[path] != epoch {
		return
	}
	if d.status == nil {
		d.status = map[string]statusAt{}
	}
	d.status[path] = statusAt{st, epoch}
}

// statusOf reads the current cached status without consuming it.
func (d *dirtyTracker) statusOf(path string) (gitview.WorktreeStatus, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.status[path]
	if !ok || s.epoch != d.epoch[path] {
		return gitview.WorktreeStatus{}, false
	}
	return s.st, true
}

// takeStatus hands out (once) the status kept for this epoch of the directory.
func (d *dirtyTracker) takeStatus(path string, epoch uint64) (gitview.WorktreeStatus, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.status[path]
	if !ok || s.epoch != epoch {
		return gitview.WorktreeStatus{}, false
	}
	delete(d.status, path)
	return s.st, true
}

// dirtyList is what the 未提交 page needs from one working directory.
type dirtyList struct {
	files      []AggFile
	lastChange int64
}

// listOf returns the cached list and the epoch a fresh run must be stored under.
func (d *dirtyTracker) listOf(path string) (dirtyList, bool, uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.epoch == nil {
		d.epoch = map[string]uint64{}
	}
	// A full invalidation must see this directory even before its first list is stored.
	epoch := d.epoch[path]
	d.epoch[path] = epoch
	l, ok := d.list[path]
	return l, ok, epoch
}

// storeList keeps a run's list unless the directory was invalidated since the run began (epoch from listOf).
func (d *dirtyTracker) storeList(path string, epoch uint64, l dirtyList) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.epoch[path] != epoch {
		return
	}
	if d.list == nil {
		d.list = map[string]dirtyList{}
	}
	d.list[path] = l
}

// invalidate drops the cached lists of the given directories (all when paths is nil): their files changed.
func (d *dirtyTracker) invalidate(paths []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.epoch == nil {
		d.epoch = map[string]uint64{}
	}
	if paths == nil {
		for p := range d.epoch {
			paths = append(paths, p)
		}
	}
	for _, p := range paths {
		delete(d.list, p)
		delete(d.status, p)
		d.epoch[p]++
	}
}

// epochOf counts the invalidations of path: what its working tree shows changed when it changed.
func (d *dirtyTracker) epochOf(path string) uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.epoch[path]
}

// get is the cached count; -1 = not known yet.
func (d *dirtyTracker) get(path string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n, ok := d.n[path]; ok {
		return n
	}
	return -1
}

// ticket numbers a git status about to run: a full refresh and a watch-triggered one may overlap,
// and only the newest status may be stored.
func (d *dirtyTracker) ticket() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gen++
	return d.gen
}

// store keeps the count and fingerprint together so overlapping runs cannot publish an older diff.
func (d *dirtyTracker) store(path string, g uint64, n int, fingerprint [sha256.Size]byte) (changed, first, stale bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if g < d.at[path] {
		return false, false, true
	}
	if d.at == nil {
		d.at, d.n = map[string]uint64{}, map[string]int{}
	}
	if d.fingerprints == nil {
		d.fingerprints = map[string][sha256.Size]byte{}
	}
	old, had := d.fingerprints[path]
	d.at[path], d.n[path], d.fingerprints[path] = g, n, fingerprint
	return had && old != fingerprint, !had, false
}

func dirtyFingerprint(ctx context.Context, worktree string, st gitview.WorktreeStatus) ([sha256.Size]byte, error) {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n", st.Head())
	for _, f := range st.Files() {
		if err := ctx.Err(); err != nil {
			return [sha256.Size]byte{}, fmt.Errorf("fingerprint: %w", err)
		}
		fmt.Fprintf(h, "%q %q %q %q\n", f.Path, f.OldPath, f.Area, f.Status)
		for _, path := range []string{f.Path, f.OldPath} {
			if path == "" {
				continue
			}
			info, err := os.Lstat(filepath.Join(worktree, filepath.FromSlash(path)))
			switch {
			case os.IsNotExist(err):
				fmt.Fprintln(h, "missing")
			case err != nil:
				return [sha256.Size]byte{}, fmt.Errorf("fingerprint %s: %w", path, err)
			default:
				fmt.Fprintf(h, "%d %d\n", info.Size(), info.ModTime().UnixNano())
			}
		}
	}
	var fingerprint [sha256.Size]byte
	h.Sum(fingerprint[:0])
	return fingerprint, nil
}

// set records a count read outside a status run (the list of uncommitted files).
func (d *dirtyTracker) set(path string, n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.n == nil {
		d.at, d.n = map[string]uint64{}, map[string]int{}
	}
	d.n[path] = n
}
