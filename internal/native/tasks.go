package native

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/i18n"
	"sheaf/internal/native/theme"
	"sheaf/internal/native/widgets"
	"sheaf/internal/session"
)

// taskDelay keeps a task out of the corner until it has lasted this long, so quick ones (a discard, a small index
// update) do not flicker there.
const taskDelay = 400 * time.Millisecond

// taskPanelW is the width of the task list.
const taskPanelW = 420

type taskWords struct {
	Tasks, NoTasks, Clear, Stop, Stopping, More                    string
	Index, IndexFirst, Fetch, Sync, MR, Discard                    string
	Done, Failed, Stopped, Waiting, Running, Skipped, FailedRows   string
	Fetched, NoRemote, Updated, UpToDate, Created, Exists          string
	SkipDirty, SkipMerging, SkipNoLocal, SkipUnpushed, SkipStopped string
	SkipNoBranch, SkipNoTarget, SkipIsTarget, SkipNoProject        string
	SkipDiverged                                                   string
	Now, Ago, Took                                                 string
}

var taskText = i18n.New(taskWords{
	Tasks: "任务", NoTasks: "没有进行中的任务", Clear: "清除已完成", Stop: "停止", Stopping: "正在停止…", More: "另有 {n} 个",
	Index: "更新数据", IndexFirst: "读取提交", Fetch: "刷新远端", Sync: "更新分支", MR: "创建 MR/PR", Discard: "还原改动",
	Done: "完成", Failed: "失败", FailedRows: "{n} 个失败", Stopped: "已停止", Waiting: "等待", Running: "进行中", Skipped: "跳过",
	Fetched: "已获取", NoRemote: "没有远端", Updated: "已更新", UpToDate: "已是最新", Created: "已创建", Exists: "已存在",
	SkipDirty: "有未提交改动", SkipMerging: "操作没做完", SkipNoLocal: "没有本地分支", SkipUnpushed: "有未推送的提交",
	SkipStopped: "已停止", SkipNoBranch: "没有需求分支", SkipNoTarget: "没有目标分支", SkipIsTarget: "就是目标分支",
	SkipNoProject: "找不到项目", SkipDiverged: "和远端对不上",
	Now: "刚刚", Ago: "{t} 前", Took: "用时 {t}",
}, taskWords{
	Tasks: "Tasks", NoTasks: "No tasks running", Clear: "Clear finished", Stop: "Stop", Stopping: "Stopping…", More: "{n} more",
	Index: "Updating data", IndexFirst: "Reading commits", Fetch: "Refresh remotes", Sync: "Update branches", MR: "Create MR/PR", Discard: "Discard changes",
	Done: "Done", Failed: "Failed", FailedRows: "{n} failed", Stopped: "Stopped", Waiting: "Waiting", Running: "Running", Skipped: "Skipped",
	Fetched: "Fetched", NoRemote: "No remote", Updated: "Updated", UpToDate: "Up to date", Created: "Created", Exists: "Exists",
	SkipDirty: "Uncommitted changes", SkipMerging: "Unfinished operation", SkipNoLocal: "No local branch", SkipUnpushed: "Unpushed commits",
	SkipStopped: "Stopped", SkipNoBranch: "No requirement branch", SkipNoTarget: "No target branch", SkipIsTarget: "Is the target",
	SkipNoProject: "Project not found", SkipDiverged: "Out of step with remote",
	Now: "just now", Ago: "{t} ago", Took: "took {t}",
})

// taskStrip is the corner of the status bar: the task running (with the count of the others) and its progress, or,
// when none runs, a quiet button to the finished ones. Either opens the task list above it.
type taskStrip struct {
	open bool
	seen int // the newest finished task the user has looked at; a newer failure marks the button
}

func (ts *taskStrip) View(c *ui.Context, s *session.Session, all []session.Task, indexing bool) {
	w := taskText.Of(c)
	var running []session.Task
	failed := false
	newest := 0
	for _, t := range all {
		switch {
		case t.State == session.TaskRunning && c.Now().Sub(t.Started) >= taskDelay:
			running = append(running, t)
		case t.State == session.TaskRunning:
			c.After(taskDelay - c.Now().Sub(t.Started))
		default:
			newest = max(newest, t.ID)
			failed = failed || t.State == session.TaskFailed && t.ID > ts.seen
		}
	}
	if len(running) == 0 && newest == 0 && !ts.open {
		return
	}
	b := ui.ButtonBase(c.Key("tasks")).Height(theme.HStatusBar - 4).PaddingX(8).Gap(8).AlignItems(ui.Center).Shrink(1).MinWidth(0).
		Cursor(ui.CursorPointer).Label(w.Tasks)
	if b.Hovered() || ts.open {
		b.Background(theme.Hover)
	}
	b.Children(func() {
		if len(running) == 0 {
			ui.Icon(c, theme.IconHistory).FontSize(theme.IconS).TextColor(theme.Text4)
			if failed {
				ui.Box(c).Size(6, 6).Radius(3).Background(theme.Del)
			}
			return
		}
		t := running[0]
		ui.Spinner(c).Size(theme.IconS, theme.IconS).TextColor(theme.Text3)
		ui.Text(c, taskTitle(w, t, indexing)).TextColor(theme.Text).SingleLine().Shrink(1).MinWidth(0)
		if t.Total > 0 {
			bar(c, 72, float32(t.Done)/float32(t.Total))
			ui.Text(c, fmt.Sprintf("%d/%d", t.Done, t.Total)).FontFeatures("tnum").TextColor(theme.Text4).SingleLine().Shrink(0)
		}
		if n := len(running) - 1; n > 0 {
			ui.Text(c, fill(w.More, n)).TextColor(theme.Text4).SingleLine().Shrink(0)
		}
	})
	if b.Clicked() {
		ts.open = !ts.open
	}
	ui.PopoverBase(c, b, &ts.open, func(panel ui.Element) {
		ts.seen = max(ts.seen, newest)
		widgets.Panel(panel).AttachTo(b, ui.AnchorTopEnd, ui.AnchorBottomEnd).Margin(0, 0, 6, 0).Width(taskPanelW).MaxHeight(480).
			Padding(0).Label(w.Tasks).Children(func() { ts.panel(c, w, s, all, indexing) })
	})
}

func (ts *taskStrip) panel(c *ui.Context, w *taskWords, s *session.Session, all []session.Task, indexing bool) {
	ended := false
	ui.Row(c).Height(36).PaddingX(14).AlignItems(ui.Center).Border(1, theme.Line).BorderWidth(0, 0, 1, 0).Children(func() {
		ui.Text(c, w.Tasks).FontWeight(600).TextColor(theme.TextStrong)
		ui.Spacer(c)
		for _, t := range all {
			ended = ended || t.State != session.TaskRunning
		}
		if ended && widgets.Button(c.Key("clear"), w.Clear).Height(22).FontSize(theme.FSSmall).Border(0, ui.Transparent).Clicked() {
			s.ClearTasks()
		}
	})
	if len(all) == 0 {
		ui.Text(c, w.NoTasks).TextColor(theme.Text4).Padding(16, 14)
		return
	}
	ui.Scroll(c.Key("task list")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).AlignItems(ui.Stretch).PaddingY(6).Children(func() {
			for i, t := range all {
				if i > 0 {
					ui.Box(c).Height(1).Background(theme.Line).Margin(6, 14, 6, 14)
				}
				taskItem(c.Key(t.ID), w, s, t, indexing)
			}
		})
	})
}

// taskItem is one task: its title and state, its progress while it runs, and its rows.
func taskItem(c *ui.Context, w *taskWords, s *session.Session, t session.Task, indexing bool) {
	ui.Column(c).AlignItems(ui.Stretch).PaddingX(14).Gap(4).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(26).Children(func() {
			ui.Text(c, taskTitle(w, t, indexing)).FontWeight(600).TextColor(theme.TextStrong).SingleLine().Shrink(1).MinWidth(0)
			ui.Spacer(c)
			label, color := taskState(w, t)
			if t.State == session.TaskRunning {
				if t.Total > 0 {
					ui.Text(c, fmt.Sprintf("%d/%d", t.Done, t.Total)).FontSize(theme.FSSmall).FontFeatures("tnum").TextColor(theme.Text4).SingleLine()
				}
				switch {
				case t.CanStop:
					if widgets.Button(c.Key("stop"), w.Stop).Height(22).FontSize(theme.FSSmall).Clicked() {
						id := t.ID
						go s.StopTask(id)
					}
				case t.Kind != session.TaskIndex && t.Kind != session.TaskDiscard:
					ui.Text(c, w.Stopping).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine()
				}
				return
			}
			ui.Text(c, when(c, w, t)).FontSize(theme.FSSmall).TextColor(theme.Text4).SingleLine().Shrink(0)
			ui.Text(c, label).FontSize(theme.FSSmall).FontWeight(600).TextColor(color).SingleLine().Shrink(0)
		})
		if t.State == session.TaskRunning && t.Total > 0 {
			bar(c, 0, float32(t.Done)/float32(t.Total)).FillWidth()
		}
		if t.Error != "" {
			ui.Text(c, t.Error).FontSize(theme.FSSmall).TextColor(theme.DangerTxt).Selectable().Wrap()
		}
		for i, r := range t.Rows {
			if r.URL != "" {
				linkRow(c.Key(i), w, r)
				continue
			}
			ui.Row(c).Height(22).Gap(8).AlignItems(ui.Center).Children(func() {
				rowMark(c, r.State)
				ui.Text(c, r.Name).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text).SingleLine().Shrink(1).MinWidth(0).Tooltip(r.Name)
				ui.Spacer(c)
				note, color := rowNote(w, r)
				ui.Text(c, note).FontSize(theme.FSSmall).TextColor(color).SingleLine().Shrink(2).MinWidth(0).Tooltip(r.Note)
			})
		}
	})
}

// linkRow is a row that created or found a merge or pull request: the whole row opens it in the browser.
func linkRow(c *ui.Context, w *taskWords, r session.TaskRow) {
	note, _ := rowNote(w, r)
	if ref := session.MRRef(r.URL); ref != "" {
		note += " " + ref
	}
	b := ui.ButtonBase(c.Key("link")).Height(22).Gap(8).AlignItems(ui.Center).Margin(0, -6, 0, -6).PaddingX(6).
		Cursor(ui.CursorPointer).Label(r.Name + " " + note).Tooltip(r.URL)
	hot := b.Hovered()
	if hot {
		b.Background(theme.Hover)
	}
	b.Children(func() {
		rowMark(c, r.State)
		ui.Text(c, r.Name).Font(theme.MonoFont).FontSize(theme.FSSmall).TextColor(theme.Text).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c)
		color := theme.Text
		if hot {
			color = theme.TextStrong
		}
		ui.Text(c, note).FontSize(theme.FSSmall).TextColor(color).SingleLine().Underline().Shrink(2).MinWidth(0)
		ui.Icon(c, theme.IconLink).FontSize(theme.IconS).TextColor(color).Shrink(0)
	})
	if b.Clicked() {
		c.OpenURL(r.URL)
	}
}

// rowMark is a row's state at a glance: a dot that fills as it runs and ends coloured by the outcome.
func rowMark(c *ui.Context, state string) {
	box := ui.Box(c).Size(7, 7).Radius(4).Shrink(0)
	switch state {
	case session.TaskRunning:
		box.Background(theme.Text)
	case session.TaskDone:
		box.Background(theme.Add)
	case session.TaskFailed:
		box.Background(theme.Del)
	default: // waiting, skipped
		box.Border(1, theme.Text4)
	}
}

// bar is a thin progress bar; width 0 leaves the width to the caller.
func bar(c *ui.Context, width, part float32) ui.Element {
	track := ui.Row(c).Height(3).Background(theme.BarTrack).Shrink(0)
	if width > 0 {
		track.Width(width)
	}
	return track.Children(func() {
		ui.Box(c).FillHeight().Grow(max(0, min(part, 1))).Background(theme.Text)
		ui.Box(c).FillHeight().Grow(max(0, 1-min(part, 1)))
	})
}

func taskTitle(w *taskWords, t session.Task, indexing bool) string {
	title := map[string]string{session.TaskIndex: w.Index, session.TaskFetch: w.Fetch, session.TaskSync: w.Sync,
		session.TaskMR: w.MR, session.TaskDiscard: w.Discard}[t.Kind]
	if t.Kind == session.TaskIndex && indexing {
		title = w.IndexFirst
	}
	if t.Subject != "" {
		title += " · " + t.Subject
	}
	return title
}

// taskState is a task's outcome; a task done with failed rows says how many.
func taskState(w *taskWords, t session.Task) (string, ui.Color) {
	failed := 0
	for _, r := range t.Rows {
		if r.State == session.TaskFailed {
			failed++
		}
	}
	if t.State == session.TaskDone && failed > 0 {
		return fill(w.FailedRows, failed), theme.Del
	}
	switch t.State {
	case session.TaskFailed:
		return w.Failed, theme.Del
	case session.TaskStopped:
		return w.Stopped, theme.Text4
	case session.TaskRunning:
		return w.Running, theme.Text
	}
	return w.Done, theme.Add
}

// rowNote turns a row's outcome code into words; anything else is an error message, shown as it is.
func rowNote(w *taskWords, r session.TaskRow) (string, ui.Color) {
	words := map[string]string{
		"fetched": w.Fetched, "no_remote": w.NoRemote, "updated": w.Updated, "up_to_date": w.UpToDate,
		"created": w.Created, "exists": w.Exists, "skipped": w.Skipped,
		"dirty": w.SkipDirty, "merging": w.SkipMerging, "no_local": w.SkipNoLocal, "unpushed": w.SkipUnpushed,
		"stopped": w.SkipStopped, "no_branch": w.SkipNoBranch, "no_target": w.SkipNoTarget, "is_target": w.SkipIsTarget,
		"no_project": w.SkipNoProject, "diverged": w.SkipDiverged,
	}
	switch r.State {
	case session.TaskWaiting:
		return w.Waiting, theme.Text4
	case session.TaskRunning:
		return w.Running, theme.Text
	case session.TaskFailed:
		if s, ok := words[r.Note]; ok {
			return s, theme.DangerTxt
		}
		return r.Note, theme.DangerTxt
	}
	if s, ok := words[r.Note]; ok {
		return s, theme.Text4
	}
	return r.Note, theme.Text4
}

// when is how long ago a finished task ended, and how long it took.
func when(c *ui.Context, w *taskWords, t session.Task) string {
	ago, at := c.Now().Sub(t.Ended), w.Now
	if ago < time.Minute {
		c.After(time.Minute - ago)
	} else {
		at = fill(w.Ago, short(ago))
	}
	return at + " · " + fill(w.Took, short(t.Ended.Sub(t.Started)))
}

func short(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func fill(text string, v any) string {
	return strings.NewReplacer("{n}", fmt.Sprint(v), "{t}", fmt.Sprint(v)).Replace(text)
}
