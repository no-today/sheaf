package settingswin

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/query"
	"sheaf/internal/session"
)

func (w *Window) commandLineTool(c *ui.Context, t *words, tool CLITool) {
	pageSection(c, t.CLIHead, "", func() {
		status, light, label := t.CLINotInstalled, theme.Text4, t.CLIInstall
		if tool.Installed {
			status, light, label = fmt.Sprintf(t.CLIInstalled, tool.Path), theme.BarAdd, t.CLIUninstall
		}
		// The row itself shows the outcome: the light, status and button flip on success, a failure stays under it.
		state := func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(6).Height(6).Radius(3).Background(light).Shrink(0)
				ui.Text(c, status).FontSize(theme.FSLabel).TextColor(theme.Text4).Wrap().MaxLines(2).Tooltip(status)
			})
			ui.Text(c, t.CLIHint).FontSize(theme.FSLabel).TextColor(theme.Text4).Wrap().MaxLines(2)
			if w.cliErr != "" {
				ui.Text(c, w.cliErr).FontSize(theme.FSLabel).TextColor(theme.DangerTxt).Wrap()
			}
		}
		settingRow(c, t.CLICommand, "", state, func() {
			if widgets.Disable(widgets.Button(c, label), w.cliBusy || w.d.SetCLITool == nil).Clicked() {
				w.setCLITool(t, tool)
			}
		})
	})
}

func (w *Window) setCLITool(t *words, before CLITool) {
	w.cliBusy, w.cliErr = true, ""
	go w.d.SetCLITool(!before.Installed, func(err error) {
		w.update(func() {
			w.cliBusy = false
			if err == nil {
				return
			}
			w.cliErr = fmt.Sprintf(t.CLIFailed, err)
			var pathErr *os.PathError
			if errors.Is(err, ErrForeignCLITool) && errors.As(err, &pathErr) {
				w.cliErr = fmt.Sprintf(t.CLIForeign, pathErr.Path)
			}
		})
	})
}

// addEmail is the last row of the my commits card: a link that opens the field, then the field with Add and Cancel.
func (w *Window) addEmail(c *ui.Context, t *words, info session.Info) {
	if !w.adding {
		link := widgets.FlatButton(c, t.AddEmail, func() {
			ui.Text(c, t.AddEmail).FontSize(theme.FSSmall).TextColor(theme.AccentTxt).SingleLine()
		}).AlignSelf(ui.Start)
		if link.Clicked() {
			w.adding = true
		}
		return
	}
	code := query.ValidateEmail(w.draft, info.Me)
	cancel := func() { w.adding, w.touched, w.draft = false, false, "" }
	settingRow(c, t.OtherEmails, t.OtherEmailsHint, nil, func() {
		ui.Column(c).AlignItems(ui.Stretch).Gap(4).Children(func() {
			in := widgets.TextInput(c, &w.draft).Width(240).Font(theme.MonoFont).FontSize(theme.FSSmall).Placeholder(t.EmailPh).Label(t.EmailPh).AutoFocus()
			ui.Row(c).Gap(6).Justify(ui.End).Children(func() {
				add := widgets.Disable(widgets.Button(c, t.Add), strings.TrimSpace(w.draft) == "" || code != "")
				if add.Clicked() || in.Submitted() {
					w.touched = true
					if code == "" {
						emails := withEmail(info.MeManual, w.draft)
						w.mutate(func() error { return w.s.SetMe(emails) })
						cancel()
					}
				}
				if widgets.Button(c, i18n.Of(c).Cancel).Clicked() {
					cancel()
				}
			})
			looksComplete := strings.Contains(w.draft, "@") && strings.Contains(w.draft, ".")
			msg := ""
			switch code {
			case "invalid":
				msg = t.EmailInvalid
			case "duplicate":
				msg = t.EmailDup
			}
			if msg != "" && (w.touched || looksComplete) {
				ui.Text(c, msg).FontSize(theme.FSLabel).TextColor(theme.DangerTxt)
			}
		})
	})
	widgets.Shortcuts(c, widgets.Shortcut{Key: ui.KeyEscape, Do: cancel})
}

// choices are the branches of repo a base can be (no tags; the remote ones first, as they follow every fetch), with the
// current base among them. They come from the in-memory index and are kept until it changes.
func (w *Window) choices(repo, current string) []string {
	w.stampBranches()
	if out, ok := w.branches[repo]; ok && (current == "" || slices.Contains(out, current)) {
		return out
	}
	var remote, local []string
	if bs, err := w.s.Branches(repo); err == nil {
		for _, b := range bs {
			switch b.Kind {
			case "remote":
				remote = append(remote, b.Name)
			case "local":
				local = append(local, b.Name)
			}
		}
	}
	out := slices.Concat(remote, local)
	if current != "" && !slices.Contains(out, current) {
		out = append(out, current)
	}
	w.branches[repo] = out
	return out
}

// stampBranches drops the kept branch choices once the index changed.
func (w *Window) stampBranches() {
	stamp := fmt.Sprint(w.info.State.Updated, w.info.State.Commits)
	if w.branchStamp != stamp {
		w.branchStamp, w.branches, w.names = stamp, map[string][]string{}, nil
	}
}

// branchNames are the branches of every repo, origin/ left off, each once: what a compare branch can be.
func (w *Window) branchNames() []string {
	w.stampBranches()
	if w.names != nil {
		return w.names
	}
	out := []string{}
	for _, r := range w.info.Repos {
		bs, err := w.s.Branches(r.Name)
		if err != nil {
			continue
		}
		for _, b := range bs {
			if b.Kind == "local" || b.Kind == "remote" {
				out = append(out, b.Name, strings.TrimPrefix(b.Name, "origin/"))
			}
		}
	}
	slices.Sort(out)
	w.names = slices.Compact(out)
	return w.names
}

func (w *Window) targets(c *ui.Context, t *words) {
	if from := w.info.Master; w.targetsFrom != from || !w.targetsInit {
		w.master, w.targetsFrom, w.targetsInit = from, from, true
	}
	names := w.branchNames()
	pageSection(c, t.Master, t.TargetsHint, func() {
		settingRow(c, t.CompareBranch, t.MasterHint, nil, func() {
			in := widgets.Caret(ui.Autocomplete(c, &w.master, names)).Width(240).Height(theme.HSelect).PaddingY(4).
				Font(theme.MonoFont).FontSize(theme.FSSmall).Placeholder(t.MasterPh).Label(t.CompareBranchField)
			focused := in.Focused()
			// A suggestion taken (Enter or a click) is a choice made, as a branch typed out in full is.
			taken := in.Changed() && slices.Contains(names, strings.TrimSpace(w.master))
			if (w.masterFocus && !focused) || in.Submitted() || taken {
				w.commitTargets()
			}
			w.masterFocus = focused
		})
	})
}

func (w *Window) commitTargets() {
	master := strings.TrimSpace(w.master)
	if master == w.info.Master {
		return
	}
	w.mutate(func() error { return w.s.SetCompareBase(master) })
}

func (w *Window) base(c *ui.Context, t *words) {
	pageTitle(c, t.TabBase)
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		w.mergeFlow(c, t)
		sectionRule(c)
		w.targets(c, t)
		sectionRule(c)
		w.repoBases(c, t)
	})
}

func (w *Window) repoBases(c *ui.Context, t *words) {
	info := w.info
	pageSection(c, t.BasesHead, fmt.Sprintf(t.BaseFoot, len(info.Repos)), func() {
		if len(info.Repos) == 0 {
			ui.Text(c, t.NoRepos).TextColor(theme.Text4)
			return
		}
		ui.Column(c).AlignItems(ui.Stretch).Gap(12).Children(func() {
			for _, r := range info.Repos {
				p := w.baseSel[r.Name]
				if p == nil {
					v := r.Base
					p = &v
					w.baseSel[r.Name] = p
					w.baseSeen[r.Name] = r.Base
				}
				if w.baseSeen[r.Name] != r.Base { // the index or someone else changed it
					*p, w.baseSeen[r.Name] = r.Base, r.Base
				}
				hint := t.BaseMainDir
				if n := countExtra(r); n > 0 {
					hint = fmt.Sprintf(t.BaseDirs, n)
				}
				if r.BaseAuto {
					hint += " · " + t.BaseAuto
				}
				settingRow(c, r.Name, hint, func() {
					only := r.BaselineOnly
					if ui.Checkbox(c.Key("baseline "+r.Name), &only, t.BaselineOnly).Label(fmt.Sprintf(t.BaselineOnlyRepo, r.Name)).Changed() {
						repo := r.Name
						w.mutate(func() error { return w.s.SetRepoBaselineOnly(repo, only) })
					}
				}, func() {
					before := *p
					ui.Combobox(c, p, w.choices(r.Name, r.Base)).Width(240).Height(theme.HSelect).Font(theme.MonoFont).FontSize(theme.FSSmall).Placeholder(t.BaseHint).Label(t.ColBase + " · " + r.Name)
					if *p != before && *p != "" {
						repo, branch := r.Name, *p
						w.mutate(func() error { return w.s.SetBase(repo, branch) })
					}
				})
			}
		})
	})
}

// repoRow is the height of a row of the repos list; the list shows at most repoRows of them and scrolls beyond.
const (
	repoRow  = 40
	repoRows = 8
)

// repos lists every repo the scan found, tracked or not, each with a switch. An untracked repo is left out of the
// index and so out of the requirements.
func (w *Window) repos(c *ui.Context, t *words) {
	info := w.info
	names := info.Scanned
	pageTitle(c, t.TabRepos)
	off := 0
	for _, n := range names {
		if slices.Contains(info.Ignored, n) {
			off++
		}
	}
	pageSection(c, t.ReposHead, fmt.Sprintf(t.ReposHint, len(names), off), func() {
		if len(names) == 0 {
			ui.Text(c, t.NoRepos).TextColor(theme.Text4)
			return
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if widgets.TextInput(c, &w.repoSearch).Grow(1).MinWidth(0).Placeholder(t.RepoSearch).Label(t.RepoSearch).Changed() {
				w.repoList.ScrollTo(0, ui.Start)
			}
			ui.Text(c, fmt.Sprintf(t.RepoCount, len(names)-off, len(names))).FontSize(theme.FSLabel).TextColor(theme.Text4).SingleLine()
		})
		search := strings.ToLower(strings.TrimSpace(w.repoSearch))
		shown := make([]string, 0, len(names))
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), search) {
				shown = append(shown, name)
			}
		}
		if len(shown) == 0 {
			ui.Text(c, t.RepoNoMatch).FontSize(theme.FSSmall).TextColor(theme.Text4).PaddingY(20)
			return
		}
		ui.List(c.Key("repos"), &w.repoList, len(shown), func(i int) {
			name := shown[i]
			on := !slices.Contains(info.Ignored, name)
			ui.Row(c.Key(name)).Height(repoRow).Gap(12).PaddingX(10).AlignItems(ui.Center).Border(1, theme.RowRule).BorderWidth(0, 0, 1, 0).Children(func() {
				ui.Text(c, name).Font(theme.MonoFont).FontSize(theme.FSSmall).FontWeight(600).TextColor(theme.TextStrong).SingleLine().Grow(1).Shrink(1).MinWidth(0).Tooltip(name)
				if n := len(info.IgnoredWith[name]); n > 0 {
					ui.Text(c, fmt.Sprintf(t.RepoInner, n)).FontSize(theme.FSLabel).TextColor(theme.Text4).SingleLine().Shrink(0)
				}
				state := t.RepoOff
				if on {
					state = t.RepoOn
				}
				ui.Text(c, state).FontSize(theme.FSLabel).TextColor(theme.Text4).SingleLine()
				sw := widgets.Switch(c.Key("repo "+name), &on).Label(fmt.Sprintf(t.RepoTrack, name))
				if sw.Changed() {
					next := slices.DeleteFunc(slices.Clone(info.Ignored), func(x string) bool { return x == name })
					if !on {
						next = append(next, name)
					}
					w.mutate(func() error { return w.s.SetIgnored(next) })
				}
			})
		}).Height(float32(min(len(shown), repoRows) * repoRow))
	})
}

// countExtra is how many working directories a repo has besides its main one.
func countExtra(r session.RepoInfo) int {
	n := 0
	for _, wt := range r.Worktrees {
		if !wt.Main {
			n++
		}
	}
	return n
}

// indexLine is the index state in words and the colour of its dot.
func (w *Window) indexLine(t *words) (string, bool, bool) {
	st := w.info.State
	n := i18n.Thousands(st.Commits)
	switch {
	case st.Error != "":
		return fmt.Sprintf(t.IndexError, st.Error), false, true
	case len(st.Failed) > 0:
		repos := make([]string, len(st.Failed))
		for i, f := range st.Failed {
			repos[i] = f.Repo
		}
		return fmt.Sprintf(t.IndexFailed, i18n.Plural(t.IndexFailedRepos, len(st.Failed)), strings.Join(repos, ", ")), false, true
	case st.Phase == "ready" && st.Updated > 0:
		return fmt.Sprintf(t.IndexLine, n, w.ago(t, st.Updated)), false, false
	case st.Phase == "ready":
		return fmt.Sprintf(t.IndexNoTime, n), false, false
	case st.Phase == "updating":
		return fmt.Sprintf(t.IndexUpdating, n), true, false
	case st.Phase == "indexing":
		return fmt.Sprintf(t.IndexProgress, st.Done, st.Total), true, false
	}
	return t.Loading, true, false
}

func (w *Window) ago(t *words, updated int64) string {
	unit, n := agoKey(updated, time.Now().Unix())
	switch unit {
	case "min":
		return fmt.Sprintf(t.AgoMin, n)
	case "hour":
		return fmt.Sprintf(t.AgoHour, n)
	case "day":
		return fmt.Sprintf(t.AgoDay, n)
	}
	return t.AgoNow
}

func (w *Window) index(c *ui.Context, t *words) {
	info := w.info
	busy := info.State.Indexing || w.refreshing
	text, working, failed := w.indexLine(t)
	dot := theme.Add
	switch {
	case failed:
		dot = theme.Del
	case working:
		dot = theme.AccentTxt
	}
	extra := 0
	for _, r := range info.Repos {
		extra += countExtra(r)
	}
	pageTitle(c, t.TabIndex)
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		pageSection(c, t.IndexHead, fmt.Sprintf(t.IndexSub, len(info.Repos), extra), func() {
			settingRow(c, text, "", nil, func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(dot).Shrink(0)
					label := t.Refresh
					switch {
					case busy:
						label = t.Refreshing
					case w.done:
						label = t.Refreshed
					}
					b := widgets.Button(c, label)
					if w.done && !busy {
						b.Children(func() { ui.Icon(c, theme.IconCheck).FontSize(theme.IconS).TextColor(theme.Text3) })
					}
					if widgets.Disable(b, busy).Clicked() {
						w.refresh()
					}
				})
			})
		})
		sectionRule(c)
		pageSection(c, t.LocalData, t.IndexHint, func() {
			settingRow(c, t.DataDir, "", nil, func() {
				if widgets.Button(c, t.Reveal).Clicked() && w.d.RevealData != nil {
					w.d.RevealData()
				}
			})
			settingRow(c, t.CmdLog, t.CmdLogSub, nil, func() {
				if widgets.Button(c, t.CmdLogOpen).Clicked() && w.d.RevealCommandLog != nil {
					w.d.RevealCommandLog()
				}
			})
		})
	})
}

func (w *Window) refresh() {
	w.refreshing, w.done = true, false
	go func() {
		err := w.s.Refresh(w.ctx)
		w.update(func() {
			w.refreshing, w.done = false, err == nil
			if err != nil {
				w.toasts.Error(err.Error())
			}
		})
		time.Sleep(2500 * time.Millisecond)
		w.update(func() { w.done = false })
	}()
}

func (w *Window) glabPage(c *ui.Context, t *words) {
	if w.glabFrom != w.info.GlabConfig || !w.glabInit {
		w.glab, w.glabFrom, w.glabInit = w.info.GlabConfig, w.info.GlabConfig, true
	}
	if w.glabChecked == nil || *w.glabChecked != w.glab {
		dir := w.glab
		w.glabProblem, w.glabChecked = w.s.CheckGlabConfig(dir), &dir
	}
	line := glabLoginLine(w.glab, "")
	pageTitle(c, t.TabGlab)
	ui.Column(c).AlignItems(ui.Stretch).Children(func() {
		pageSection(c, t.GlabHead, t.GlabHint, func() {
			settingRow(c, t.GlabDir, t.GlabDirHint, func() {
				if w.glabProblem != "" {
					ui.Text(c, t.GlabMissing).FontSize(theme.FSLabel).TextColor(theme.DangerTxt).Wrap()
				}
			}, func() {
				in := widgets.TextInput(c, &w.glab).Width(240).Font(theme.MonoFont).FontSize(theme.FSSmall).Placeholder(t.GlabPh).Label(t.GlabDir)
				focused := in.Focused()
				if (w.glabFocus && !focused) || in.Submitted() {
					w.commitGlab()
				}
				w.glabFocus = focused
			})
			settingRow(c, t.GlabLogin, "", nil, func() {
				ui.Text(c, line).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text3).Selectable().Wrap()
				if widgets.Button(c, t.Copy).Clicked() {
					c.WriteClipboard(line)
					w.toasts.Show(t.GlabCopied)
				}
			})
		})
		sectionRule(c)
		pageSection(c, t.GhHead, t.GhHint, func() {
			settingRow(c, t.GhLogin, "", nil, func() {
				ui.Text(c, t.GhCommand).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text3).Selectable().Wrap()
				if widgets.Button(c.Key("gh-copy"), t.Copy).Label(t.GhCopy).Clicked() {
					c.WriteClipboard(t.GhCommand)
					w.toasts.Show(t.GlabCopied)
				}
			})
		})
	})
}

func (w *Window) commitGlab() {
	dir := strings.TrimSpace(w.glab)
	if dir == w.info.GlabConfig {
		return
	}
	w.mutate(func() error { return w.s.SetGlabConfig(dir) })
}

// requirementCard is the pattern that tells a requirement number in branch names and commit titles; one that is not a
// regular expression stays in the field with the reason and is not saved.
func (w *Window) requirementCard(c *ui.Context, t *words) {
	if w.reqFrom != w.info.Requirement || !w.reqInit {
		w.req, w.reqFrom, w.reqInit = w.info.Requirement, w.info.Requirement, true
	}
	if w.reqChecked == nil || *w.reqChecked != w.req {
		pattern := w.req
		w.reqProblem, w.reqChecked = w.s.CheckRequirement(pattern), &pattern
	}
	pageSection(c, t.ReqHead, t.ReqHint, func() {
		settingRow(c, t.ReqPattern, t.ReqPatternHint, func() {
			if w.reqProblem != "" {
				ui.Text(c, t.ReqInvalid).FontSize(theme.FSLabel).TextColor(theme.DangerTxt).Wrap()
			}
		}, func() {
			in := widgets.TextInput(c, &w.req).Width(240).Font(theme.MonoFont).FontSize(theme.FSSmall).Placeholder(session.DefaultRequirement).Label(t.ReqPattern)
			focused := in.Focused()
			if (w.reqFocus && !focused) || in.Submitted() {
				w.commitRequirement()
			}
			w.reqFocus = focused
		})
	})
}

func (w *Window) commitRequirement() {
	pattern := strings.TrimSpace(w.req)
	if pattern == "" {
		pattern = session.DefaultRequirement
	}
	if pattern == w.info.Requirement || w.reqProblem != "" {
		return
	}
	w.mutate(func() error { return w.s.SetRequirement(pattern) })
}
