// Package gitcli is the only place that runs git, and only the subcommands in its whitelist (record.go). Reads never take
// optional locks, so they never rewrite the user's index file; the few writes (KindWrite) run only through Write. Each run is
// recorded when the context carries a Recorder.
package gitcli

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

// Oid is a SHA-1 object id.
type Oid [20]byte

func ParseOid(s string) (Oid, error) {
	var o Oid
	if len(s) != 40 {
		return o, fmt.Errorf("bad object id %q", s)
	}
	_, err := hex.Decode(o[:], []byte(s))
	return o, err
}

func (o Oid) String() string { return hex.EncodeToString(o[:]) }
func (o Oid) Short() string  { return o.String()[:7] }
func (o Oid) IsZero() bool   { return o == Oid{} }

// FixedEnv is what every command gets on top of the user's environment.
func FixedEnv() []string {
	return []string{"LC_ALL=C", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat"}
}

func env() []string { return append(os.Environ(), FixedEnv()...) }

func fullArgs(dir string, args []string) []string {
	full := append([]string{"--no-optional-locks", "-c", "core.quotePath=false", "-c", "color.ui=false"}, args...)
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	return full
}

// CommandLine is the command Cmd runs, shell-quoted with its environment, to paste into a terminal.
func CommandLine(dir string, args []string) string {
	parts := append(FixedEnv(), "git")
	for _, a := range fullArgs(dir, args) {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_=./:,@+^~") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Cmd builds a git command run in dir. Every call goes through here; a write fails unless it comes from Write.
func Cmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return cmd(ctx, dir, false, args)
}

func cmd(ctx context.Context, dir string, write bool, args []string) *exec.Cmd {
	c := exec.CommandContext(ctx, "git", fullArgs(dir, args)...)
	c.Env = env()
	switch k := KindOf(args); {
	case k == "":
		c.Err = fmt.Errorf("git %s: not a command Sheaf runs", strings.Join(args, " "))
	case (k == KindWrite) != write:
		c.Err = fmt.Errorf("git %s: a write runs only through gitcli.Write", strings.Join(args, " "))
	}
	return c
}

type ignoreSpaceKey struct{}

// IgnoreSpace makes the patch functions of this package (ShowPatch, DiffPatch, DiffHeadPatch, DiffMergeBasePatch,
// DiffMergeBaseWorktree) pass -w: a view option of one request, so it rides on the context instead of widening every signature.
func IgnoreSpace(ctx context.Context, on bool) context.Context {
	if !on {
		return ctx
	}
	return context.WithValue(ctx, ignoreSpaceKey{}, true)
}

// patchArgs is the start of a git diff / show command line that produces a patch.
func patchArgs(ctx context.Context, cmd string, more ...string) []string {
	args := append([]string{cmd}, "-M", "--no-ext-diff", "--no-textconv")
	if ctx.Value(ignoreSpaceKey{}) != nil {
		args = append(args, "-w")
	}
	return append(args, more...)
}

// Output runs a read and returns stdout; stderr is folded into the error.
func Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return run(ctx, dir, false, args)
}

// Write runs a command that changes the repository (KindWrite in the whitelist) and returns stdout; stderr is folded into the
// error, which wraps the *exec.ExitError.
func Write(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return run(ctx, dir, true, args)
}

func run(ctx context.Context, dir string, write bool, args []string) ([]byte, error) {
	release, err := acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	defer release()
	c := cmd(ctx, dir, write, args)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	var ev *Event
	rec := recorderOf(ctx)
	if rec != nil {
		ev = rec.begin(ctx, dir, args, "")
	}
	out, err := c.Output()
	if rec != nil {
		rec.end(ctx, ev, int64(len(out)), string(out[:min(len(out), maxCapture+utf8.UTFMax)]), stderr.String(), err)
	}
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// stream runs git with stdin, handing stdout to read.
func stream(ctx context.Context, dir string, stdin string, read func(io.Reader) error, args ...string) (err error) {
	release, err := acquire(ctx)
	if err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	defer release()
	c := Cmd(ctx, dir, args...)
	c.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	pipe, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	out := &counter{r: pipe}
	if rec := recorderOf(ctx); rec != nil {
		ev := rec.begin(ctx, dir, args, stdin)
		defer func() { rec.end(ctx, ev, out.n, string(out.head), stderr.String(), err) }()
	}
	if err := c.Start(); err != nil {
		return err
	}
	rerr := read(out)
	if rerr != nil {
		io.Copy(io.Discard, out) // let git finish instead of blocking on a full pipe
	}
	werr := c.Wait()
	if rerr != nil {
		return rerr
	}
	if werr != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), werr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// counter counts the bytes read through it and keeps the first maxCapture of them (for the command log).
type counter struct {
	r    io.Reader
	n    int64
	head []byte
}

func (c *counter) Read(p []byte) (int, error) {
	k, err := c.r.Read(p)
	if room := maxCapture - len(c.head); room > 0 {
		c.head = append(c.head, p[:min(k, room)]...)
	}
	c.n += int64(k)
	return k, err
}

// ErrNotRepo is returned when a directory is not a git working tree.
var ErrNotRepo = errors.New("not a git repository")

// unquote undoes git's C-style quoting of unusual paths ("a\tb").
func unquote(p string) string {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		if s, err := unquoteC(p[1 : len(p)-1]); err == nil {
			return s
		}
	}
	return p
}

func unquoteC(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", errors.New("trailing backslash")
		}
		switch s[i] {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'v':
			b.WriteByte('\v')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case '0', '1', '2', '3':
			if i+2 >= len(s) {
				return "", errors.New("short octal")
			}
			b.WriteByte((s[i]-'0')<<6 | (s[i+1]-'0')<<3 | (s[i+2] - '0'))
			i += 2
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}
