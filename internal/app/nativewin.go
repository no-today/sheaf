package app

import (
	"context"
	"fmt"
	"os"
	"sheaf/internal/native/theme"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans" // mygo's own strings (controls, menus) in Chinese
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/diffwin"
	"sheaf/internal/native/settingswin"
	"sheaf/internal/session"
)

// setNativeLanguage follows the saved language, or the user's preferred languages when unset. Declaring one language
// updates native windows and mygo's controls without writing the app's defaults (as SetLocale would).
// setCodeFont redraws every window with the code font chosen.
func setCodeFont(family string) {
	theme.SetCodeFont(family)
	for _, w := range mygo.Windows() {
		w.Invalidate()
	}
}

func setNativeLanguage(lang string) {
	if lang == "" {
		lang = "en"
		for _, l := range mygo.App.PreferredLanguages() {
			if strings.HasPrefix(strings.ToLower(l), "zh") {
				lang = "zh"
			}
		}
	}
	if lang == "zh" {
		mygo.App.SetLanguages("zh-Hans")
		return
	}
	mygo.App.SetLanguages("en")
}

// nativeContent is what a native window shows (the packages under internal/native).
type nativeContent interface {
	View(c *ui.Context)
	Close()
}

// nativeMaker makes the content of a window that belongs to a workspace window, from that workspace's session.
type nativeMaker func(s *session.Session, update func(func())) nativeContent

// openNativeWindow opens a native window: make gets the window's Update, and the content's Close runs as it closes.
func openNativeWindow(opts mygo.WindowOptions, make func(update func(func())) nativeContent) *mygo.Window {
	u := &lateUpdater{}
	content := make(u.update)
	opts.Content = ui.View(content.View)
	win := mygo.NewWindow(opts)
	u.attach(win.Update)
	win.OnClosed(content.Close)
	if os.Getenv("SHEAF_NATIVE_WIN") == opts.StateKey && opts.StateKey != "" {
		go captureNative(win, nil)
	}
	return win
}

// lateUpdater is the Update of a window that does not exist yet: the content starts background work while it is made, and
// what it asks before the window is there waits and runs in order once attach is called.
type lateUpdater struct {
	mu      sync.Mutex
	send    func(func())
	pending []func()
}

func (u *lateUpdater) update(f func()) {
	u.mu.Lock()
	if u.send == nil {
		u.pending = append(u.pending, f)
		u.mu.Unlock()
		return
	}
	send := u.send
	u.mu.Unlock()
	send(f)
}

func (u *lateUpdater) attach(send func(func())) {
	u.mu.Lock()
	u.send = send
	p := u.pending
	u.pending = nil
	u.mu.Unlock()
	for _, f := range p {
		send(f)
	}
}

// nativeWindows opens a workspace's diff and settings windows: one per key, closing with their workspace.
type nativeWindows struct {
	ws interface {
		openAuxOf(*mygo.Window, string, mygo.WindowOptions, nativeMaker) error
	}
	wins *Windows
	win  **mygo.Window // the workspace window, set once it is made
}

func (n nativeWindows) OpenDiff(src session.DiffSource) error {
	what, opts, err := diffWindow(src)
	if err != nil {
		return err
	}
	return n.ws.openAuxOf(*n.win, what, opts, func(s *session.Session, update func(func())) nativeContent {
		return diffwin.New(s, src, update)
	})
}

func (n nativeWindows) OpenSettings(tab string) error {
	return n.ws.openAuxOf(*n.win, "settings", settingsOptions(), func(s *session.Session, update func(func())) nativeContent {
		return settingswin.New(tab, s, update, n.wins.settingsDeps(s))
	})
}

// OpenIn keeps older view hosts compatible while their external-app actions are removed.
func (n nativeWindows) OpenIn(string, string) error {
	return nil
}

// saveSettings is what the settings do in every window: save, tell the windows, redo the menu, and show the language.
func (m *Windows) saveSettings(s Settings) error {
	if err := m.settings.Set(s); err != nil {
		return err
	}
	setNativeLanguage(s.Lang)
	setCodeFont(s.CodeFont)
	m.RebuildMenu()
	return nil
}

// settingsDeps is what a workspace's settings window asks of the app.
func (m *Windows) settingsDeps(s *session.Session) settingswin.Deps {
	d := settingswin.Deps{
		CLITool: func() settingswin.CLITool { return commandLineToolState(bundledCLITool()) },
		SetCLITool: func(install bool, done func(error)) {
			_, err := changeCLITool(context.Background(), bundledCLITool(), install)
			done(err)
		},
		Settings: func() settingswin.Settings {
			v := m.settings.Get()
			return settingswin.Settings{Lang: v.Lang, CodeFont: v.CodeFont}
		},
		SetSettings: func(v settingswin.Settings) error {
			return m.saveSettings(Settings{Lang: v.Lang, CodeFont: v.CodeFont})
		},
	}
	if s != nil {
		d.RevealData = func() { mygo.Shell.OpenPath(s.DataDir()) }
		d.RevealCommandLog = func() { revealCommandLog(s) }
	}
	return d
}

// captureNative is the native windows' screenshot smoke: with SHEAF_CAPTURE=<png> it writes a 2x
// PNG of win once ready is closed (nil: at once) and SHEAF_NATIVE_WAIT ms more (1500 by default) have passed for the
// content to settle, and with SHEAF_SMOKE=1 then quits.
func captureNative(win *mygo.Window, ready <-chan struct{}) {
	out := os.Getenv("SHEAF_CAPTURE")
	if out == "" {
		return
	}
	if ready != nil {
		<-ready
	}
	wait := 1500 * time.Millisecond
	if ms, err := strconv.Atoi(os.Getenv("SHEAF_NATIVE_WAIT")); err == nil && ms >= 0 {
		wait = time.Duration(ms) * time.Millisecond
	}
	time.Sleep(wait)
	if png, err := win.CapturePage(); err != nil {
		fmt.Println("[capture]", err)
	} else if err := os.WriteFile(out, png, 0o644); err != nil {
		fmt.Println("[capture]", err)
	}
	if os.Getenv("SHEAF_SMOKE") == "1" {
		mygo.App.Quit()
	}
}
