package session

// SetBases replaces every base branch (repo name -> branch); tests set up several repos at once with it.
func (s *Session) SetBases(bases map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Bases = map[string]string{}
	for k, v := range bases {
		if v != "" {
			s.cfg.Bases[k] = v
		}
	}
	return s.saveConfig()
}
