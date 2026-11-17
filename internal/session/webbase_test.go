package session

import (
	"strings"
	"testing"
)

func TestWebBase(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"https", "https://gitlab.example.com/g/p.git", "https://gitlab.example.com/g/p"},
		{"https no .git", "https://gitlab.example.com/g/p", "https://gitlab.example.com/g/p"},
		{"http", "http://gitlab.example.com/g/p.git", "http://gitlab.example.com/g/p"},
		{"credentials", "https://oauth2:glpat-SECRET@gitlab.example.com/g/p.git", "https://gitlab.example.com/g/p"},
		{"user only", "https://bob@gitlab.example.com/g/p.git", "https://gitlab.example.com/g/p"},
		{"http port kept", "http://gitlab.example.com:8080/g/p.git", "http://gitlab.example.com:8080/g/p"},
		{"subgroups", "https://gitlab.example.com/a/b/c/p.git", "https://gitlab.example.com/a/b/c/p"},
		{"scp ssh", "git@gitlab.example.com:g/p.git", "http://gitlab.example.com/g/p"},
		{"scp subgroups", "git@gitlab.example.com:a/b/p.git", "http://gitlab.example.com/a/b/p"},
		{"ssh url with port", "ssh://git@gitlab.example.com:2222/g/p.git", "http://gitlab.example.com/g/p"},
		{"ssh url", "ssh://git@gitlab.example.com/g/p.git", "http://gitlab.example.com/g/p"},
		{"trailing slash", "https://gitlab.example.com/g/p/", "https://gitlab.example.com/g/p"},
		{"empty", "", ""},
		{"local path", "/Users/x/repo.git", ""},
		{"no path", "https://gitlab.example.com", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WebBase(tc.in)
			if got != tc.want {
				t.Errorf("WebBase(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got != "" && (strings.Contains(got, "SECRET") || strings.Contains(got, "@")) {
				t.Errorf("credentials leaked: %q", got)
			}
		})
	}
}
