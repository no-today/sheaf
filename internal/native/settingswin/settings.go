// Package settingswin is the settings window of the native UI: the pages for every workspace (general, open with), and,
// opened from a workspace window, those of that workspace (my commits, base branches, index, MR/PR). Changes apply at
// once; there is no save button.
package settingswin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/shell"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

// Settings are the app's settings for every workspace (the app's Settings, which this package cannot import).
type Settings struct {
	Lang     string // "" follow the system | zh | en
	CodeFont string // "" the bundled monospace
}

// CLITool is the bundled command and completion's installation state.
type CLITool struct {
	Available bool
	Installed bool
	Path      string
}

var ErrForeignCLITool = errors.New("command line tool path is occupied")

// Deps is what the settings window asks of the app. SetCLITool runs in the background;
// its completion returns through the window's update path.
type Deps struct {
	Settings         func() Settings
	SetSettings      func(Settings) error
	RevealData       func() // the workspace's data folder in Finder
	RevealCommandLog func()
	CLITool          func() CLITool
	SetCLITool       func(install bool, done func(error))
}

type tabID string

const (
	tabGeneral tabID = "general"
	tabMe      tabID = "me"
	tabBase    tabID = "base"
	tabRepos   tabID = "repos"
	tabIndex   tabID = "index"
	tabGlab    tabID = "glab"
)

var (
	globalTabs    = []tabID{tabGeneral}
	workspaceTabs = []tabID{tabMe, tabBase, tabRepos, tabGlab, tabIndex} // 索引 is rarely needed: last
)

// Window is the settings window's state; every field is used on the main thread.
type Window struct {
	tab     tabID
	s       *session.Session // nil: only the settings for every workspace show
	d       Deps
	update  func(func())
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
	started bool
	toasts  widgets.Toaster

	set     Settings
	cliBusy bool
	cliErr  string // the last install or uninstall failure, shown under the row

	info     session.Info
	infoSeq  atomic.Int64 // bumped by every mutate: an Info read that started before it is older than the change
	haveInfo bool

	adding, touched bool // my commits: the email being added
	draft           string

	glab        string // glab: the directory as typed
	glabFrom    string // what the typed one was taken from, to see the workspace changed it
	glabInit    bool
	glabFocus   bool // the field had the focus in the last frame: leaving it commits
	glabProblem string
	glabChecked *string // the directory glabProblem is about

	req        string // my commits: the requirement pattern as typed
	reqFrom    string // what the typed one was taken from
	reqInit    bool
	reqFocus   bool
	reqProblem string
	reqChecked *string // the pattern reqProblem is about

	branches    map[string][]string // base: the choices per repo, kept until the index changes
	branchStamp string
	names       []string // branches: the branch names of every repo without origin/, kept with branches

	master      string
	targetsFrom string
	targetsInit bool
	masterFocus bool
	flow        []session.MergeStep
	flowFrom    string
	flowFocus   string
	baseSel     map[string]*string // what each repo's field shows
	baseSeen    map[string]string  // the base it was taken from

	repoList   ui.ListState // repos: the rows are built by visible range
	repoSearch string

	refreshing, done bool // index: a refresh runs; "up to date" shows for a moment when it ended
}

// New makes the settings window showing page tab ("" = the first); s is the workspace's session, or nil; update is the
// window's mygo.Window.Update.
func New(tab string, s *session.Session, update func(func()), d Deps) *Window {
	if tab == "" { // the screenshot smoke picks the page without clicking
		tab = os.Getenv("SHEAF_NATIVE_TAB")
	}
	w := &Window{tab: tabID(tab), s: s, d: d, branches: map[string][]string{}, baseSel: map[string]*string{}, baseSeen: map[string]string{}}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.update = func(f func()) {
		update(func() {
			if !w.closed {
				f()
			}
		})
	}
	if d.Settings != nil {
		w.set = d.Settings()
	}
	return w
}

// start begins the background reads. Not in New: the window does not exist yet when the content is made.
func (w *Window) start() {
	w.started = true
	if w.s != nil {
		go w.poll()
	}
}

// Close stops the window's work.
func (w *Window) Close() {
	w.closed = true
	w.cancel()
}

// poll reads Info again every second and tells the window when it changed: the repos arrive with the scan and the index
// state moves while the window is open.
func (w *Window) poll() {
	for {
		seq := w.infoSeq.Load()
		info := w.s.Info()
		w.update(func() {
			if seq != w.infoSeq.Load() {
				return
			}
			if !w.haveInfo || !reflect.DeepEqual(w.info, info) {
				w.info, w.haveInfo = info, true
			}
		})
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// mutate does f (a change of the workspace's settings) in the background, then reads Info again; its error shows as a toast.
func (w *Window) mutate(f func() error) {
	seq := w.infoSeq.Add(1)
	go func() {
		err := f()
		info := w.s.Info()
		w.update(func() {
			if err != nil {
				w.toasts.Error(err.Error())
			}
			if seq == w.infoSeq.Load() {
				w.info, w.haveInfo = info, true
				if err != nil {
					w.reqInit, w.glabInit, w.targetsInit = false, false, false
					w.flowFrom = ""
					w.baseSel, w.baseSeen = map[string]*string{}, map[string]string{}
				}
			}
		})
	}()
}

func (w *Window) save(f func(*Settings)) {
	// Another settings window may have saved since this one read: change only what f touches, on what is stored now.
	cur := w.set
	if w.d.Settings != nil {
		cur = w.d.Settings()
	}
	next := cur
	f(&next)
	if next == cur {
		w.set = cur
		return
	}
	w.set = next
	if w.d.SetSettings == nil {
		return
	}
	if err := w.d.SetSettings(next); err != nil {
		w.set = cur
		w.toasts.Error(err.Error())
	}
}

func (w *Window) tabs() []tabID {
	if w.s == nil {
		return globalTabs
	}
	return slices.Concat(globalTabs, workspaceTabs)
}

func (w *Window) shown() tabID {
	if slices.Contains(w.tabs(), w.tab) {
		return w.tab
	}
	return tabGeneral
}

func (w *Window) label(t *words, id tabID) string {
	switch id {
	case tabMe:
		return t.TabMe
	case tabBase:
		return t.TabBase
	case tabRepos:
		return t.TabRepos
	case tabIndex:
		return t.TabIndex
	case tabGlab:
		return t.TabGlab
	}
	return t.TabGeneral
}

// View builds the settings window.
func (w *Window) View(c *ui.Context) {
	c.SetTheme(theme.Theme())
	if !w.started {
		w.start()
	}
	if w.d.Settings != nil { // another settings window may have changed them
		w.set = w.d.Settings()
	}
	contentFocused := false
	t := tr.Of(c)
	shown := w.shown()
	width, _ := c.Size()
	railWidth := float32(180)
	if width <= 640 {
		railWidth = 160
	}
	shell.Window(c, i18n.Of(c).Settings, nil, func(c *ui.Context) {
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(railWidth).Shrink(0).PaddingY(16).Background(theme.Rail).
				Border(theme.Gap, theme.Line).BorderWidth(0, theme.Gap, 0, 0).AlignItems(ui.Stretch).Children(func() {
				w.navigationItem(c, t, tabGeneral, shown)
				if w.s != nil {
					ui.Text(c, t.Workspace+" "+w.info.Name).FontSize(theme.FSLabel).TextColor(theme.Text4).
						SingleLine().Margin(24, 16, 8, 16).Tooltip(w.info.Name)
					for _, id := range workspaceTabs {
						w.navigationItem(c, t, id, shown)
					}
				}
			})
			content := ui.Scroll(c.Key(string(shown))).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
				ui.Column(c).AlignItems(ui.Stretch).Padding(28, 40, 28, 40).Children(func() {
					ui.Column(c).MaxWidth(640).MinWidth(0).AlignItems(ui.Stretch).AlignSelf(ui.Stretch).Gap(20).Children(func() {
						switch shown {
						case tabGeneral:
							w.general(c, t)
						default:
							if !w.haveInfo {
								ui.Text(c, t.Loading).TextColor(theme.Text4)
								return
							}
							switch shown {
							case tabMe:
								w.me(c, t)
							case tabBase:
								w.base(c, t)
							case tabRepos:
								w.repos(c, t)
							case tabIndex:
								w.index(c, t)
							case tabGlab:
								w.glabPage(c, t)
							}
						}
					})
				})
			})
			contentFocused = content.FocusWithin()
		})
	}, nil)
	if !contentFocused {
		widgets.Shortcuts(c,
			widgets.Shortcut{Key: ui.KeyUp, Do: func() { w.moveTab(-1) }},
			widgets.Shortcut{Key: ui.KeyDown, Do: func() { w.moveTab(1) }},
		)
	}
	w.toasts.View(c)
}

func (w *Window) moveTab(delta int) {
	tabs := w.tabs()
	index := slices.Index(tabs, w.shown())
	w.tab = tabs[max(0, min(index+delta, len(tabs)-1))]
}

func (w *Window) navigationItem(c *ui.Context, t *words, id, shown tabID) {
	label := w.label(t, id)
	row := widgets.RowLook(ui.ButtonBase(c.Key("nav "+string(id))).Height(theme.HRow).PaddingX(16).
		Justify(ui.Start).Label(label), id == shown)
	row.Children(func() {
		ui.Text(c, label).FontSize(theme.FSBody).TextColor(theme.Text).SingleLine()
	})
	if row.Clicked() {
		w.tab = id
	}
}

func pageTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(theme.FSPage).FontWeight(600).TextColor(theme.Text)
}

func pageSection(c *ui.Context, title, hint string, content func()) {
	ui.Column(c).AlignItems(ui.Stretch).Gap(16).Children(func() {
		ui.Column(c).AlignItems(ui.Stretch).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(theme.FSBody).FontWeight(600).TextColor(theme.Text)
			if hint != "" {
				ui.Text(c, hint).FontSize(theme.FSSmall).TextColor(theme.Text4).Wrap()
			}
		})
		content()
	})
}

func sectionRule(c *ui.Context) {
	ui.Box(c).Height(theme.Gap).Background(theme.Line).MarginY(24)
}

func settingRow(c *ui.Context, title, hint string, extra, control func()) {
	ui.Row(c).MinHeight(56).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(theme.FSBody).TextColor(theme.Text).Wrap()
			if hint != "" {
				ui.Text(c, hint).FontSize(theme.FSLabel).TextColor(theme.Text4).Wrap().MaxLines(2).Tooltip(hint)
			}
			if extra != nil {
				extra()
			}
		})
		ui.Column(c).Width(240).Shrink(0).AlignItems(ui.Stretch).Children(control)
	})
}

func (w *Window) general(c *ui.Context, t *words) {
	langs := []string{"", "zh", "en"}
	idx := slices.Index(langs, w.set.Lang)
	pageTitle(c, t.TabGeneral)
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		pageSection(c, t.Lang, t.LangHint, func() {
			settingRow(c, t.Lang, "", nil, func() {
				prev := idx
				widgets.Segmented(c, &idx, t.Lang, t.LangAuto, t.LangZh, t.LangEn)
				if idx != prev && idx >= 0 {
					w.save(func(s *Settings) { s.Lang = langs[idx] })
				}
			})
		})
		sectionRule(c)
		pageSection(c, t.CodeFont, t.CodeFontHint, func() {
			settingRow(c, t.CodeFont, "", nil, func() {
				font := cmp.Or(w.set.CodeFont, theme.BundledCode)
				before := font
				ui.Combobox(c, &font, theme.CodeFonts).Width(240).Height(theme.HSelect).Font(theme.MonoFont).FontSize(theme.FSSmall).
					Placeholder(theme.BundledCode).Label(t.CodeFont)
				if font != before {
					if font == theme.BundledCode {
						font = ""
					}
					w.save(func(s *Settings) { s.CodeFont = font })
				}
			})
		})
		if w.d.CLITool != nil {
			if tool := w.d.CLITool(); tool.Available {
				sectionRule(c)
				w.commandLineTool(c, t, tool)
			}
		}
		sectionRule(c)
		pageSection(c, t.About, fmt.Sprintf(t.AppVersion, cmp.Or(mygo.App.Version(), developmentVersion)), func() {})
	})
}

// Packaged builds read their version from mygo.json through mygo.App.Version.
const developmentVersion = "0.1.0"

func (w *Window) me(c *ui.Context, t *words) {
	info := w.info
	pageTitle(c, t.TabMe)
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		pageSection(c, t.MeHead, t.MeHint, func() {
			for _, m := range info.MeAuto {
				settingRow(c, m.Email, fromText(t, m), nil, func() {})
			}
			for _, email := range info.MeManual {
				settingRow(c, email, t.Manual, nil, func() {
					ui.Row(c).Justify(ui.End).Children(func() {
						if widgets.IconButton(c, theme.IconClose, fmt.Sprintf(t.RemoveEmail, email)).Clicked() {
							prev := info.MeManual
							w.mutate(func() error { return w.s.SetMe(withoutEmail(prev, email)) })
							w.toasts.Undo(fmt.Sprintf(t.EmailRemoved, email), func() { w.mutate(func() error { return w.s.SetMe(prev) }) }, "")
						}
					})
				})
			}
			w.addEmail(c, t, info)
		})
		sectionRule(c)
		w.requirementCard(c, t)
	})
}

func fromText(t *words, f session.MeFrom) string {
	if f.Global {
		return t.FromGit
	}
	shown, more := fromRepos(f.Repos)
	if more > 0 {
		return fmt.Sprintf(t.FromReposMore, shown, more)
	}
	return fmt.Sprintf(t.FromRepos, shown)
}
