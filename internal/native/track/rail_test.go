package track

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
)

func TestRepoRailNamePriority(t *testing.T) {
	for _, locale := range []string{"en-US", "zh-Hans"} {
		for _, repo := range []string{"pay-core", "checkout-api", "receipt-worker", strings.Repeat("W", 100)} {
			id := repo
			long := len(repo) == 100
			if long {
				id = "long-name"
			}
			t.Run(locale+"/"+id, func(t *testing.T) {
				p := &Page{
					res:  &session.TagAggResult{},
					flow: []session.MergeStep{{Name: "release"}, {Name: "master"}},
					rows: []session.AggRepo{{Key: repo, Repo: repo, Stage: session.MergeStage(0)}},
				}
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Themed(c, theme.ListTheme()).Grow(1).Children(func() {
						ui.Column(c).Width(railWidth).Padding(4, 0, 12, 0).AlignItems(ui.Stretch).Children(func() {
							p.repoStages(c, tr.Of(c))
						})
					})
				}, railWidth, 120)
				tt.SetLocale(locale)
				tt.Frame()
				h := &viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}
				h.Shot("repo-rail-" + id + "-" + locale)
				name, ok := tt.Find(repo)
				if !ok {
					t.Fatalf("repo name %q is missing", repo)
				}
				font := ui.Font{Family: theme.UIFont, Size: theme.FSBody}
				width, _ := ui.ShapeText(repo, font, 10000).Size()
				if !long && name.W+0.5 < width {
					t.Errorf("repo %q is truncated: width %.1f, needs %.1f", repo, name.W, width)
				}
				if name.X < 0 || name.X+name.W > railWidth-16 {
					t.Errorf("repo escapes the rail: %+v", name)
				}
				stage := p.stageName(tr.For(locale), session.MergeStage(0))
				status, ok := tt.Find(stage)
				if !ok || status.W <= 0 || status.X < name.X+name.W || status.X+status.W > railWidth-16 {
					t.Fatalf("stage overlaps or escapes the rail: name %+v, stage %+v", name, status)
				}
				stageWidth, _ := ui.ShapeText(stage, ui.Font{Family: theme.UIFont, Size: theme.FSSmall}, 1000).Size()
				if long {
					ellipsisWidth, _ := ui.ShapeText("…", ui.Font{Family: theme.UIFont, Size: theme.FSSmall}, 1000).Size()
					if width <= railWidth || name.W >= width || status.W > theme.FSSmall+0.5 || status.W < ellipsisWidth-0.5 {
						t.Errorf("long repo must shrink after stage reaches its ellipsis: repo %.1f/%.1f, stage %.1f", name.W, width, status.W)
					}
					assertRepoRailEllipsis(t, tt, repo, name, locale)
				} else if locale == "zh-Hans" && status.W+0.5 < stageWidth {
					t.Errorf("Chinese stage layout changed: width %.1f, needs %.1f", status.W, stageWidth)
				}
				if status.W+0.5 < stageWidth {
					tt.Move(status.X+status.W/2, status.Y+status.H/2)
					tt.Advance(time.Second)
					count := 0
					for _, text := range tt.Texts() {
						if text == stage {
							count++
						}
					}
					if count < 2 {
						t.Error("truncated stage has no full tooltip")
					}
					h.Shot("repo-rail-stage-tooltip-" + id + "-" + locale)
				}
			})
		}
	}
}

func assertRepoRailEllipsis(t *testing.T, tt *ui.Tester, repo string, box ui.Rect, locale string) {
	t.Helper()
	font := ui.Font{Family: theme.UIFont, Size: theme.FSBody}
	shown := "…"
	for n := 1; n < len(repo); n++ {
		candidate := repo[:n] + "…"
		width, _ := ui.ShapeText(candidate, font, 10000).Size()
		if width > box.W {
			break
		}
		shown = candidate
	}
	ref := ui.NewTester(func(c *ui.Context) {
		ui.Themed(c, theme.ListTheme()).Grow(1).Children(func() {
			ui.Text(c, shown).SingleLine().Absolute().Left(box.X).Top(box.Y)
		})
	}, railWidth, 120)
	ref.SetLocale(locale)
	tt.SetScale(2)
	ref.SetScale(2)
	tt.Frame()
	ref.Frame()
	actual, expected := tt.Image(), ref.Image()
	for y := int(box.Y * 2); y < int((box.Y+box.H)*2); y++ {
		x := int(box.X * 2)
		a, b := actual.PixOffset(x, y), expected.PixOffset(x, y)
		width := int(box.W*2) * 4
		if !bytes.Equal(actual.Pix[a:a+width], expected.Pix[b:b+width]) {
			t.Fatalf("repo does not render the expected ellipsis %q", shown)
		}
	}
}

func TestRailSearchFiltersExistingRequirementsWithoutOpeningAResult(t *testing.T) {
	h := openReq(t, "zh-Hans")
	landed := session.Requirement{ID: "YT-9998", Title: "archived fixture", Stage: session.StageArchived, Repos: 1}
	h.p.reqs = append(h.p.reqs, landed)
	h.TT.Frame()
	label := landed.ID + " " + landed.Title
	if _, ok := h.TT.Find(label); ok {
		t.Fatal("the landed group is not folded by default")
	}
	shown := h.p.shownReq()
	for _, q := range []string{landed.ID, landed.Title, "unknown-fix-text"} {
		if err := h.TT.Click(wordsOf(h).FindReq); err != nil {
			t.Fatal(err)
		}
		h.TT.Key(ui.Cmd, ui.KeyA)
		h.TT.Type(q)
		h.TT.Frame()
		_, found := h.TT.Find(label)
		if found != (q != "unknown-fix-text") {
			t.Errorf("search %q: landed row visible = %v", q, found)
		}
		if h.p.shownReq() != shown || h.p.input == nil || h.p.input.Requirement != shown {
			t.Fatalf("search opened a new result: shown %q, input %+v", h.p.shownReq(), h.p.input)
		}
	}
	h.TT.Key(0, ui.KeyEscape)
	h.TT.Frame()
	if h.p.find != "" {
		t.Fatal("Escape did not clear the existing filter")
	}
	if _, ok := h.TT.Find(label); ok {
		t.Fatal("clearing search did not fold the landed group again")
	}
}
