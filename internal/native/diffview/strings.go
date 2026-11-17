package diffview

import (
	"fmt"
	"strings"

	"sheaf/internal/native/i18n"
)

// words is what Preview Diff says.
type words struct {
	Split, Unified, Layout string
	PrevFile, NextFile     string
	PrevChange, NextChange string
	PickFile, PreviewHint  string
	Loading, Binary        string
	RefreshToLoad          string
	Unchanged, Cut         string
	FileOf                 string // %d / %d
	Changes                func(n int) string
	Change                 func(i, n int) string
	Failed                 func(err error) string
	More                   func(n int) string // commits that changed the file and are not shown
	BinaryParts            func(titles []string) string
	Folded                 func(n int) string
	StreamFolded           func(n int) string // a large file of the stream, folded until clicked
	StreamDiff             func(path string) string
	ExpandUp, ExpandDown   string
	ExpandAll              string
	// The file tree.
	Tree, Bin                         string
	Modified, Added, Deleted, Renamed string
	NFiles                            func(n int) string
}

var tr = i18n.New(words{
	Split: "并排", Unified: "单列", Layout: "布局",
	PrevFile: "上一个文件 (←)", NextFile: "下一个文件 (→)",
	PrevChange: "上一处改动", NextChange: "下一处改动",
	PickFile: "选一个文件", PreviewHint: "这里显示它的改动",
	Loading: "正在读取…", Binary: "无法预览此文件",
	RefreshToLoad: "内容已变化，刷新后读取此文件",
	Unchanged:     "内容没变", Cut: "文件过大，只显示开头 2 MB",
	FileOf:  "%d / %d",
	Changes: func(n int) string { return fmt.Sprintf("%d 处改动", n) },
	Change:  func(i, n int) string { return fmt.Sprintf("第 %d / %d 处改动", i, n) },
	Failed:  func(err error) string { return "读取失败：" + err.Error() },
	More:    func(n int) string { return fmt.Sprintf("另有 %d 个提交改过它，未列出", n) },
	BinaryParts: func(titles []string) string {
		return "无法预览：" + strings.Join(titles, "、")
	},
	Folded:       func(n int) string { return fmt.Sprintf("%d 行未修改", n) },
	StreamFolded: func(n int) string { return fmt.Sprintf("%d 行已折叠，点击展开", n) },
	StreamDiff:   func(path string) string { return path + " 的改动" },
	ExpandUp:     "向上展开", ExpandDown: "向下展开", ExpandAll: "全部展开",
	Tree: "改动的文件", Bin: "bin",
	Modified: "修改", Added: "新增", Deleted: "删除", Renamed: "重命名",
	NFiles: func(n int) string { return fmt.Sprintf("%d 个文件", n) },
}, words{
	Split: "Split", Unified: "Unified", Layout: "Layout",
	PrevFile: "Previous file (←)", NextFile: "Next file (→)",
	PrevChange: "Previous change", NextChange: "Next change",
	PickFile: "Select a file", PreviewHint: "Its changes appear here",
	Loading: "Loading…", Binary: "Can't preview this file",
	RefreshToLoad: "Changes available. Refresh to load this file.",
	Unchanged:     "Content unchanged", Cut: "File too large: only the first 2 MB are shown",
	FileOf: "%d / %d",
	Changes: func(n int) string {
		if n == 1 {
			return "1 change"
		}
		return fmt.Sprintf("%d changes", n)
	},
	Change: func(i, n int) string { return fmt.Sprintf("change %d of %d", i, n) },
	Failed: func(err error) string { return "Could not read the file: " + err.Error() },
	More: func(n int) string {
		if n == 1 {
			return "1 more commit changed it, not listed"
		}
		return fmt.Sprintf("%d more commits changed it, not listed", n)
	},
	BinaryParts: func(titles []string) string { return "Can't preview: " + strings.Join(titles, ", ") },
	Folded: func(n int) string {
		if n == 1 {
			return "1 unmodified line"
		}
		return fmt.Sprintf("%d unmodified lines", n)
	},
	StreamFolded: func(n int) string { return fmt.Sprintf("%d lines folded, click to expand", n) },
	StreamDiff:   func(path string) string { return "Changes in " + path },
	ExpandUp:     "Expand up", ExpandDown: "Expand down", ExpandAll: "Expand all",
	Tree: "Changed files", Bin: "bin",
	Modified: "Modified", Added: "Added", Deleted: "Deleted", Renamed: "Renamed",
	NFiles: func(n int) string {
		if n == 1 {
			return "1 file"
		}
		return fmt.Sprintf("%d files", n)
	},
})
