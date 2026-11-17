package session

import (
	"cmp"
	"fmt"
	"maps"
	"net/mail"
	"slices"
	"strings"

	"sheaf/internal/engine"
	"sheaf/internal/gitview"
	"sheaf/internal/query"
)

const (
	KeyBranch = "branch"
	KeyTarget = "target"
)

// Options lists branch names and the number of repositories that have each one.
func (s *Session) Options(_ query.Conditions, key string) ([]Option, error) {
	eng, _, _ := s.current()
	if eng == nil {
		return nil, errNotReady
	}
	switch key {
	case KeyBranch:
		return branchOptions(eng), nil
	case KeyTarget:
		return targetOptions(eng), nil
	}
	return nil, fmt.Errorf("unknown branch option %q", key)
}

// targetOptions: every local and remote branch by its short name (master, origin/uat), with the number of repos that have it,
// most recently moved first. Unlike branchOptions origin/x and x stay apart: a compare target is one exact ref.
func targetOptions(eng *engine.Engine) []Option {
	latest := map[string]int64{}
	repos := map[string]int{}
	for _, r := range eng.Index().Repos {
		for _, ref := range r.Refs {
			name, ok := strings.CutPrefix(ref.Name, "refs/heads/")
			if !ok {
				name, ok = strings.CutPrefix(ref.Name, "refs/remotes/")
			}
			if !ok || name == "HEAD" || strings.HasSuffix(name, "/HEAD") {
				continue
			}
			if t := r.Time[ref.Commit]; t > latest[name] {
				latest[name] = t
			}
			repos[name]++ // a ref name is unique within a repo
		}
	}
	out := make([]Option, 0, len(latest))
	for n := range latest {
		out = append(out, Option{Value: n, Count: repos[n]})
	}
	slices.SortFunc(out, func(a, b Option) int {
		return cmp.Or(cmp.Compare(latest[b.Value], latest[a.Value]), strings.Compare(a.Value, b.Value))
	})
	return out
}

// branchOptions: every branch name across repos, local or remote (origin/x counts as x), most recently moved
// first. Counting commits per branch would walk every repo for each of hundreds of names; the number of repos
// that have the name is what picking the branches of one requirement needs.
func branchOptions(eng *engine.Engine) []Option {
	latest := map[string]int64{}
	repos := map[string]int{}
	for _, r := range eng.Index().Repos {
		seen := map[string]bool{}
		for _, ref := range r.Refs {
			name, ok := strings.CutPrefix(ref.Name, "refs/heads/")
			if !ok {
				if rest, ok2 := strings.CutPrefix(ref.Name, "refs/remotes/"); ok2 {
					_, name, ok = strings.Cut(rest, "/")
				}
			}
			if !ok || name == "HEAD" {
				continue
			}
			if t := r.Time[ref.Commit]; t > latest[name] {
				latest[name] = t
			}
			if !seen[name] {
				seen[name] = true
				repos[name]++
			}
		}
	}
	out := make([]Option, 0, len(latest))
	for n := range latest {
		out = append(out, Option{Value: n, Count: repos[n]})
	}
	slices.SortFunc(out, func(a, b Option) int {
		return cmp.Or(cmp.Compare(latest[b.Value], latest[a.Value]), strings.Compare(a.Value, b.Value))
	})
	return out
}

// DefaultRelease is the release branch of a workspace that has not chosen one.
const DefaultRelease = "release"

func (s *Session) SetCompareBase(master string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, flow := s.cfg.Master, s.cfg.MergeFlow
	if len(s.cfg.MergeFlow) == 0 {
		s.cfg.MergeFlow = s.mergeFlowLocked()
	}
	s.cfg.Master = strings.TrimSpace(master)
	if err := s.saveConfig(); err != nil {
		s.cfg.Master, s.cfg.MergeFlow = prev, flow
		return err
	}
	return nil
}

// SetCompareTargets saves the filter page's release and master branches; they are the same for every filter.
func (s *Session) SetCompareTargets(release, master string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prevRelease, prevMaster, flow := s.cfg.Release, s.cfg.Master, s.cfg.MergeFlow
	if len(s.cfg.MergeFlow) == 0 {
		s.cfg.MergeFlow = s.mergeFlowLocked()
	}
	s.cfg.Release, s.cfg.Master = strings.TrimSpace(release), strings.TrimSpace(master)
	if s.cfg.Release == DefaultRelease {
		s.cfg.Release = ""
	}
	if err := s.saveConfig(); err != nil {
		s.cfg.Release, s.cfg.Master, s.cfg.MergeFlow = prevRelease, prevMaster, flow
		return err
	}
	return nil
}

// SetGlabConfig saves the workspace's own glab configuration directory ("" = the system's glab login). It is not checked here:
// the directory may be made after, and CheckGlabConfig tells the settings page.
func (s *Session) SetGlabConfig(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cfg.GlabConfig
	s.cfg.GlabConfig = strings.TrimSpace(dir)
	if err := s.saveConfig(); err != nil {
		s.cfg.GlabConfig = prev
		return err
	}
	return nil
}

// CheckGlabConfig is "" when dir can be used, else why not, in the English of the error (the page shows it as is).
func (s *Session) CheckGlabConfig(dir string) string {
	if err := gitview.CheckGlabConfig(dir); err != nil {
		return err.Error()
	}
	return ""
}

// Favs are the favourite compare targets of the workspace.
func (s *Session) Favs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.cfg.Favs...)
}

// SetFavs keeps the order given, without blanks or repeats.
func (s *Session) SetFavs(favs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, f := range favs {
		if f = strings.TrimSpace(f); f != "" && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	prev := s.cfg.Favs
	s.cfg.Favs = out
	if err := s.saveConfig(); err != nil {
		s.cfg.Favs = prev
		return err
	}
	return nil
}

// SetMe replaces the emails added by hand (the derived ones stay). Each must be a valid address.
func (s *Session) SetMe(emails []string) error {
	seen := map[string]bool{}
	clean := []string{}
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
			return fmt.Errorf("invalid email %q", e)
		}
		if !seen[e] {
			seen[e] = true
			clean = append(clean, e)
		}
	}
	s.mu.Lock()
	prev := s.cfg.MeManual
	before := s.meLocked()
	s.cfg.MeManual = clean
	changed := !slices.Equal(before, s.meLocked())
	err := s.saveConfig()
	if err != nil {
		s.cfg.MeManual = prev
	}
	s.mu.Unlock()
	if err == nil && changed {
		s.indexUpdated(nil)
	}
	return err
}

// SetBase sets a repo's base branch; "" clears it.
func (s *Session) SetBase(repo, branch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cfg.Bases
	s.cfg.Bases = maps.Clone(prev)
	if branch == "" {
		delete(s.cfg.Bases, repo)
	} else {
		s.cfg.Bases[repo] = branch
	}
	if err := s.saveConfig(); err != nil {
		s.cfg.Bases = prev
		return err
	}
	return nil
}
