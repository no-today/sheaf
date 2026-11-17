package track

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func reqTitle(h *harness, id string) (string, bool) {
	for _, r := range h.p.reqs {
		if r.ID == id {
			return r.Title, r.Named
		}
	}
	return "", false
}

// rightClickFirstRow opens the menu of the first requirement row under the group heading.
func rightClickFirstRow(t *testing.T, h *harness, group string) {
	t.Helper()
	r, ok := h.TT.Find(group)
	if !ok {
		t.Fatalf("no group %q; texts %q", group, h.TT.Texts())
	}
	h.TT.RightClickAt(r.X+20, r.Y+r.H+2+13)
	h.TT.Frame()
}

func TestRenameARequirementFromItsRow(t *testing.T) {
	h := open(t, "en-US")
	rightClickFirstRow(t, h, "Awaiting master")
	if err := h.TT.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	if h.p.renaming != "YT1" || h.p.renameText != "work" {
		t.Fatalf("rename state: %q %q", h.p.renaming, h.p.renameText)
	}
	h.TT.Key(ui.Cmd, ui.KeyA)
	h.TT.Type("Billing rework")
	h.TT.Key(0, ui.KeyEnter)
	h.Until("the new title", func() bool { title, named := reqTitle(h, "YT1"); return named && title == "Billing rework" })
	if h.p.renaming != "" || h.p.temp.Title != "Billing rework" {
		t.Errorf("after saving: renaming %q, header title %q", h.p.renaming, h.p.temp.Title)
	}

	// Escape drops the edit; so does leaving the field.
	for _, cancel := range []func(){func() { h.TT.Key(0, ui.KeyEscape) }, func() { h.TT.ClickAt(700, 300) }} {
		rightClickFirstRow(t, h, "Awaiting master")
		if err := h.TT.ChooseMenuItem("Rename"); err != nil {
			t.Fatal(err)
		}
		h.TT.Frame()
		h.TT.Type("x")
		cancel()
		h.TT.Frame()
		if h.p.renaming != "" {
			t.Fatal("the edit stayed open")
		}
		if title, _ := reqTitle(h, "YT1"); title != "Billing rework" {
			t.Fatalf("a cancelled edit changed the title to %q", title)
		}
	}

	// An empty name goes back to the commits' title.
	rightClickFirstRow(t, h, "Awaiting master")
	if err := h.TT.ChooseMenuItem("Rename"); err != nil {
		t.Fatal(err)
	}
	h.TT.Frame()
	h.TT.Key(ui.Cmd, ui.KeyA)
	h.TT.Key(0, ui.KeyBackspace)
	h.TT.Key(0, ui.KeyEnter)
	h.Until("the commit title again", func() bool { title, named := reqTitle(h, "YT1"); return !named && title == "work" })
}
