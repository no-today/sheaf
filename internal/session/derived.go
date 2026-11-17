package session

import (
	"encoding/gob"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"sheaf/internal/gitcli"
)

const (
	derivedVersion   = 1
	derivedSaveDelay = 2 * time.Second
	derivedCoverDays = 7
)

type derivedCommit struct {
	Dir string
	Oid gitcli.Oid
}

type derivedCover struct {
	Dir        string
	Line, From gitcli.Oid
}

type derivedData struct {
	Version   int
	Merges    map[derivedCommit]gitcli.MergeChanges
	IDs       map[derivedCommit]string
	Cover     map[derivedCover]bool
	CoverUsed map[derivedCover]int64
}

func readDerived(path string) *derivedData {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var data derivedData
	if gob.NewDecoder(f).Decode(&data) != nil || data.Version != derivedVersion {
		return nil
	}
	if data.CoverUsed == nil {
		data.CoverUsed = map[derivedCover]int64{}
		// Older caches have no per-entry age; their last write starts the retention window.
		if st, err := f.Stat(); err == nil {
			for k := range data.Cover {
				data.CoverUsed[k] = st.ModTime().Unix() / 86400
			}
		}
	}
	return &data
}

func (s *Session) loadDerived() {
	data := readDerived(s.derivedPath)
	if data == nil {
		return
	}
	s.remerge.m = make(map[remergeKey]gitcli.MergeChanges, len(data.Merges))
	for k, v := range data.Merges {
		s.remerge.m[remergeKey{k.Dir, k.Oid}] = v
	}
	s.cover.ids = make(map[idKey]string, len(data.IDs))
	for k, v := range data.IDs {
		s.cover.ids[idKey{k.Dir, k.Oid}] = v
	}
	s.cover.cover = make(map[coverKey]bool, len(data.Cover))
	for k, v := range data.Cover {
		s.cover.cover[coverKey{k.Dir, k.Line, k.From}] = v
	}
	s.derivedSaved = data
}

func (s *Session) scheduleDerived() {
	s.derivedMu.Lock()
	defer s.derivedMu.Unlock()
	if s.derivedClosed || s.derivedTimer != nil {
		return
	}
	// A fixed window also flushes during sustained work; resetting it could postpone saving indefinitely.
	s.derivedTimer = time.AfterFunc(s.derivedDelay, func() {
		s.derivedMu.Lock()
		defer s.derivedMu.Unlock()
		s.derivedTimer = nil
		if !s.derivedClosed {
			s.saveDerivedLocked()
		}
	})
}

func (s *Session) saveDerived() {
	s.derivedMu.Lock()
	defer s.derivedMu.Unlock()
	s.saveDerivedLocked()
}

func (s *Session) saveDerivedLocked() {
	day := s.opts.Now().Unix() / 86400
	data := derivedData{Version: derivedVersion,
		Merges: map[derivedCommit]gitcli.MergeChanges{}, IDs: map[derivedCommit]string{},
		Cover: map[derivedCover]bool{}, CoverUsed: map[derivedCover]int64{}}
	s.remerge.mu.Lock()
	for k, v := range s.remerge.m {
		data.Merges[derivedCommit{k.dir, k.oid}] = v
	}
	s.remerge.mu.Unlock()
	s.cover.mu.Lock()
	for k, v := range s.cover.ids {
		data.IDs[derivedCommit{k.dir, k.oid}] = v
	}
	for k := range s.cover.used {
		key := derivedCover{k.dir, k.line, k.from}
		data.Cover[key] = s.cover.cover[k]
		data.CoverUsed[key] = day
	}
	s.cover.mu.Unlock()
	if reflect.DeepEqual(s.derivedSaved, &data) {
		return
	}
	if os.MkdirAll(filepath.Dir(s.derivedPath), 0o755) != nil {
		return
	}
	// Rename prevents partial reads; a separate cache lock prevents concurrent read/union/rename from losing entries.
	lock, err := os.OpenFile(s.derivedPath+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return
	}
	defer lock.Close()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return
	}
	disk := readDerived(s.derivedPath)
	if disk != nil {
		maps.Copy(data.Merges, disk.Merges)
		maps.Copy(data.IDs, disk.IDs)
		for k, v := range disk.Cover {
			used := disk.CoverUsed[k]
			if used >= day-derivedCoverDays && used >= data.CoverUsed[k] {
				data.Cover[k], data.CoverUsed[k] = v, used
			}
		}
	}
	if !reflect.DeepEqual(disk, &data) {
		f, err := os.CreateTemp(filepath.Dir(s.derivedPath), "derived-*.tmp")
		if err != nil {
			return
		}
		defer os.Remove(f.Name())
		err = gob.NewEncoder(f).Encode(data)
		if closeErr := f.Close(); err != nil || closeErr != nil {
			return
		}
		if os.Rename(f.Name(), s.derivedPath) != nil {
			return
		}
	}
	s.derivedSaved = &data
}
