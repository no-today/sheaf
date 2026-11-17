package app

import (
	"fmt"
	"strings"
	"sync"
)

// Settings is settings.json: what applies to every workspace. (The emails of "me"
// are per workspace and live in the session's workspace.json.)
type Settings struct {
	// Lang is "zh" or "en"; "" = follow the system (English unless the system is Chinese).
	Lang string `json:"lang"`
	// CodeFont is the font family of code; "" = the bundled one.
	CodeFont string `json:"codeFont,omitempty"`
}

// Validate rejects values the settings page cannot produce.
func (s Settings) Validate() error {
	if s.Lang != "" && s.Lang != "zh" && s.Lang != "en" {
		return fmt.Errorf("invalid language %q", s.Lang)
	}
	if len(s.CodeFont) > 64 || strings.ContainsAny(s.CodeFont, "\",\n") {
		return fmt.Errorf("invalid code font %q", s.CodeFont)
	}
	return nil
}

// SettingsStore keeps Settings in memory and on disk.
type SettingsStore struct {
	mu   sync.Mutex
	path string
	cur  Settings
}

func openSettings(path string) *SettingsStore {
	s := &SettingsStore{path: path}
	readJSON(path, &s.cur)
	if s.cur.Validate() != nil {
		s.cur = Settings{} // a hand-edited file with junk: start over rather than fail
	}
	return s
}

func (s *SettingsStore) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *SettingsStore) Set(v Settings) error {
	if err := v.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSON(s.path, v); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	s.cur = v
	return nil
}
