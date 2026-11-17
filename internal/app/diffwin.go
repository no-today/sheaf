package app

import (
	"encoding/json"
	"fmt"
	"path"

	"github.com/egoist/mygo"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

// diffWindow is the key (per workspace window) and the options of src's diff window.
func diffWindow(src session.DiffSource) (string, mygo.WindowOptions, error) {
	raw, err := json.Marshal(src)
	if err != nil {
		return "", mygo.WindowOptions{}, err
	}
	return "diff\x00" + string(raw), mygo.WindowOptions{
		Title:     path.Base(src.Path),
		Width:     1200,
		Height:    780,
		MinWidth:  560,
		MinHeight: 360,
		StateKey:  "diff",
	}, nil
}

// openAuxOf opens a native window sharing its workspace's session and lifetime. A window with the same key is raised.
func (w *Workspace) openAuxOf(parent *mygo.Window, what string, opts mygo.WindowOptions, content nativeMaker) error {
	s, err := w.sessionFor(parent)
	if err != nil {
		return err
	}
	if p := w.mainOf(parent); p != nil {
		parent = p
	}
	key := fmt.Sprintf("%d\x00%s", parent.ID(), what)
	w.mu.Lock()
	id, same := findDiff(w.diffKeys, key)
	old := w.diffs[id]
	w.mu.Unlock()
	if same && old != nil {
		old.Show()
		return nil
	}
	opts.BackgroundColor = theme.FrameHex
	opts.TitleBarStyle = mygo.TitleBarHidden
	opts.Hidden = true
	win := openNativeWindow(opts, func(update func(func())) nativeContent { return content(s, update) })
	w.mu.Lock()
	w.sessions[win.ID()] = s
	w.diffs[win.ID()] = win
	w.diffKeys[win.ID()] = key
	w.parent[win.ID()] = parent
	w.mu.Unlock()
	off := parent.OnClosed(win.Close)
	win.OnClosed(func() {
		off()
		w.mu.Lock()
		if w.sessions[win.ID()] == s {
			delete(w.sessions, win.ID())
		}
		delete(w.diffs, win.ID())
		delete(w.diffKeys, win.ID())
		delete(w.parent, win.ID())
		w.mu.Unlock()
	})
	win.Show()
	return nil
}

// mainOf is the workspace window an auxiliary window belongs to; nil for any other window.
func (w *Workspace) mainOf(win *mygo.Window) *mygo.Window {
	if win == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.parent[win.ID()]
}

// findDiff is the open diff window whose key is key: the same workspace window, source and file.
func findDiff(keys map[int]string, key string) (int, bool) {
	for id, k := range keys {
		if k == key {
			return id, true
		}
	}
	return 0, false
}
