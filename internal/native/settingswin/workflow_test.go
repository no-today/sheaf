package settingswin

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sheaf/internal/native/view/viewtest"
	"sheaf/internal/session"
)

func TestWorkspaceFlowCanBeReorderedAndRemovedWithUndo(t *testing.T) {
	window, host, tester := open(t, "base", true, Deps{})
	if err := host.S.SetMergeFlow([]session.MergeStep{{Name: "release", Branch: "release"}, {Name: "master"}}); err != nil {
		t.Fatal(err)
	}
	until(t, host, tester, "workspace flow", func() bool { return len(window.flow) == 2 && tester.HasText("合并流程") })
	if err := tester.Click("上移第 2 阶段"); err != nil {
		t.Fatal(err)
	}
	until(t, host, tester, "reordered", func() bool {
		flow := host.S.Info().MergeFlow
		return len(flow) == 2 && flow[0].Name == "master" && len(window.flow) == 2 && window.flow[0].Name == "master"
	})
	if err := tester.Click("删除第 1 阶段"); err != nil {
		t.Fatal(err)
	}
	until(t, host, tester, "removed", func() bool { return len(host.S.Info().MergeFlow) == 1 && len(window.flow) == 1 })
	if err := tester.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	until(t, host, tester, "restored", func() bool {
		flow := host.S.Info().MergeFlow
		return len(flow) == 2 && flow[0].Name == "master" && len(window.flow) == 2 && window.flow[0].Name == "master"
	})
	if err := tester.Click("+ 添加阶段"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("删除第 3 阶段"); err != nil {
		t.Fatal(err)
	}
	until(t, host, tester, "discarded empty target", func() bool { return len(window.flow) == 2 })
	if err := tester.Click("+ 添加阶段"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("第 3 阶段分支"); err != nil {
		t.Fatal(err)
	}
	tester.Type("hotfix")
	tester.Key(0, ui.KeyEnter)
	until(t, host, tester, "added", func() bool {
		flow := host.S.Info().MergeFlow
		return len(flow) == 3 && flow[2].Branch == "hotfix" && flow[2].Name == "hotfix"
	})
}

func TestMergeFlowHintUsesLocalizedSpacing(t *testing.T) {
	w, h, tt := open(t, "base", true, Deps{})
	until(t, h, tt, "merge flow", func() bool { return len(w.flow) > 0 })
	for _, tc := range []struct {
		locale, separator string
	}{
		{"zh-Hans", "归档。终点分支："},
		{"en-US", "when done. Final branch: "},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			tt.SetLocale(tc.locale)
			tt.Frame()
			if !tt.HasText(tc.separator + w.flow[len(w.flow)-1].Name) {
				t.Fatalf("missing localized baseline spacing in %q", tt.Texts())
			}
		})
	}
}

func TestRepositoryBaselineOnlySetting(t *testing.T) {
	window, host, tester := open(t, "base", true, Deps{})
	tester.SetSize(900, 1200)
	until(t, host, tester, "repository settings", func() bool {
		_, ok := tester.Find("repo-a 只看终点")
		return ok
	})
	if err := tester.Click("repo-a 只看终点"); err != nil {
		t.Fatal(err)
	}
	until(t, host, tester, "baseline-only saved", func() bool {
		for _, repo := range window.info.Repos {
			if repo.Name == "repo-a" {
				return repo.BaselineOnly
			}
		}
		return false
	})
	(&viewtest.Harness{T: t, H: host, TT: tester}).Shot("settings-baseline-only-zh-Hans")
	if err := tester.Click("repo-a 只看终点"); err != nil {
		t.Fatal(err)
	}
	until(t, host, tester, "full flow restored", func() bool {
		for _, repo := range host.S.Info().Repos {
			if repo.Name == "repo-a" {
				return !repo.BaselineOnly
			}
		}
		return false
	})
}
