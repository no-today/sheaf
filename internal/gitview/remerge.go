package gitview

import (
	"context"
	"fmt"

	"sheaf/internal/gitcli"
	"sheaf/internal/index"
)

// remergeBatch bounds the merges of one git call, so the command line stays short on a repo with thousands of them.
const remergeBatch = 300

// RemergeFiles classifies merges for review and stage ownership (see gitcli.RemergeFiles).
func RemergeFiles(ctx context.Context, r *index.Repo, merges []int32) (map[int32]gitcli.MergeChanges, error) {
	out := make(map[int32]gitcli.MergeChanges, len(merges))
	for start := 0; start < len(merges); start += remergeBatch {
		batch := merges[start:min(len(merges), start+remergeBatch)]
		oids := make([]gitcli.Oid, len(batch))
		byOid := make(map[gitcli.Oid]int32, len(batch))
		for k, c := range batch {
			oids[k] = r.Oid(c)
			byOid[oids[k]] = c
		}
		got, err := gitcli.RemergeFiles(ctx, r.Dir, oids)
		if err != nil {
			return nil, fmt.Errorf("remerge in %s: %w", r.Name, err)
		}
		for o, files := range got {
			if c, ok := byOid[o]; ok {
				out[c] = files
			}
		}
	}
	return out, nil
}

// RemergePatch is what merge commit i changed in path beyond merging its parents again.
func RemergePatch(ctx context.Context, r *index.Repo, i int32, path string) (gitcli.Patch, error) {
	return gitcli.RemergePatch(ctx, r.Dir, r.Oid(i), path)
}
