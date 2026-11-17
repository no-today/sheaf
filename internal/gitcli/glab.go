package gitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// glab is GitLab's command line for 创建 MR. It is not git, but it is the other program Sheaf starts against the
// user's repositories, so it lives here under the same rules: a whitelist (glabCommands, flags in glabFlags), writes only through
// glabWrite, every run recorded (Event.Tool = ToolGlab), and a time limit on each run.

// The programs an Event can be.
const (
	ToolGit  = "" // the zero value: every event before glab was git
	ToolGlab = "glab"
)

// glabCommands is every glab command Sheaf runs (first two words) and its kind.
var glabCommands = map[string]Kind{
	"mr list":     KindRead,
	"mr create":   KindWrite,
	"auth status": KindRead, // only to tell "not logged in" from a project that really is not there (404)
}

// glabFlags are the only arguments each command may have, as flag=value prefixes or whole flags. Both mr commands must have
// --repo= (glabKindOf): without it glab picks the project from whichever remote it likes, not origin. mr create must also have
// --push=false: with --fill glab pushes by itself otherwise, past the git whitelist.
var glabFlags = map[string][]string{
	"mr list":     {"--repo=", "--source-branch=", "--target-branch=", "--output=json"},
	"mr create":   {"--repo=", "--source-branch=", "--target-branch=", "--fill", "--yes", "--push=false"},
	"auth status": {"--hostname="},
}

// glabTimeout bounds one glab run: it talks to the GitLab server and must not hang the sheet.
const glabTimeout = time.Minute

// glabKindOf is the kind of a glab command line; "" when it is not one Sheaf runs.
func glabKindOf(args []string) Kind {
	if len(args) < 2 {
		return ""
	}
	name := args[0] + " " + args[1]
	allowed, ok := glabFlags[name]
	if !ok {
		return ""
	}
	for _, a := range args[2:] {
		if !glabFlagOK(a, allowed) {
			return ""
		}
	}
	if name == "mr create" && !slices.Contains(args, "--push=false") {
		return ""
	}
	if strings.HasPrefix(name, "mr ") && !slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--repo=") && len(a) > len("--repo=") }) {
		return ""
	}
	return glabCommands[name]
}

func glabFlagOK(a string, allowed []string) bool {
	for _, f := range allowed {
		if value, ok := strings.CutPrefix(a, f); ok && (value == "") != strings.HasSuffix(f, "=") {
			return true
		}
	}
	return false
}

// ErrNoGlab: glab is not installed (or not where a GUI app can find it).
var ErrNoGlab = errors.New("glab is not installed")

// ErrGlabAuth: glab is not logged in to the repo's GitLab host.
var ErrGlabAuth = errors.New("glab is not logged in")

// glabDirs are looked in after PATH: an app started from the Finder has only /usr/bin:/bin:/usr/sbin:/sbin.
var glabDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// GlabPath is where glab is.
func GlabPath() (string, error) {
	return findGlab(append(filepath.SplitList(os.Getenv("PATH")), glabDirs...))
}

func findGlab(dirs []string) (string, error) {
	for _, d := range dirs {
		p := filepath.Join(d, "glab")
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", ErrNoGlab
}

// GlabEnv is what every glab run gets on top of the user's environment (which is passed through, so a GITLAB_TOKEN there
// still works, and never recorded).
func GlabEnv() []string { return []string{"GLAB_NO_PROMPT=1", "NO_COLOR=1"} }

// GlabLine is the command a glab run is, with its directory and environment, to paste into a terminal.
func GlabLine(dir string, args []string) string {
	parts := append(GlabEnv(), "glab")
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	line := strings.Join(parts, " ")
	if dir != "" {
		line = "cd " + shellQuote(dir) + " && " + line
	}
	return line
}

type glabConfigKey struct{}

// WithGlabConfig makes the glab commands run with the returned context use dir as their GLAB_CONFIG_DIR (the workspace's own
// logins); "" leaves glab to the system's configuration. It travels on the context, like the Recorder, so
// nothing here is package-level state.
func WithGlabConfig(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, glabConfigKey{}, dir)
}

func glabConfigOf(ctx context.Context) string {
	d, _ := ctx.Value(glabConfigKey{}).(string)
	return d
}

// ErrGlabConfig: the glab configuration directory of the workspace cannot be used.
var ErrGlabConfig = errors.New("glab config directory")

// ResolveGlabConfig is the directory a workspace's setting names: "" stays "" (the system's glab configuration), ~ is the
// home directory, and anything else must be an existing absolute path: a typo must not fall back to the system login silently.
func ResolveGlabConfig(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w %q: %v", ErrGlabConfig, dir, err)
		}
		dir = filepath.Join(home, dir[1:])
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w %q: not an absolute path", ErrGlabConfig, dir)
	}
	st, err := os.Stat(dir)
	switch {
	case err != nil:
		return "", fmt.Errorf("%w %q: does not exist; create it and run glab auth login with GLAB_CONFIG_DIR set to it", ErrGlabConfig, dir)
	case !st.IsDir():
		return "", fmt.Errorf("%w %q: not a directory", ErrGlabConfig, dir)
	}
	return filepath.Clean(dir), nil
}

func glabRead(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGlab(ctx, dir, false, args)
}

func glabWrite(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGlab(ctx, dir, true, args)
}

func runGlab(ctx context.Context, dir string, write bool, args []string) ([]byte, error) {
	name := "glab " + strings.Join(args, " ")
	switch k := glabKindOf(args); {
	case k == "":
		return nil, fmt.Errorf("%s: not a command Sheaf runs", name)
	case (k == KindWrite) != write:
		return nil, fmt.Errorf("%s: a write runs only through glabWrite", name)
	}
	path, err := GlabPath()
	if err != nil {
		return nil, err
	}
	cfgDir, err := ResolveGlabConfig(glabConfigOf(ctx))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, glabTimeout)
	defer cancel()
	release, err := acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	c := exec.CommandContext(ctx, path, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), GlabEnv()...)
	if cfgDir != "" {
		c.Env = append(c.Env, "GLAB_CONFIG_DIR="+cfgDir) // not recorded, only that it was set (Event.GlabConfig)
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	var ev *Event
	rec := recorderOf(ctx)
	if rec != nil {
		ev = rec.add(ctx, &Event{Tool: ToolGlab, Dir: dir, Args: args, Kind: glabKindOf(args), GlabConfig: cfgDir != ""})
	}
	out, err := c.Output()
	if rec != nil {
		rec.end(ctx, ev, int64(len(out)), string(out[:min(len(out), maxCapture+utf8.UTFMax)]), stderr.String(), err)
	}
	release() // glabLoggedIn below runs glab again and needs the slot
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if notLoggedIn(msg+"\n"+string(out)) || (projectNotFound(msg+"\n"+string(out)) && !glabLoggedIn(parent, dir, args)) {
			return out, fmt.Errorf("%s: %w: %s", name, ErrGlabAuth, msg)
		}
		return out, fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return out, nil
}

// glabLoggedIn: glab has a login for the host of the --repo= project in args. GitLab answers 404 for a private project to
// someone not logged in, so a 404 alone says nothing; `auth status` does. Unknown host: assumed logged in (the 404 stands).
func glabLoggedIn(ctx context.Context, dir string, args []string) bool {
	for _, a := range args {
		if repo, ok := strings.CutPrefix(a, "--repo="); ok {
			host, _, _ := strings.Cut(repo, "/")
			_, err := glabRead(ctx, dir, "auth", "status", "--hostname="+host)
			return err == nil
		}
	}
	return true
}

func projectNotFound(s string) bool {
	return strings.Contains(strings.ToLower(s), "404 project not found")
}

// notLoggedIn tells glab's "not logged in" from other failures by its own words; a bare "401" is not one (a branch called
// YT-14012 or "!401" can be in glab's output too).
func notLoggedIn(s string) bool {
	s = strings.ToLower(s)
	for _, w := range []string{"401 unauthorized", "auth login", "not logged in"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// GlabMRFind is the web address of an open merge request from branch into target, "" when there is none (one into another
// target is not it).
func GlabMRFind(ctx context.Context, dir, project, branch, target string) (string, error) {
	out, err := glabRead(ctx, dir, "mr", "list", "--repo="+project, "--source-branch="+branch, "--target-branch="+target, "--output=json")
	if err != nil {
		return "", err
	}
	var mrs []struct {
		WebURL       string `json:"web_url"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &mrs); err != nil {
		return "", fmt.Errorf("glab mr list: cannot read %q: %w", strings.TrimSpace(string(out)), err)
	}
	for _, m := range mrs {
		if m.SourceBranch == branch && m.TargetBranch == target && m.WebURL != "" {
			return m.WebURL, nil
		}
	}
	return "", nil
}

// The "/-/" is absent on GitLab before 11.
var mrURL = regexp.MustCompile(`https?://\S+/merge_requests/\d+`)

// ErrNoMRAddress: glab created (or may have created) the merge request but its stdout has no address of it.
var ErrNoMRAddress = errors.New("no merge request address in glab's output")

// GlabMRCreate opens a merge request from branch (already on the remote) into target, its title and description from the
// branch's commits; returns its web address. --push=false: the push, when needed, is Push's.
func GlabMRCreate(ctx context.Context, dir, project, branch, target string) (string, error) {
	out, err := glabWrite(ctx, dir, "mr", "create", "--repo="+project, "--source-branch="+branch, "--target-branch="+target, "--fill", "--yes", "--push=false")
	if err != nil {
		return "", err
	}
	urls := mrURL.FindAllString(string(out), -1)
	if len(urls) == 0 {
		return "", fmt.Errorf("glab mr create: %w: %q", ErrNoMRAddress, strings.TrimSpace(string(out)))
	}
	return urls[len(urls)-1], nil
}

// HasGlab reports whether glab is installed (where GlabPath finds it).
func HasGlab() bool {
	_, err := GlabPath()
	return err == nil
}

// ErrNoProject: origin's URL does not name a GitLab project.
var ErrNoProject = errors.New("cannot tell the GitLab project from origin's URL")

var scpRemote = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):(.+)$`)

// GlabProject is the project glab is told to use (--repo): origin's, as host/group/sub/proj. Taken from origin on purpose, so a
// repo with several remotes never gets the merge request on another one. Credentials and the ssh port are dropped.
func GlabProject(ctx context.Context, dir string) (string, error) {
	out, err := Output(ctx, dir, "config", "remote.origin.url")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoProject, err)
	}
	if p := ProjectOf(strings.TrimSpace(string(out))); p != "" {
		return p, nil
	}
	return "", ErrNoProject
}

// ProjectOf is host/group/proj of a remote URL (http, https, ssh, git or scp-like), "" when it has none. The port stays for
// http(s), where it is the web's; for ssh it is not.
func ProjectOf(remote string) string {
	remote = strings.TrimSpace(remote)
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" {
			return ""
		}
		host, path = u.Hostname(), u.Path
		if (u.Scheme == "http" || u.Scheme == "https") && u.Port() != "" {
			host += ":" + u.Port()
		}
	} else if m := scpRemote.FindStringSubmatch(remote); m != nil {
		host, path = m[1], m[2]
	} else {
		return ""
	}
	path = strings.Trim(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if !strings.Contains(path, "/") {
		return ""
	}
	return host + "/" + path
}
