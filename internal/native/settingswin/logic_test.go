package settingswin

import (
	"slices"
	"testing"
)

func TestGlabLoginLine(t *testing.T) {
	for _, c := range []struct{ dir, want string }{
		{"", "glab auth login --hostname <host>"},
		{" \t ", "glab auth login --hostname <host>"},
		{"/Users/a/gl-work", "GLAB_CONFIG_DIR=/Users/a/gl-work glab auth login --hostname <host>"},
		{"  ~/gl-work ", "GLAB_CONFIG_DIR=~/gl-work glab auth login --hostname <host>"},
		{"~/my gl", "GLAB_CONFIG_DIR=~/'my gl' glab auth login --hostname <host>"},
		{"/a b/it's", `GLAB_CONFIG_DIR='/a b/it'\''s' glab auth login --hostname <host>`},
		{"~", "GLAB_CONFIG_DIR=~ glab auth login --hostname <host>"},
	} {
		if got := glabLoginLine(c.dir, ""); got != c.want {
			t.Errorf("%q: got %q, want %q", c.dir, got, c.want)
		}
	}
}

func TestManualEmails(t *testing.T) {
	if got := withoutEmail([]string{"a@x.com", "b@x.com"}, "a@x.com"); !slices.Equal(got, []string{"b@x.com"}) {
		t.Errorf("remove: %v", got)
	}
	if got := withoutEmail([]string{"a@x.com"}, "a@x.com"); len(got) != 0 { // may empty the list
		t.Errorf("remove last: %v", got)
	}
	if got := withoutEmail([]string{"a@x.com"}, "zzz@x.com"); !slices.Equal(got, []string{"a@x.com"}) {
		t.Errorf("remove none: %v", got)
	}
	if got := withEmail([]string{"a@x.com"}, " B@X.com "); !slices.Equal(got, []string{"a@x.com", "b@x.com"}) {
		t.Errorf("add: %v", got)
	}
}

func TestFromRepos(t *testing.T) {
	for _, c := range []struct {
		repos []string
		shown string
		more  int
	}{
		{[]string{"a", "b"}, "a、b", 0},
		{[]string{"a", "b", "c"}, "a、b、c", 0},
		{[]string{"a", "b", "c", "d", "e"}, "a、b、c", 2},
	} {
		if shown, more := fromRepos(c.repos); shown != c.shown || more != c.more {
			t.Errorf("%v: %q %d", c.repos, shown, more)
		}
	}
}

func TestAgoKey(t *testing.T) {
	for _, c := range []struct {
		ago  int64
		unit string
		n    int
	}{{0, "now", 0}, {59, "now", 0}, {60, "min", 1}, {59 * 60, "min", 59}, {3600, "hour", 1}, {23 * 3600, "hour", 23}, {24 * 3600, "day", 1}, {-100, "now", 0}} {
		if unit, n := agoKey(1000-c.ago, 1000); unit != c.unit || n != c.n {
			t.Errorf("%ds: %s %d", c.ago, unit, n)
		}
	}
}
