package index

import (
	"bufio"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrStale means the snapshot was written by another format version.
var ErrStale = errors.New("index snapshot has an old format")

type header struct {
	Version int
	Strs    []string
	Paths   []string
	Repos   int
}

// Save writes the index atomically, one repo at a time so encoding never
// holds a second copy of the whole index.
func (ix *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	enc := gob.NewEncoder(w)
	err = enc.Encode(header{Version, ix.Strs, ix.Paths, len(ix.Repos)})
	for _, r := range ix.Repos {
		if err != nil {
			break
		}
		err = enc.Encode(r)
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("save index: %w", err)
	}
	return os.Rename(tmp, path)
}

func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := gob.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	var h header
	if err := dec.Decode(&h); err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}
	if h.Version != Version {
		return nil, ErrStale
	}
	ix := &Index{Strs: h.Strs, Paths: h.Paths}
	for k := 0; k < h.Repos; k++ {
		r := &Repo{}
		if err := dec.Decode(r); err != nil {
			return nil, fmt.Errorf("load index: %w", err)
		}
		ix.Repos = append(ix.Repos, r)
	}
	ix.finish()
	return ix, nil
}
