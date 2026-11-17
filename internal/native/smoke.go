package native

import (
	"strconv"

	"sheaf/internal/native/view"
)

// smoke is the workspace window's screenshot scenario, from SHEAF_NATIVE_*.
type smoke struct {
	on       bool
	view     view.ID    // SHEAF_NATIVE_VIEW: the view to show
	scenario view.Smoke // SHEAF_NATIVE_Q / ROW, handed to the requirement page
	win      string     // SHEAF_NATIVE_WIN: settings
}

func smokeFromEnv(getenv func(string) string) smoke {
	s := smoke{view: view.ID(getenv("SHEAF_NATIVE_VIEW")), win: getenv("SHEAF_NATIVE_WIN"), scenario: view.Smoke{Q: getenv("SHEAF_NATIVE_Q"), Row: -1}}
	if n, err := strconv.Atoi(getenv("SHEAF_NATIVE_ROW")); err == nil && n >= 0 {
		s.scenario.Row = n
	}
	if s.win != "settings" {
		s.win = ""
	}
	s.on = s.view != "" || s.win != "" || s.scenario != (view.Smoke{Row: -1})
	return s
}
