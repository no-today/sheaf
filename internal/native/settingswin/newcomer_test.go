package settingswin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sheaf/internal/native/view/viewtest"
)

func TestFailedSaveRestoresTheDisplayedDraft(t *testing.T) {
	for _, tc := range []struct {
		name, tab, next string
		value           func(*Window) *string
		commit          func(*Window)
	}{
		{"pattern", "me", `TEAM-\d+`, func(w *Window) *string { return &w.req }, (*Window).commitRequirement},
		{"comparison", "base", "missing", func(w *Window) *string { return &w.master }, (*Window).commitTargets},
		{"glab", "glab", "/missing/glab", func(w *Window) *string { return &w.glab }, (*Window).commitGlab},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h, tt := open(t, tc.tab, true, Deps{})
			until(t, h, tt, "settings loaded", func() bool { return w.haveInfo })
			previous := *tc.value(w)
			before := h.S.Info()
			if err := os.MkdirAll(filepath.Join(h.S.DataDir(), "workspace.json.tmp"), 0o755); err != nil {
				t.Fatal(err)
			}
			*tc.value(w) = tc.next
			tc.commit(w)
			until(t, h, tt, "failed save and restored field", func() bool {
				return *tc.value(w) == previous && tt.HasText("workspace.json.tmp")
			})
			after := h.S.Info()
			if after.Master != before.Master || after.Requirement != before.Requirement || after.GlabConfig != before.GlabConfig {
				t.Fatalf("failed settings persisted: %+v", after)
			}
			(&viewtest.Harness{T: t, H: h, TT: tt}).Shot("settings-save-failed-" + tc.name)
		})
	}
}

func TestFailedLanguageSaveRestoresPreviousSelection(t *testing.T) {
	w, h, tt := open(t, "general", false, Deps{
		SetSettings: func(Settings) error { return errors.New("settings unavailable") },
	})
	until(t, h, tt, "general", func() bool { return tt.HasText("语言") })
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.set.Lang != "" || !tt.HasText("settings unavailable") {
		t.Fatalf("failed save kept selection %q; texts %q", w.set.Lang, tt.Texts())
	}
}

func TestGeneralShowsBuildVersionAndRevealsCommandLog(t *testing.T) {
	data, err := os.ReadFile("../../../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct{ Version string }
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != developmentVersion {
		t.Fatalf("development version %q differs from mygo.json %q", developmentVersion, config.Version)
	}
	for _, locale := range []string{"zh-Hans", "en-US"} {
		t.Run(locale, func(t *testing.T) {
			_, h, tt := open(t, "general", true, Deps{RevealCommandLog: func() {}})
			tt.SetLocale(locale)
			until(t, h, tt, "version", func() bool { return tt.HasText("Sheaf " + config.Version) })
			if tt.HasText(tr.For(locale).CmdLogOpen) {
				t.Errorf("the command log belongs to the workspace's index page, not general: %q", tt.Texts())
			}
		})
	}
}

func TestFailedRepoSaveRestoresSwitch(t *testing.T) {
	w, h, tt := open(t, "repos", true, Deps{})
	until(t, h, tt, "repos", func() bool { return w.haveInfo && tt.HasText("包含 repo-b") })
	if err := os.MkdirAll(filepath.Join(h.S.DataDir(), "workspace.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("包含 repo-b"); err != nil {
		t.Fatal(err)
	}
	until(t, h, tt, "failure", func() bool { return tt.HasText("workspace.json.tmp") })
	if len(w.info.Ignored) != 0 || len(h.S.Info().Ignored) != 0 {
		t.Fatal("failed save changed repo tracking")
	}
}
