// Package i18n is how the native UI says things: in Chinese when the app shows Chinese, else in English.
// The language is the app's (ui.Context.Locale, which follows the system among the languages app.Run declares).
//
// Each package keeps its own words: a struct type of its own and one Table of it, made with New from a Chinese and an
// English literal, so pages written side by side never edit the same file. Words shared by several packages (the frame,
// the widgets, window titles) are Common's. archtest TestNativeWords checks every table (both literals list every field,
// none empty, the same fmt verbs), and TestNativeHygiene that views pass no literal text to the ui.
package i18n

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Table is one package's words in both languages.
type Table[T any] struct{ zh, en *T }

// New makes a table. Pass composite literals of the package's words type: archtest reads them.
func New[T any](zh, en T) *Table[T] { return &Table[T]{zh: &zh, en: &en} }

// For is the words for locale: Chinese for any zh tag, else English.
func (t *Table[T]) For(locale string) *T {
	if Zh(locale) {
		return t.zh
	}
	return t.en
}

// Of is the words in the language the window shows.
func (t *Table[T]) Of(c *ui.Context) *T { return t.For(c.Locale()) }

// Zh tells whether locale is Chinese.
func Zh(locale string) bool { return strings.HasPrefix(strings.ToLower(locale), "zh") }

// Words are the words shared by the frame, the widgets and the windows' titles.
type Words struct {
	Opening   string
	Hide      string // a dock pane's hide button
	ShowLeft  string // the title bar's button of the left pane
	HideLeft  string
	Divider   string // the handle between two islands, for assistive technology
	Undo      string // the toast's undo button
	Close     string
	Cancel    string
	OK        string
	Settings  string
	Details   string // a toast's button to the task list
	CopyError string
	Log       string
	Track     string // the requirement page
	Diff      string // windows
	History   string
	Welcome   string
	Scope     string
	AllRepos  string
	Yesterday string
}

// Common is the shared words.
var Common = New(Words{
	Opening:   "正在打开…",
	Hide:      "隐藏",
	ShowLeft:  "显示左栏",
	HideLeft:  "隐藏左栏",
	Divider:   "分隔条",
	Undo:      "撤销",
	Close:     "关闭",
	Cancel:    "取消",
	OK:        "确定",
	Settings:  "设置",
	Details:   "详情",
	CopyError: "复制错误信息",
	Log:       "提交记录",
	Track:     "需求",
	Diff:      "改动",
	History:   "文件历史",
	Welcome:   "欢迎使用 Sheaf",
	Scope:     "仓库范围",
	AllRepos:  "全部仓库",
	Yesterday: "昨天",
}, Words{
	Opening:   "Opening…",
	Hide:      "Hide",
	ShowLeft:  "Show left pane",
	HideLeft:  "Hide left pane",
	Divider:   "Divider",
	Undo:      "Undo",
	Close:     "Close",
	Cancel:    "Cancel",
	OK:        "OK",
	Settings:  "Settings",
	Details:   "Details",
	CopyError: "Copy error",
	Log:       "Commits",
	Track:     "Requirements",
	Diff:      "Changes",
	History:   "File History",
	Welcome:   "Welcome to Sheaf",
	Scope:     "Repository scope",
	AllRepos:  "All repositories",
	Yesterday: "Yesterday",
})

// Of is the shared words in the language the window shows.
func Of(c *ui.Context) *Words { return Common.Of(c) }

// Plural chooses the singular form only for one, then fills the count in either language.
func Plural(text string, n int) string {
	one, many, plural := strings.Cut(text, "|")
	if plural && n != 1 {
		one = many
	}
	return strings.ReplaceAll(one, "{n}", strconv.Itoa(n))
}

// Thousands writes n with comma groups, as both languages do: 183,012.
func Thousands(n int) string {
	s := strconv.Itoa(n)
	sign := ""
	if n < 0 {
		sign, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}
