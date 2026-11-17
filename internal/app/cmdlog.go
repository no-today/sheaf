package app

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"sheaf/internal/session"
)

// revealCommandLog shows the workspace's command log file in Finder (its folder when nothing was logged yet).
func revealCommandLog(s *session.Session) {
	path := s.CommandLogPath()
	if _, err := os.Stat(path); err != nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		mygo.Shell.OpenPath(filepath.Dir(path))
		return
	}
	mygo.Shell.ShowItemInFolder(path)
}

// revealCommandLogFor is the menu item: the log of the focused workspace window (or of the one an aux window belongs to).
func (w *Workspace) revealCommandLogFor(win *mygo.Window) {
	if p := w.mainOf(win); p != nil {
		win = p
	}
	w.mu.Lock()
	s := w.sessions[win.ID()]
	w.mu.Unlock()
	if s != nil {
		revealCommandLog(s)
	}
}
