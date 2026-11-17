package gitcli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

// PatchIDs is the stable patch-id (git patch-id --stable) of each non-merge commit of oids. A commit that changes nothing
// has none and is left out of the map; so is a merge.
func PatchIDs(ctx context.Context, dir string, oids []Oid) (map[Oid]string, error) {
	out := make(map[Oid]string, len(oids))
	if len(oids) == 0 {
		return out, nil
	}
	var in strings.Builder
	for _, o := range oids {
		in.WriteString(o.String())
		in.WriteByte('\n')
	}
	release, err := acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("git patch-id: %w", err)
	}
	defer release() // one slot for the pair: they run together and neither waits for a slot
	treeArgs := []string{"diff-tree", "--stdin", "--root", "-p", "--no-ext-diff", "--no-textconv", "--no-renames"}
	idArgs := []string{"patch-id", "--stable"}
	tree, ids := Cmd(ctx, dir, treeArgs...), Cmd(ctx, dir, idArgs...)
	tree.Stdin = strings.NewReader(in.String())
	var treeErr, idErr bytes.Buffer
	tree.Stderr, ids.Stderr = &treeErr, &idErr
	treeOut, err := tree.StdoutPipe()
	if err != nil {
		return nil, err
	}
	ids.Stdin = treeOut
	idsOut, err := ids.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if rec := recorderOf(ctx); rec != nil {
		ev := rec.begin(ctx, dir, treeArgs, in.String())
		defer func() { rec.end(ctx, ev, 0, "", treeErr.String(), err) }()
		ev2 := rec.begin(ctx, dir, idArgs, "")
		defer func() { rec.end(ctx, ev2, int64(len(out)), "", idErr.String(), err) }()
	}
	if err = tree.Start(); err != nil {
		return nil, err
	}
	if err = ids.Start(); err != nil {
		tree.Process.Kill()
		tree.Wait()
		return nil, err
	}
	sc := bufio.NewReader(idsOut)
	for {
		line, rerr := sc.ReadString('\n')
		if f := strings.Fields(line); len(f) == 2 {
			if o, perr := ParseOid(f[1]); perr == nil {
				out[o] = f[0]
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				err = rerr
			}
			break
		}
	}
	if werr := tree.Wait(); werr != nil && err == nil {
		err = fmt.Errorf("git diff-tree: %w: %s", werr, strings.TrimSpace(treeErr.String()))
	}
	if werr := ids.Wait(); werr != nil && err == nil {
		err = fmt.Errorf("git patch-id: %w: %s", werr, strings.TrimSpace(idErr.String()))
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LineCandidates lists the non-merge commits of tip, newest first, that changed one of paths and are not older than since
// (unix seconds). A commit that is a copy of another one touches the same paths and comes no earlier, so these are all a
// patch-equivalent of a commit could be.
func LineCandidates(ctx context.Context, dir string, tip Oid, since int64, paths []string) ([]Oid, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var in strings.Builder
	in.WriteString(tip.String() + "\n--\n")
	for _, p := range paths {
		in.WriteString(":(literal)" + p + "\n")
	}
	var oids []Oid
	err := stream(ctx, dir, in.String(), func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			o, err := ParseOid(strings.TrimSpace(sc.Text()))
			if err != nil {
				return err
			}
			oids = append(oids, o)
		}
		return sc.Err()
	}, "rev-list", "--stdin", "--no-merges", "--full-history", fmt.Sprintf("--max-age=%d", max(since, 1))) // not --since: that reads a bare number as a date
	return oids, err
}
