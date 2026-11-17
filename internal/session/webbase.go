package session

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"sheaf/internal/discover"
)

// scpRemote is git's scp-like form: [user@]host:path, no "://" and no slash before the colon.
var scpRemote = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):(.+)$`)

// WebBase is the web address of the project an origin URL points at (http://host/group/proj), "" when it cannot be made.
// Credentials (user:token@) are dropped, never kept; ssh and git forms become http, with no port (the ssh port is not the web's).
func WebBase(remote string) string {
	remote = strings.TrimSpace(remote)
	var host, path string
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" {
			return ""
		}
		host, path = u.Hostname(), u.Path
		scheme := "http"
		if u.Scheme == "https" {
			scheme = "https"
		}
		if u.Scheme == "http" || u.Scheme == "https" {
			if p := u.Port(); p != "" {
				host += ":" + p
			}
		}
		if p := cleanProjectPath(path); p != "" {
			return scheme + "://" + host + "/" + p
		}
		return ""
	}
	m := scpRemote.FindStringSubmatch(remote)
	if m == nil {
		return ""
	}
	if p := cleanProjectPath(m[2]); p != "" {
		return "http://" + m[1] + "/" + p
	}
	return ""
}

func cleanProjectPath(p string) string {
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	return strings.Trim(p, "/")
}

// RepoWebBases is repo name -> WebBase of its origin, only for repos that have a usable one. Read once per session (git config);
// the origin URL itself, which may carry a token, never leaves this function.
func (s *Session) RepoWebBases(ctx context.Context) map[string]string {
	s.mu.Lock()
	if s.webBases != nil {
		defer s.mu.Unlock()
		return s.webBases
	}
	repos := append([]discover.Repo(nil), s.repos...)
	s.mu.Unlock()
	bases := make([]string, len(repos))
	if err := parallel(ctx, len(repos), 8, func(i int) {
		bases[i] = WebBase(discover.RepoOriginURL(ctx, repos[i].Dir))
	}); err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for i, r := range repos {
		if bases[i] != "" {
			out[r.Name] = bases[i]
		}
	}
	s.mu.Lock()
	s.webBases = out
	s.mu.Unlock()
	return out
}
