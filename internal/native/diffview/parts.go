package diffview

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/theme"
	"sheaf/internal/session"
)

// Part is one patch of a file shown as patches only, under its title: a filter's file commit by commit.
type Part struct {
	Title string // "" for none
	Patch session.Patch
}

// AggLoader reads the filter page's merged diff (session.TagAggPatch of in) of files whose Source is their
// session.AggFile: a repo with a branch diff has one patch; a repo without (merged into its base, or no base) has one per
// commit, oldest first, each under its commit.
func AggLoader(s *session.Session, in session.TagAggInput) Loader {
	return func(ctx context.Context, f File) (Texts, error) {
		af, _ := f.Source.(session.AggFile)
		// TagAggPatch supersedes the running call of its caller: a caller per State (a page's Preview Diff, a diff window)
		// and file, as the file shown and the one after it load together; the State itself stops the loads it stepped past.
		ctx = session.Caller(ctx, "aggPatch "+ownerOf(ctx)+"\x00"+af.Key+"\x00"+strconv.FormatBool(af.Worktree)+"\x00"+af.Path)
		ap, err := s.TagAggPatch(ctx, in, af.Key, af.Path, f.OldPath, af.Untracked, af.Worktree, false)
		if err != nil {
			return Texts{}, err
		}
		return TextsOfAgg(ap), nil
	}
}

// TextsOfAgg is a merged diff as parts: titled "hash  subject" in commits mode (and a range diff's merge commit parts), More the commits not shown. Binary when
// every part is: a binary file changed by several commits too.
func TextsOfAgg(ap session.AggPatch) Texts {
	t := Texts{More: ap.More}
	binary := len(ap.Parts) > 0
	for _, x := range ap.Parts {
		binary = binary && x.Patch.Binary
		title := ""
		if ap.Mode == "commits" || x.Hash != "" {
			title = x.Hash[:min(len(x.Hash), 7)] + "  " + x.Subject
		}
		t.Parts = append(t.Parts, Part{Title: title, Patch: x.Patch})
		t.Resolved = t.Resolved || ap.Mode == "range" && x.Hash != ""
	}
	if binary {
		return Texts{Binary: true, More: ap.More}
	}
	return t
}

// binaryParts are the titles of the parts that are binary (no hunks to show): the viewer names them in a note.
func binaryParts(parts []Part) []string {
	var out []string
	for _, p := range parts {
		if p.Patch.Binary {
			out = append(out, p.Title)
		}
	}
	return out
}

// ownerKey marks the context of a State's loads with the State (ownerOf).
type ownerKey struct{}

// ownerOf names the State a load runs for, "" outside one.
func ownerOf(ctx context.Context) string {
	s, _ := ctx.Value(ownerKey{}).(string)
	return s
}

// partMark starts the section of a part's first hunk, then the part's index and partMark again: the viewer hands its
// separators only their header, which is how the separator finds the part's title.
const partMark = "\x00"

var hunkHead = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// patchOf joins the parts into one patch of path, every hunk numbered as in the file; titles are the parts' titles,
// which the sections of their first hunks point at.
func patchOf(path string, parts []Part) (editor.FilePatch, []string) {
	fp := editor.FilePatch{OldName: path, NewName: path}
	var titles []string
	for _, pt := range parts {
		for j, h := range pt.Patch.Hunks {
			ph := hunkOf(h)
			if j == 0 && pt.Title != "" {
				ph.Section = partMark + strconv.Itoa(len(titles)) + partMark + ph.Section
				titles = append(titles, pt.Title)
			}
			fp.Hunks = append(fp.Hunks, ph)
		}
	}
	return fp, titles
}

// hunkOf is a session hunk as the editor's: its ranges from its header, else from its lines.
func hunkOf(h session.Hunk) editor.PatchHunk {
	var ph editor.PatchHunk
	for _, l := range h.Lines {
		ph.Lines = append(ph.Lines, l.Kind+l.Text)
		if l.Kind != "+" {
			ph.OldLines++
			if ph.OldStart == 0 {
				ph.OldStart = l.Old
			}
		}
		if l.Kind != "-" {
			ph.NewLines++
			if ph.NewStart == 0 {
				ph.NewStart = l.New
			}
		}
	}
	if m := hunkHead.FindStringSubmatch(h.Header); m != nil {
		num := func(s string, or int) int {
			if s == "" {
				return or
			}
			n, _ := strconv.Atoi(s)
			return n
		}
		ph.OldStart, ph.OldLines = num(m[1], 0), num(m[2], 1)
		ph.NewStart, ph.NewLines = num(m[3], 0), num(m[4], 1)
		ph.Section = m[5]
	}
	return ph
}

// partOf splits a separator's header into the title of the part it starts and the hunk's own header.
func partOf(header string, titles []string) (title, rest string, ok bool) {
	i := strings.Index(header, partMark)
	if i < 0 {
		return "", header, false
	}
	idx, section, found := strings.Cut(header[i+len(partMark):], partMark)
	n, err := strconv.Atoi(idx)
	if !found || err != nil || n < 0 || n >= len(titles) {
		return "", header, false
	}
	return titles[n], strings.TrimSpace(header[:i] + section), true
}

// Part titles distinguish commits whose patches do not share one pair of file versions.
func partSeparator(titles []string, th *Theme) func(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
	return func(c *ui.Context, sep ui.Element, s *editor.DiffSeparator) {
		if s.Lines > 0 {
			foldSeparator(c, sep, s)
			return
		}
		title, rest, ok := partOf(s.Header, titles)
		if !ok {
			sep.Padding(4).Children(func() {
				ui.Row(c).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center).PaddingX(8).Children(func() {
					ui.Text(c, s.Header).Font(s.Font().Family).FontSize(theme.FSSmall).TextColor(theme.Faint).SingleLine().Shrink(1).MinWidth(0)
				})
			})
			return
		}
		sep.Padding(4).Children(func() {
			ui.Row(c).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center).Gap(12).Padding(0, 10).Radius(6).Background(th.Part).Children(func() {
				ui.Text(c, title).Font(s.Font().Family).FontSize(theme.FSSmall).TextColor(th.Text).SingleLine().Shrink(1).MinWidth(0)
				ui.Box(c).Grow(1)
				ui.Text(c, rest).Font(s.Font().Family).FontSize(theme.FSSmall).TextColor(theme.Faint).SingleLine().Shrink(2).MinWidth(0)
			})
		})
	}
}
