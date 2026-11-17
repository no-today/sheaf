package session

import (
	"reflect"
	"testing"

	"sheaf/internal/discover"
)

func TestWithoutIgnored(t *testing.T) {
	repos := []discover.Repo{{Name: "a", Dir: "/w/a"}, {Name: "a/sub", Dir: "/w/a/sub"}, {Name: "ab", Dir: "/w/ab"}, {Name: "b", Dir: "/w/b"}, {Name: "c", Dir: "/w/c"}}
	names := func(rs []discover.Repo) []string {
		out := []string{}
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return out
	}
	tests := []struct {
		name          string
		ignored       []string
		kept, dropped []string
		with          map[string][]string
	}{
		{"nothing ignored", nil, []string{"a", "a/sub", "ab", "b", "c"}, []string{}, map[string][]string{}},
		{"one ignored", []string{"b"}, []string{"a", "a/sub", "ab", "c"}, []string{"b"}, map[string][]string{}},
		{"nested repos go with it, a sibling with the same prefix stays", []string{"a"}, []string{"ab", "b", "c"}, []string{"a", "a/sub"}, map[string][]string{"a": {"a/sub"}}},
		{"a nested repo alone", []string{"a/sub"}, []string{"a", "ab", "b", "c"}, []string{"a/sub"}, map[string][]string{}},
		{"unknown name ignored", []string{"x"}, []string{"a", "a/sub", "ab", "b", "c"}, []string{}, map[string][]string{}},
		{"all ignored", []string{"a", "ab", "b", "c"}, []string{}, []string{"a", "a/sub", "ab", "b", "c"}, map[string][]string{"a": {"a/sub"}}},
		{"nested one ignored too: not listed as taken along", []string{"a", "a/sub"}, []string{"ab", "b", "c"}, []string{"a", "a/sub"}, map[string][]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kept, dropped, with := withoutIgnored(repos, tt.ignored)
			if got := names(kept); !reflect.DeepEqual(got, tt.kept) {
				t.Errorf("kept = %v, want %v", got, tt.kept)
			}
			if got := names(dropped); !reflect.DeepEqual(got, tt.dropped) {
				t.Errorf("dropped = %v, want %v", got, tt.dropped)
			}
			if !reflect.DeepEqual(with, tt.with) {
				t.Errorf("with = %v, want %v", with, tt.with)
			}
		})
	}
}

func TestDropUnder(t *testing.T) {
	skip := []string{"/w/pbs/.git", "/w/pbs", "/w/pbs-fix"}
	tests := []struct {
		name       string
		dirs, want []string
	}{
		{"inside an ignored worktree", []string{"/w/pbs/src/main"}, []string{}},
		{"an ignored git dir itself", []string{"/w/pbs/.git"}, []string{}},
		{"an ignored linked worktree", []string{"/w/pbs-fix/a"}, []string{}},
		{"a sibling sharing the name's prefix", []string{"/w/pbs2/src"}, []string{"/w/pbs2/src"}},
		{"the workspace root above it", []string{"/w"}, []string{"/w"}},
		{"a tracked repo", []string{"/w/api/src", "/w/pbs/x"}, []string{"/w/api/src"}},
		{"unclean path", []string{"/w/pbs/src/../lib/"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dropUnder(tt.dirs, skip, nil); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dropUnder(%v) = %v, want %v", tt.dirs, got, tt.want)
			}
		})
	}
	if got := dropUnder([]string{"/w/a"}, nil, nil); !reflect.DeepEqual(got, []string{"/w/a"}) {
		t.Errorf("no skip: %v", got)
	}
	// A tracked repo can have a linked worktree inside an ignored repo's directory.
	keep := []string{"/w/pbs/sub", "/w/pbs/.git/modules/sub"}
	got := dropUnder([]string{"/w/pbs/sub/a", "/w/pbs/.git/modules/sub", "/w/pbs/src"}, skip, keep)
	if want := []string{"/w/pbs/sub/a", "/w/pbs/.git/modules/sub"}; !reflect.DeepEqual(got, want) {
		t.Errorf("keep inside skip: %v, want %v", got, want)
	}
}

func TestCleanNames(t *testing.T) {
	tests := []struct {
		in, want []string
	}{
		{nil, []string{}},
		{[]string{" a ", "", "b", "a"}, []string{"a", "b"}},
		{[]string{"b", "a"}, []string{"b", "a"}},
	}
	for _, tt := range tests {
		if got := cleanNames(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("cleanNames(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
