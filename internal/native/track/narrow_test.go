package track

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNarrowTabsKeepTheChangeSelectorVisible(t *testing.T) {
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			h := openReq(t, locale)
			w := tr.For(locale)
			for _, width := range []int{1440, 820, 1180} {
				h.TT.SetSize(width, 900)
				h.TT.Frame()
				picker, ok := h.TT.Access(w.DiffLabel + " " + w.DiffBase)
				if !ok || picker.Bounds.X+picker.Bounds.W > float32(width) || picker.Bounds.W <= 0 {
					t.Fatalf("change selector clipped at %d: %+v", width, picker)
				}
				_, base := h.TT.Find(w.Master)
				if base != (width != 820) {
					t.Errorf("comparison base visible=%v at %d", base, width)
				}
				if err := h.TT.Click(w.DiffLabel + " " + w.DiffBase); err != nil {
					t.Fatal(err)
				}
				h.TT.Frame()
				if h.p.pop.kind != "diffmode" {
					t.Fatal("change selector did not open")
				}
				h.TT.Key(0, ui.KeyEscape)
				h.TT.Frame()
			}
		})
	}
}
