package session

import (
	"context"

	"sheaf/internal/gitview"
)

// DiffTexts is one file before and after a change, whole: Preview Diff (internal/native/diffview) diffs them itself,
// so it can show the unchanged lines around a change too. A side the file is not on has no lines.
type DiffTexts struct {
	Old FileText `json:"old"`
	New FileText `json:"new"`
}

// CommitTexts reads file path of commit k against its first parent. The status and oldPath
// identify which sides exist and the source path of a rename or copy.
func (s *Session) CommitTexts(ctx context.Context, k CommitKey, path, oldPath, status string) (DiffTexts, error) {
	ctx = s.git(ctx, "commitTexts")
	eng, c, err := s.find(k)
	if err != nil {
		return DiffTexts{}, err
	}
	r := eng.Index().Repos[c.Repo]
	read := func(i int32, p string) (FileText, error) {
		lines, bin, cut, err := gitview.FileText(ctx, r.Dir, r.Oid(i), p)
		if lines == nil {
			lines = []string{}
		}
		return FileText{Lines: lines, Binary: bin, Cut: cut}, err
	}
	out := DiffTexts{Old: FileText{Lines: []string{}}, New: FileText{Lines: []string{}}}
	if status != "D" {
		if out.New, err = read(c.I, path); err != nil {
			return DiffTexts{}, err
		}
	}
	if ps := r.Parents(c.I); status != "A" && len(ps) > 0 {
		from := path
		if oldPath != "" {
			from = oldPath
		}
		if out.Old, err = read(ps[0], from); err != nil {
			return DiffTexts{}, err
		}
	}
	return out, nil
}
