package diffview

import "testing"

func TestStreamCanKeepSubsectionHeadersUnpinned(t *testing.T) {
	s, tt := longStream(t, 9)
	if !s.List.Header(0) || !s.List.Header(4) || s.List.Header(5) {
		t.Fatal("default header behavior changed")
	}
	s.Sticky = func(group string) bool { return group != "g1" }
	if !s.List.Header(0) || s.List.Header(4) || s.List.Header(5) {
		t.Fatal("subsection should not pin")
	}
	s.ScrollToFile(4)
	until(t, tt, "subsection file", func() bool { return s.Current() == 4 && !tt.FramePending() })
	tt.Scroll(450, 300, 0, 500)
	until(t, tt, "subsection scrolled away", func() bool { return !tt.FramePending() && s.boxes[4].Y < s.top-200 })
	if sub, ok := tt.Find("header g1"); ok && sub.Y+sub.H > s.top {
		t.Fatalf("subsection still pinned: %+v", sub)
	}
	if repo, ok := tt.Find("header g0"); !ok || repo.Y+repo.H <= s.top {
		t.Fatalf("parent header not pinned: %+v", repo)
	}
}
