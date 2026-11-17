package gitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const ToolGh = "gh"

var ghCommands = map[string]Kind{
	"pr list":     KindRead,
	"pr create":   KindWrite,
	"auth status": KindRead,
}

var ghFlags = map[string][]string{
	"pr list":     {"--repo=", "--base=", "--head=", "--state=open", "--json=url,headRefName,baseRefName"},
	"pr create":   {"--repo=", "--base=", "--head=", "--fill"},
	"auth status": {"--hostname=github.com"},
}

const ghTimeout = time.Minute

// An explicit head disables gh's implicit push/fork path. Reject duplicate flags so a later value cannot override it.
func ghKindOf(args []string) Kind {
	if len(args) < 2 {
		return ""
	}
	name := args[0] + " " + args[1]
	allowed, ok := ghFlags[name]
	if !ok {
		return ""
	}
	seen := map[string]string{}
	for _, a := range args[2:] {
		if !glabFlagOK(a, allowed) {
			return ""
		}
		key, value, _ := strings.Cut(a, "=")
		if _, duplicate := seen[key]; duplicate {
			return ""
		}
		seen[key] = value
	}
	if strings.HasPrefix(name, "pr ") {
		parts := strings.Split(seen["--repo"], "/")
		if len(parts) != 3 || !strings.EqualFold(parts[0], "github.com") || parts[1] == "" || parts[2] == "" {
			return ""
		}
		if name == "pr create" && (strings.TrimSpace(seen["--head"]) == "" || strings.TrimSpace(seen["--base"]) == "") {
			return ""
		}
	}
	return ghCommands[name]
}

var ErrNoGh = errors.New("gh is not installed")
var ErrGhAuth = errors.New("gh is not logged in")
var ErrNoPRAddress = errors.New("no pull request address in gh's output")

func GhPath() (string, error) {
	return findGh(append(filepath.SplitList(os.Getenv("PATH")), "/opt/homebrew/bin", "/usr/local/bin"))
}

func HasGh() bool {
	_, err := GhPath()
	return err == nil
}

func findGh(dirs []string) (string, error) {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, "gh")
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", ErrNoGh
}

func GhEnv() []string { return []string{"GH_PROMPT_DISABLED=1", "NO_COLOR=1"} }

func GhLine(dir string, args []string) string {
	parts := append(GhEnv(), "gh")
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	line := strings.Join(parts, " ")
	if dir != "" {
		line = "cd " + shellQuote(dir) + " && " + line
	}
	return line
}

func ghRead(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGh(ctx, dir, false, args)
}

func ghWrite(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGh(ctx, dir, true, args)
}

func runGh(ctx context.Context, dir string, write bool, args []string) ([]byte, error) {
	name := "gh " + strings.Join(args, " ")
	switch k := ghKindOf(args); {
	case k == "":
		return nil, fmt.Errorf("%s: not a command Sheaf runs", name)
	case (k == KindWrite) != write:
		return nil, fmt.Errorf("%s: a write runs only through ghWrite", name)
	}
	path, err := GhPath()
	if err != nil {
		return nil, err
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	release, err := acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	c := exec.CommandContext(ctx, path, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), GhEnv()...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	rec := recorderOf(ctx)
	var ev *Event
	if rec != nil {
		ev = rec.add(ctx, &Event{Tool: ToolGh, Dir: dir, Args: args, Kind: ghKindOf(args)})
	}
	out, err := c.Output()
	if rec != nil {
		rec.end(ctx, ev, int64(len(out)), string(out[:min(len(out), maxCapture+utf8.UTFMax)]), stderr.String(), err)
	}
	release()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		text := strings.ToLower(msg + "\n" + string(out))
		var exit *exec.ExitError
		auth := notLoggedIn(text) || strings.Contains(text, "http 401") || strings.Contains(text, "bad credentials") || (errors.As(err, &exit) && exit.ExitCode() == 4)
		if !auth && args[0] == "pr" && (strings.Contains(text, "http 404") || strings.Contains(text, "could not resolve to a repository")) {
			_, authErr := ghRead(parent, dir, "auth", "status", "--hostname=github.com")
			auth = errors.Is(authErr, ErrGhAuth)
		}
		if auth {
			return out, fmt.Errorf("%s: %w: %s", name, ErrGhAuth, msg)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return out, fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return out, nil
}

func GhPRFind(ctx context.Context, dir, project, branch, target string) (string, error) {
	out, err := ghRead(ctx, dir, "pr", "list", "--repo="+project, "--head="+branch, "--base="+target, "--state=open", "--json=url,headRefName,baseRefName")
	if err != nil {
		return "", err
	}
	var prs []struct {
		URL  string `json:"url"`
		Head string `json:"headRefName"`
		Base string `json:"baseRefName"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &prs); err != nil {
		return "", fmt.Errorf("gh pr list: cannot read %q: %w", strings.TrimSpace(string(out)), err)
	}
	for _, pr := range prs {
		if pr.Head == branch && pr.Base == target && pr.URL != "" {
			return pr.URL, nil
		}
	}
	return "", nil
}

var prURL = regexp.MustCompile(`https://github\.com/[^/\s]+/[^/\s]+/pull/\d+`)

// The branch must already be on origin; only gitcli.Push may publish it.
func GhPRCreate(ctx context.Context, dir, project, branch, target string) (string, error) {
	out, err := ghWrite(ctx, dir, "pr", "create", "--repo="+project, "--base="+target, "--head="+branch, "--fill")
	if err != nil {
		return "", err
	}
	urls := prURL.FindAllString(string(out), -1)
	if len(urls) == 0 {
		return "", fmt.Errorf("gh pr create: %w: %q", ErrNoPRAddress, strings.TrimSpace(string(out)))
	}
	return urls[len(urls)-1], nil
}
