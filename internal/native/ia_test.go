package native

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"sheaf/internal/native/view"
)

func TestIA(t *testing.T) {
	b, err := os.ReadFile("../../docs/product/views.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	if len(registry) != 1 || registry[0].id != view.Track {
		t.Fatalf("workspace views = %+v, want only track", registry)
	}
	headings := regexp.MustCompile(`(?m)^### ([a-z]+)\s*$`).FindAllStringSubmatch(doc, -1)
	if len(headings) != len(registry) {
		t.Errorf("docs/product/views.md lists %d views, want %d", len(headings), len(registry))
	}
	for _, v := range registry {
		heading := regexp.MustCompile(`(?m)^### ` + regexp.QuoteMeta(string(v.id)) + `\s*$`)
		if !heading.MatchString(doc) {
			t.Errorf("docs/product/views.md is missing ### %s", v.id)
		}
	}
	for _, window := range []string{"工作区", "改动", "设置"} {
		entry := "**" + window + " 窗口**"
		compact := "**" + window + "窗口**"
		if !strings.Contains(doc, entry) && !strings.Contains(doc, compact) {
			t.Errorf("docs/product/views.md is missing %s", compact)
		}
	}
}
