package session

import (
	"context"
	"time"

	"sheaf/internal/gitview"
)

// pulseEvery bounds how often OnCommands is called: an index build runs hundreds of commands a second.
const pulseEvery = 250 * time.Millisecond

// git records the commands run with the returned context in the command log file (writes) and memory (reads), as asked for by
// source (a short phrase naming the operation). The source of an outer call wins: a refresh started by the watch is the watch's.
func (s *Session) git(ctx context.Context, source string) context.Context {
	return gitview.Source(gitview.Limit(gitview.Record(ctx, &s.rec), s.opts.GitLimit), source)
}

// sourceText is the Recorder's Describe: a source key in words for the command log file, in Chinese (the file is
// read by the user; Go has no i18n below the native UI). The phrases of the command log page removed on 2026-11-03.
func sourceText(source string) string {
	switch source {
	case "open":
		return "打开工作区"
	case "refresh":
		return "索引 · 刷新"
	case "watch":
		return "文件监听"
	case "dirtyCount":
		return "未提交计数"
	case "releaseCheck":
		return "release 合入检查"
	case "remerge":
		return "合并提交 · 冲突解决检查"
	case "commit":
		return "提交详情"
	case "files":
		return "提交的文件"
	case "commitTexts":
		return "差异 · 全文"
	case "uncommitted":
		return "未提交 · 文件"
	case "discard":
		return "未提交 · 丢弃改动"
	case "uncommittedPatch":
		return "未提交 · 差异"
	case "uncommittedTexts":
		return "未提交 · 全文"
	case "aggFiles":
		return "筛选器页 · 文件"
	case "aggPrefetch":
		return "需求页 · 预读文件与行数"
	case "aggAvail":
		return "筛选器页 · 各查看方式的文件数"
	case "aggPatch":
		return "筛选器页 · 差异"
	case "aggStats":
		return "筛选器页 · 改动行数"
	case "dirtyView":
		return "筛选器页 · 未提交"
	case "mrPlan":
		return "筛选器页 · 创建 MR/PR：确认"
	case "mrCreate":
		return "筛选器页 · 创建 MR/PR"
	case "batchFetch":
		return "需求页 · 获取全部"
	case "branchesPlan":
		return "需求页 · 同步收藏分支：确认"
	case "branchesRun":
		return "需求页 · 同步收藏分支"
	}
	return ""
}

// cmdChanged is the Recorder's OnChange: at most one OnCommands call per pulseEvery.
func (s *Session) cmdChanged() {
	if s.opts.OnCommands == nil {
		return
	}
	s.pulseMu.Lock()
	defer s.pulseMu.Unlock()
	if s.pulsing {
		return
	}
	s.pulsing = true
	time.AfterFunc(pulseEvery, func() {
		s.pulseMu.Lock()
		s.pulsing = false
		s.pulseMu.Unlock()
		s.opts.OnCommands(CmdPulse{Running: s.rec.Running(), Last: int(s.rec.LastID())})
	})
}

// CommandsRunning is how many git commands run now.
func (s *Session) CommandsRunning() int { return s.rec.Running() }

// CommandLogPath is the file the writes (git, glab and gh) are logged to, one JSON line each.
func (s *Session) CommandLogPath() string { return s.rec.WriteLog }
