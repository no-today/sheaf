package settingswin

import (
	"slices"
	"strings"
)

const glabSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-"

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, glabSafe) == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// glabLoginLine is the terminal line that logs glab in with the workspace's own configuration directory (the leading
// ~/ stays unquoted so the shell expands it).
func glabLoginLine(dir, host string) string {
	d := strings.TrimSpace(dir)
	if host == "" {
		host = "<host>"
	}
	line := "glab auth login --hostname " + host
	if d == "" {
		return line
	}
	shown := ""
	switch {
	case d == "~":
		shown = "~"
	case strings.HasPrefix(d, "~/"):
		shown = "~/" + shellQuote(d[2:])
	default:
		shown = shellQuote(d)
	}
	return "GLAB_CONFIG_DIR=" + shown + " " + line
}

// withoutEmail is the manual list after removing one email.
func withoutEmail(list []string, email string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(e string) bool { return e == email })
}

// withEmail is the manual list after adding one (lowercased, trimmed; the caller validated it).
func withEmail(list []string, email string) []string {
	return append(slices.Clone(list), strings.ToLower(strings.TrimSpace(email)))
}

// fromRepos is the repos named by an automatic email's source: the first three, then how many more.
func fromRepos(repos []string) (shown string, more int) {
	if len(repos) > 3 {
		more = len(repos) - 3
		repos = repos[:3]
	}
	return strings.Join(repos, "、"), more
}

// agoKey tells how long ago updated (unix seconds) was at now: the unit and the count.
func agoKey(updated, now int64) (unit string, n int) {
	min := max(0, (now-updated)/60)
	switch {
	case min < 1:
		return "now", 0
	case min < 60:
		return "min", int(min)
	case min < 24*60:
		return "hour", int(min / 60)
	}
	return "day", int(min / 60 / 24)
}
