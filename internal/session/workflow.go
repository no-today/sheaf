package session

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type MergeStep struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
}

func (s *Session) mergeFlowLocked() []MergeStep { return s.cfg.mergeFlow() }

// mergeFlow is the saved flow, or the one older settings imply when none was saved.
func (c Config) mergeFlow() []MergeStep {
	if len(c.MergeFlow) > 0 {
		return slices.Clone(c.MergeFlow)
	}
	if c.Release == "" {
		return []MergeStep{{Name: cmp.Or(c.Master, "main"), Branch: c.Master}}
	}
	return []MergeStep{
		{Name: c.Release, Branch: c.Release},
		{Name: cmp.Or(c.Master, "master"), Branch: c.Master},
	}
}

func (s *Session) MergeFlow() []MergeStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mergeFlowLocked()
}

func (s *Session) SetMergeFlow(flow []MergeStep) error {
	if len(flow) == 0 {
		return errors.New("merge flow: at least one target is required")
	}
	next := slices.Clone(flow)
	names, branches := map[string]bool{}, map[string]bool{}
	for index := range next {
		step := &next[index]
		step.Branch = strings.TrimSpace(step.Branch)
		step.Name = cmp.Or(strings.TrimSpace(step.Name), step.Branch)
		if step.Name == "" || strings.ContainsAny(step.Name+step.Branch, "\x00\r\n") {
			return fmt.Errorf("merge flow: target %d needs a name and a valid branch", index+1)
		}
		if names[step.Name] || branches[step.Branch] {
			return errors.New("merge flow: duplicate target")
		}
		names[step.Name], branches[step.Branch] = true, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.cfg.MergeFlow
	s.cfg.MergeFlow = next
	if err := s.saveConfig(); err != nil {
		s.cfg.MergeFlow = previous
		return err
	}
	return nil
}

func (s *Session) SetRepoBaselineOnly(repo string, only bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.cfg.BaselineOnly
	next := slices.DeleteFunc(slices.Clone(previous), func(name string) bool { return name == repo })
	if only {
		next = append(next, repo)
	}
	s.cfg.BaselineOnly = next
	if err := s.saveConfig(); err != nil {
		s.cfg.BaselineOnly = previous
		return err
	}
	return nil
}

func (s *Session) repoBaselineOnly(repo string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.cfg.BaselineOnly, repo)
}

func MergeStage(index int) string { return "merge:" + strconv.Itoa(index) }

func MergeStageIndex(stage string) (int, bool) {
	raw, ok := strings.CutPrefix(stage, "merge:")
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(raw)
	return index, err == nil && index >= 0
}

func StageRank(stage string) int {
	switch stage {
	case StageDev:
		return 0
	case StageReading:
		return 1
	case StageUnknown:
		return 2
	case StageLanded:
		return int(^uint(0)>>1) - 1
	case StageArchived:
		return int(^uint(0) >> 1)
	}
	if index, ok := MergeStageIndex(stage); ok {
		return index + 3
	}
	return int(^uint(0) >> 1)
}

func StageOrder(flow []MergeStep) []string {
	order := []string{StageDev, StageReading, StageUnknown}
	for index := range flow {
		order = append(order, MergeStage(index))
	}
	return append(order, StageLanded, StageArchived)
}

func (s *Session) ArchiveRequirement(id string, archived bool) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(s.cfg.Tracked, id) {
		return errors.New("archive: requirement is not tracked")
	}
	previous := s.cfg.Archived
	next := slices.DeleteFunc(slices.Clone(previous), func(value string) bool { return value == id })
	if archived {
		next = append(next, id)
	}
	s.cfg.Archived = next
	if err := s.saveConfig(); err != nil {
		s.cfg.Archived = previous
		return err
	}
	return nil
}
