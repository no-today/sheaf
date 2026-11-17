package gitcli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type RawCommit struct {
	Oid     Oid
	Parents []Oid
	Author  string
	Email   string
	Time    int64 // author time, unix seconds
	Subject string
	Files   []FileChange // against the first parent
}

// FileChange is one entry of --name-status.
type FileChange struct {
	Status  byte // A M D R C T
	Path    string
	OldPath string // for R and C
}

const logFormat = "--format=\x1e%H\x1f%P\x1f%an\x1f%ae\x1f%at\x1f%s"

// Log streams commits reachable from include and not from exclude, newest
// first by default order. Merge commits list files against their first parent.
func Log(ctx context.Context, dir string, include, exclude []Oid, fn func(*RawCommit) error) error {
	if len(include) == 0 {
		return nil
	}
	var revs strings.Builder
	for _, o := range include {
		revs.WriteString(o.String())
		revs.WriteByte('\n')
	}
	for _, o := range exclude {
		revs.WriteByte('^')
		revs.WriteString(o.String())
		revs.WriteByte('\n')
	}
	return stream(ctx, dir, revs.String(), func(r io.Reader) error { return parseLog(r, fn) },
		"log", "--stdin", logFormat, "--name-status", "-M", "--diff-merges=first-parent",
		"--no-show-signature", "--no-ext-diff", "--no-textconv")
}

// Count is how many commits to has that from lacks.
func Count(ctx context.Context, dir string, from, to Oid) (int, error) {
	out, err := Output(ctx, dir, "rev-list", "--count", from.String()+".."+to.String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

func parseLog(r io.Reader, fn func(*RawCommit) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var cur *RawCommit
	flush := func() error {
		if cur == nil {
			return nil
		}
		c := cur
		cur = nil
		return fn(c)
	}
	for {
		line, rerr := br.ReadString('\n')
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, "\x1e") {
			if err := flush(); err != nil {
				return err
			}
			c, err := parseHeader(line[1:])
			if err != nil {
				return err
			}
			cur = c
		} else if line != "" && cur != nil {
			if fc, ok := parseNameStatus(line); ok {
				cur.Files = append(cur.Files, fc)
			}
		}
		if rerr == io.EOF {
			return flush()
		}
		if rerr != nil {
			return rerr
		}
	}
}

func parseHeader(s string) (*RawCommit, error) {
	f := strings.SplitN(s, "\x1f", 6)
	if len(f) != 6 {
		return nil, fmt.Errorf("malformed log header %q", s)
	}
	oid, err := ParseOid(f[0])
	if err != nil {
		return nil, err
	}
	c := &RawCommit{Oid: oid, Author: f[2], Email: f[3], Subject: f[5]}
	for _, p := range strings.Fields(f[1]) {
		po, err := ParseOid(p)
		if err != nil {
			return nil, err
		}
		c.Parents = append(c.Parents, po)
	}
	c.Time, _ = strconv.ParseInt(f[4], 10, 64)
	return c, nil
}

// "M\tpath", "R087\told\tnew"
func parseNameStatus(line string) (FileChange, bool) {
	parts := strings.Split(line, "\t")
	if len(parts) < 2 || parts[0] == "" {
		return FileChange{}, false
	}
	fc := FileChange{Status: parts[0][0], Path: unquote(parts[len(parts)-1])}
	if (fc.Status == 'R' || fc.Status == 'C') && len(parts) >= 3 {
		fc.OldPath = unquote(parts[1])
	}
	return fc, true
}
