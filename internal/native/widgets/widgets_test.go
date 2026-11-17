package widgets

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestUndoToast(t *testing.T) {
	var toasts Toaster
	undone := 0
	tt := ui.NewTester(func(c *ui.Context) {
		if ui.Button(c, "Delete").Clicked() {
			toasts.Undo("Deleted", func() { undone++ }, "")
		}
		toasts.View(c)
	}, 600, 400)
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Deleted") || !tt.HasText("Undo") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if undone != 1 || tt.HasText("Deleted") {
		t.Errorf("undone %d; texts %q", undone, tt.Texts())
	}
	// Left alone, it goes after UndoTime, undoing nothing.
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(UndoTime - time.Second)
	if !tt.HasText("Undo") {
		t.Error("gone before its time")
	}
	tt.Advance(2 * time.Second)
	if tt.HasText("Deleted") || undone != 1 {
		t.Errorf("undone %d; texts %q", undone, tt.Texts())
	}
}

func TestOneToastAtATime(t *testing.T) {
	var toasts Toaster
	toasts.Show("first")
	toasts.Show("second")
	tt := ui.NewTester(toasts.View, 600, 400)
	if tt.HasText("first") || !tt.HasText("second") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestConfirmPopover(t *testing.T) {
	open, ok, closed := false, 0, 0
	tt := ui.NewTester(func(c *ui.Context) {
		b := Button(c, "Remove")
		if b.Clicked() {
			open = true
		}
		ConfirmPopover(c, b, &open, Confirm{Title: "Remove it?", OK: "Yes", Cancel: "No", OnOK: func() { ok++ }, OnClose: func() { closed++ }})
	}, 600, 400)
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("No"); err != nil {
		t.Fatal(err)
	}
	if open || ok != 0 || closed != 1 {
		t.Errorf("open %v ok %d closed %d", open, ok, closed)
	}
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	if open || ok != 1 || closed != 1 {
		t.Errorf("open %v ok %d closed %d", open, ok, closed)
	}
}

func TestPromptPopoverChecks(t *testing.T) {
	open, value, got := false, "", ""
	tt := ui.NewTester(func(c *ui.Context) {
		b := Button(c, "Save as")
		if b.Clicked() {
			open = true
		}
		PromptPopover(c, b, &open, &value, Prompt{Title: "Name", OK: "Save", Cancel: "Cancel",
			Check: func(v string) string {
				if v == "taken" {
					return "That name is taken"
				}
				return ""
			},
			OnOK: func(v string) { got = v }})
	}, 600, 400)
	if err := tt.Click("Save as"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter) // blank: nothing to take
	if got != "" || !open {
		t.Fatalf("took a blank name")
	}
	tt.Type("taken")
	if !tt.HasText("That name is taken") {
		t.Fatalf("texts %q", tt.Texts())
	}
	value = "fresh"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if got != "fresh" || open {
		t.Errorf("got %q open %v", got, open)
	}
}

func TestSelectAndSegmented(t *testing.T) {
	choice, seg := "a", 0
	tt := ui.NewTester(func(c *ui.Context) {
		Select(c, &choice, []string{"a", "b"}, func(s string) string { return "option " + s }, "Pick")
		Segmented(c, &seg, "Layout", "Split", "Unified")
	}, 600, 400)
	if err := tt.Click("Pick"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("option b"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Unified"); err != nil {
		t.Fatal(err)
	}
	if choice != "b" || seg != 1 {
		t.Errorf("choice %q seg %d", choice, seg)
	}
}

func TestShortcuts(t *testing.T) {
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Text(c, "x")
		Shortcuts(c, Shortcut{Mods: ui.Cmd, Key: ui.KeyR, Do: func() { n++ }})
	}, 200, 100)
	tt.Key(ui.Cmd, ui.KeyR)
	if n != 1 {
		t.Errorf("ran %d times", n)
	}
}
