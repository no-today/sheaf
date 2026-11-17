package session

import (
	"encoding/json"
	"os"
	"path/filepath"

	"sheaf/internal/discover"
)

// loadScan reads the last scan, so the repo list shows before the next scan finishes. A missing or unreadable file is no list.
func loadScan(path string) ([]discover.Repo, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var repos []discover.Repo
	if json.Unmarshal(b, &repos) != nil || len(repos) == 0 {
		return nil, false
	}
	return repos, true
}

// saveScan keeps the scan for the next start; failing to is not worth failing a refresh for.
func saveScan(path string, repos []discover.Repo) {
	b, err := json.Marshal(repos)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, path)
	}
}
