package diffview

import (
	"context"
	"strings"

	"sheaf/internal/session"
)

// FilesOf lists a commit's changed files (session.Files); each File's Source is its FileChange.
func FilesOf(changes []session.FileChange) []File {
	out := make([]File, len(changes))
	for i, fc := range changes {
		out[i] = File{Path: fc.Path, OldPath: fc.OldPath, Status: fc.Status, Source: fc}
	}
	return out
}

// CommitLoader reads files of FilesOf against their commit's first parent. A file of a union of commits shows the
// newest commit that changed it (FileChange.Latest); others show commit k.
func CommitLoader(s *session.Session, k session.CommitKey) Loader {
	return func(ctx context.Context, f File) (Texts, error) {
		at := k
		if fc, ok := f.Source.(session.FileChange); ok && fc.Latest != "" {
			at.Hash = fc.Latest
			if fc.Repo != "" { // a union across repos
				at.Repo = fc.Repo
			}
		}
		t, err := s.CommitTexts(ctx, at, f.Path, f.OldPath, f.Status)
		if err != nil {
			return Texts{}, err
		}
		return Texts{
			Old: joinLines(t.Old.Lines), New: joinLines(t.New.Lines),
			Binary: t.Old.Binary || t.New.Binary, Cut: t.Old.Cut || t.New.Cut,
		}, nil
	}
}

// UncommittedLoader reads both sides of a worktree file change in full.
func UncommittedLoader(s *session.Session, worktree string) Loader {
	return func(ctx context.Context, f File) (Texts, error) {
		fs, _ := f.Source.(session.FileStatus)
		area, status := fs.Area, fs.Status
		if area == "" {
			area, status = "head", f.Status
		}
		t, err := s.UncommittedTexts(ctx, worktree, f.Path, f.OldPath, area, status)
		if err != nil {
			return Texts{}, err
		}
		return Texts{
			Old: joinLines(t.Old.Lines), New: joinLines(t.New.Lines),
			Binary: t.Old.Binary || t.New.Binary, Cut: t.Old.Cut || t.New.Cut,
		}, nil
	}
}

// joinLines undoes session.FileText's split. Whether the file ended in a newline is lost, so a change of only that
// shows as no change.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
