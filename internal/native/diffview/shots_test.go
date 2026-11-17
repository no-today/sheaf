package diffview

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"sheaf/internal/native/theme"
	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
)

func TestShots(t *testing.T) {
	patch := session.Patch{Hunks: []session.Hunk{hunk("@@ -1,2 +1,2 @@",
		session.Line{Kind: " ", Old: 1, New: 1, Text: "package config"},
		session.Line{Kind: "-", Old: 2, Text: "const name = \"old\""},
		session.Line{Kind: "+", New: 2, Text: "const name = \"new\""},
	)}}
	parts := TextsOfAgg(session.AggPatch{Mode: "commits", Parts: []session.AggPart{
		{Hash: "e5cd1ac0123456789", Subject: "Keep configuration paths readable", Patch: patch},
		{Hash: "a10b20c0123456789", Subject: "Add workspace configuration", Patch: patch},
	}})
	for _, locale := range []string{"zh-Hans", "en-US"} {
		for _, scene := range []struct {
			name  string
			files []File
		}{
			{"empty", nil},
			{"added", []File{{Path: "notes.txt", Status: "A", Texts: &Texts{New: "A new note\n"}}}},
			{"deleted", []File{{Path: "notes.txt", Status: "D", Texts: &Texts{Old: "An old note\n"}}}},
			{"single", []File{{Path: "internal/config/settings.go", Status: "M", Texts: changedAt(5)}}},
			{"multiple", []File{{Path: "internal/config/settings.go", Status: "M", Texts: changedAt(50, 150, 250)}, {Path: "notes.txt", Status: "A", Texts: &Texts{New: "A new note\n"}}}},
			{"patch", []File{{Path: "internal/config/settings.go", Status: "M", Texts: &Texts{Parts: []Part{{Patch: patch}}}}}},
			{"parts", []File{{Path: "internal/config/settings.go", Status: "M", Texts: &parts}}},
		} {
			t.Run(locale+"/"+scene.name, func(t *testing.T) {
				s := New(scene.files, nil)
				t.Cleanup(s.Close)
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(theme.Theme())
					s.BeginFrame()
					View(c, s)
				}, 900, 400)
				tt.SetLocale(locale)
				tt.Frame()
				h := &viewtest.Harness{T: t, H: &viewtest.Host{}, TT: tt}
				name := "diffview-" + scene.name + "-" + locale
				h.Shot(name)
				if scene.name != "multiple" {
					return
				}
				label := "47 unmodified lines"
				if locale == "zh-Hans" {
					label = "47 行未修改"
				}
				r, ok := tt.Find(label)
				if !ok {
					r, ok = tt.Find("47 unmodified lines")
				}
				if !ok {
					t.Fatalf("missing fold bar %q: %q", label, tt.Texts())
				}
				tt.Move(r.X+r.W-8, r.Y+r.H/2)
				h.Shot(name + "-fold-hover")
				tt.Move(0, 0)
				s.Layout = editor.DiffUnified
				h.Shot(name + "-unified")
			})
		}
	}
}
