package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	s := openSettings(path)
	if s.Get() != (Settings{}) {
		t.Fatalf("defaults: %+v", s.Get())
	}
	want := Settings{Lang: "en"}
	if err := s.Set(want); err != nil {
		t.Fatal(err)
	}
	if got := openSettings(path).Get(); got != want {
		t.Errorf("reloaded: %+v", got)
	}
}

func TestSettingsValidate(t *testing.T) {
	for name, c := range map[string]struct {
		s  Settings
		ok bool
	}{
		"empty":          {Settings{}, true},
		"zh":             {Settings{Lang: "zh"}, true},
		"other language": {Settings{Lang: "fr"}, false},
	} {
		if err := c.s.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a rejected value is not saved
	path := filepath.Join(t.TempDir(), "settings.json")
	s := openSettings(path)
	if err := s.Set(Settings{Lang: "fr"}); err == nil {
		t.Error("invalid settings accepted")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("file written for invalid settings")
	}
	// a hand-edited file with junk starts over
	os.WriteFile(path, []byte(`{"lang":"klingon"}`), 0o644)
	if openSettings(path).Get() != (Settings{}) {
		t.Error("junk file should give defaults")
	}
}

func TestSettingsIgnoreRemovedPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"lang":"zh","reopenLast":false,"editor":"unknown","terminal":"gone","ide":"fork"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := openSettings(path).Get(); got != (Settings{Lang: "zh"}) {
		t.Fatalf("removed preferences lost the saved language: %+v", got)
	}
}
