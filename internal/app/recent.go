package app

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"time"
)

// Recent is one entry of the Open Recent menu.
type Recent struct {
	Root     string `json:"root"`
	Name     string `json:"name"`
	OpenedAt int64  `json:"openedAt"` // unix seconds
	Missing  bool   `json:"missing"`  // the folder is gone (computed, not stored)
}

const maxRecents = 50

type recentFile struct {
	Recent []Recent `json:"recent"`
	// Open lists the roots restored at the next start, oldest first.
	Open []string `json:"open"`
}

// RecentStore is recent.json: the recent workspaces and the ones open now.
type RecentStore struct {
	mu     sync.Mutex
	path   string
	now    func() time.Time
	f      recentFile
	frozen bool // the app is quitting: windows closing now must not leave the open list
}

func openRecents(path string, now func() time.Time) *RecentStore {
	s := &RecentStore{path: path, now: now}
	readJSON(path, &s.f)
	return s
}

// List is newest first, with current directory status.
func (s *RecentStore) List() []Recent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recent, len(s.f.Recent))
	copy(out, s.f.Recent)
	for i := range out {
		st, err := os.Stat(out[i].Root)
		out[i].Missing = err != nil || !st.IsDir()
	}
	return out
}

// Touch records that root was opened now.
func (s *RecentStore) Touch(root, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := Recent{Root: root, Name: name, OpenedAt: s.now().Unix()}
	if i := s.index(root); i >= 0 {
		s.f.Recent = slices.Delete(s.f.Recent, i, i+1)
	}
	s.f.Recent = slices.Insert(s.f.Recent, 0, r)
	if len(s.f.Recent) > maxRecents {
		s.f.Recent = s.f.Recent[:maxRecents]
	}
	return s.save()
}

func (s *RecentStore) index(root string) int {
	return slices.IndexFunc(s.f.Recent, func(r Recent) bool { return r.Root == root })
}

// MarkOpen / MarkClosed keep the list of open windows. After Freeze, MarkClosed does nothing.
func (s *RecentStore) MarkOpen(root string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.f.Open, root) {
		return nil
	}
	s.f.Open = append(s.f.Open, root)
	return s.save()
}

func (s *RecentStore) MarkClosed(root string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.Index(s.f.Open, root)
	if s.frozen || i < 0 {
		return nil
	}
	s.f.Open = slices.Delete(s.f.Open, i, i+1)
	return s.save()
}

// Freeze is called when the app starts to quit: the windows that close with it stay in the open list.
func (s *RecentStore) Freeze() {
	s.mu.Lock()
	s.frozen = true
	s.mu.Unlock()
}

// OpenRoots are the workspaces that were open at the last quit (folders that are gone are left out).
func (s *RecentStore) OpenRoots() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, r := range s.f.Open {
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

func (s *RecentStore) save() error {
	if err := writeJSON(s.path, s.f); err != nil {
		return fmt.Errorf("save recent workspaces: %w", err)
	}
	return nil
}
