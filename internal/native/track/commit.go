package track

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"sheaf/internal/query"
	"sheaf/internal/session"
)

func (p *Page) openCommit(row session.Row) {
	if !row.Resolved {
		p.showCommit(session.DiffSource{Kind: "commit", Repo: row.Repo, Hash: row.Hash})
		return
	}
	var bases []string
	for _, r := range p.rows {
		if r.Repo == row.Repo {
			for _, base := range []string{r.Branch, r.Base} {
				if !slices.Contains(bases, base) {
					bases = append(bases, base)
				}
			}
		}
	}
	s, ctx := p.s, p.h.Caller()
	go func() {
		src, err := resolvedCommitSource(ctx, s, row, bases)
		p.h.Update(func() {
			if err != nil {
				p.h.Toasts().Error(err.Error())
				return
			}
			p.showCommit(src)
		})
	}()
}

func (p *Page) showCommit(src session.DiffSource) {
	if err := p.h.Windows().OpenDiff(src); err != nil {
		p.h.Toasts().Error(err.Error())
	}
}

func resolvedCommitSource(ctx context.Context, s *session.Session, row session.Row, bases []string) (session.DiffSource, error) {
	cond := query.Default()
	cond.Paths = []string{row.Repo}
	cond.Regex = true
	cond.Q = " " + regexp.QuoteMeta(row.Hash) + " " + regexp.QuoteMeta(row.Repo) + "$"
	// Comparing with a branch that contains this commit selects the per-commit path in TagAggPatch.
	// Its remerge part contains only the merge's own edits, without the branch range or working directory.
	for _, base := range bases {
		if err := ctx.Err(); err != nil {
			return session.DiffSource{}, err
		}
		in := session.TagAggInput{Cond: &cond, Base: base}
		agg, err := s.TagAgg(ctx, in)
		if err != nil {
			return session.DiffSource{}, fmt.Errorf("commit diff: %w", err)
		}
		if agg.Commits != 1 || len(agg.Repos) != 1 || agg.Repos[0].Branch != "" {
			continue
		}
		return session.DiffSource{Kind: "agg", Repo: row.Repo, Hash: row.Hash, Row: agg.Repos[0].Key, Agg: &in}, nil
	}
	return session.DiffSource{}, fmt.Errorf("commit diff: no per-commit source for %s in %s", row.Hash, row.Repo)
}
