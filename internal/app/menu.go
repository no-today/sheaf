package app

import (
	"context"
	"errors"
	"strings"

	"github.com/egoist/mygo"
)

// The system draws the menu bar; its custom texts live here. Role menus (App, Edit, View, Window) are translated by mygo.
type menuText struct {
	file, openFolder, openRecent, noRecent string
	noRepos, notDir                        string
	commandLog, settings                   string
	busy                                   string
}

var menuTexts = map[string]menuText{
	"zh": {"文件", "打开…", "最近打开", "暂无记录",
		"这里没有代码仓库，请选代码所在的文件夹。", "请选择一个文件夹。", "操作记录", "设置…",
		"另一个程序正在写入这个工作区，请稍后再打开。"},
	"en": {"File", "Open…", "Open Recent", "No Recent Workspaces",
		"No code repositories here. Pick the folder with your code.", "Please choose a folder.", "Activity Log", "Settings…",
		"Another program is writing to this workspace. Try opening it again shortly."},
}

// text picks the language of the settings; "" follows the system (Chinese when the system language is, else English).
func (m *Windows) text() menuText {
	lang := m.settings.Get().Lang
	if lang == "" && strings.HasPrefix(mygo.App.Locale(), "zh") {
		lang = "zh"
	}
	if t, ok := menuTexts[lang]; ok {
		return t
	}
	return menuTexts["en"]
}

// RebuildMenu installs the menu bar again: after the language or the recent list changed.
func (m *Windows) RebuildMenu() {
	tx := m.text()
	var recent []*mygo.MenuItem
	for _, r := range m.recents.List() {
		if r.Missing {
			continue
		}
		recent = append(recent, &mygo.MenuItem{Label: r.Name, ToolTip: r.Root, Click: func(*mygo.MenuItem, *mygo.Window) { go m.openFromMenu(r.Root) }})
		if len(recent) == 10 {
			break
		}
	}
	if len(recent) == 0 {
		recent = []*mygo.MenuItem{{Label: tx.noRecent, Disabled: true}}
	}
	appMenu := []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		mygo.Separator(),
		// The settings of the focused workspace window (or of the window an aux window belongs to).
		{Label: tx.settings, Accelerator: "CmdOrCtrl+,", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
			if win != nil {
				go m.openSettingsFor(win)
			}
		}},
	}
	appMenu = append(appMenu,
		mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleServices},
		mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleHide},
		&mygo.MenuItem{Role: mygo.RoleHideOthers},
		&mygo.MenuItem{Role: mygo.RoleUnhide},
		mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleQuit},
	)
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: appMenu},
		{Label: tx.file, Submenu: []*mygo.MenuItem{
			{Label: tx.openFolder, Accelerator: "CmdOrCtrl+O", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
				go func() {
					if err := m.OpenFolderDialog(context.Background(), win); err != nil {
						m.report("", err)
					}
				}()
			}},
			{Label: tx.openRecent, Submenu: recent},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize},
			{Role: mygo.RoleZoom},
			mygo.Separator(),
			// The command log file of the focused workspace window (or of the window an aux window belongs to); nothing without one.
			{Label: tx.commandLog, Accelerator: "Alt+CmdOrCtrl+L", Click: func(_ *mygo.MenuItem, win *mygo.Window) {
				if win != nil {
					go m.ws.revealCommandLogFor(win)
				}
			}},
			mygo.Separator(),
			{Role: mygo.RoleFront},
		}},
	}))
}

// openSettingsFor opens the settings of win's workspace; nothing when win belongs to none.
func (m *Windows) openSettingsFor(win *mygo.Window) {
	if p := m.ws.mainOf(win); p != nil {
		win = p
	}
	m.ws.mu.Lock()
	s := m.ws.sessions[win.ID()]
	m.ws.mu.Unlock()
	if s == nil {
		return
	}
	if err := (nativeWindows{ws: m.ws, wins: m, win: &win}).OpenSettings(""); err != nil {
		mygo.Dialog.Error("Sheaf", err.Error())
	}
}

func (m *Windows) openFromMenu(root string) {
	if err := m.OpenWorkspace(context.Background(), root); err != nil {
		m.report(root, err)
	}
}

// report tells the user why a folder did not open from the menu or a file drop.
func (m *Windows) report(root string, err error) {
	tx := m.text()
	msg := err.Error()
	switch {
	case errors.Is(err, errNoRepos):
		msg = tx.noRepos
	case errors.Is(err, errNotDir):
		msg = tx.notDir
	case errors.Is(err, errWorkspaceBusy):
		msg = tx.busy
	}
	if root != "" {
		msg = root + "\n" + msg
	}
	mygo.Dialog.Error("Sheaf", msg)
}
