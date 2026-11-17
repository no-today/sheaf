package session

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSettingsSaveFailureKeepsPreviousConfigAndInfo(t *testing.T) {
	for _, initial := range []string{"default", "custom"} {
		t.Run(initial, func(t *testing.T) {
			s, _ := requirementFixture(t)
			if initial == "custom" {
				for _, set := range []func() error{
					func() error { return s.SetCompareTargets("uat", "origin/master") },
					func() error { return s.SetGlabConfig("/old/glab") },
					func() error { return s.SetMe([]string{"old@example.com"}) },
					func() error { return s.SetBase("repo", "master") },
					func() error { return s.SetRequirement(`OLD-\d+`) },
					func() error { return s.SetFavs([]string{"master"}) },
				} {
					if err := set(); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.MkdirAll(s.cfgPath+".tmp", 0o755); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name string
				set  func() error
			}{
				{"comparison base", func() error { return s.SetCompareBase("missing") }},
				{"comparison targets", func() error { return s.SetCompareTargets("next", "missing") }},
				{"glab directory", func() error { return s.SetGlabConfig("/new/glab") }},
				{"manual emails", func() error { return s.SetMe([]string{"new@example.com"}) }},
				{"main branch", func() error { return s.SetBase("repo", "missing") }},
				{"clear main branch", func() error { return s.SetBase("repo", "") }},
				{"ignored repos", func() error { return s.SetIgnored([]string{"repo"}) }},
				{"requirement pattern", func() error { return s.SetRequirement(`NEW-\d+`) }},
				{"favourite branches", func() error { return s.SetFavs([]string{"next"}) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					before := s.Info()
					beforeConfig, err := json.Marshal(s.cfg)
					if err != nil {
						t.Fatal(err)
					}
					if err := tc.set(); err == nil {
						t.Fatal("save to a directory succeeded")
					}
					afterConfig, err := json.Marshal(s.cfg)
					if err != nil {
						t.Fatal(err)
					}
					if string(afterConfig) != string(beforeConfig) {
						t.Errorf("config changed after failed save: before %s after %s", beforeConfig, afterConfig)
					}
					if after := s.Info(); !reflect.DeepEqual(before, after) {
						t.Errorf("Info changed after failed save: before %+v after %+v", before, after)
					}
				})
			}
		})
	}
}
