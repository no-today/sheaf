package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo"

	"sheaf/internal/appdir"
	"sheaf/internal/native/settingswin"
)

var cliToolTarget = "/usr/local/bin/sheaf"
var cliCompletionTarget = "/usr/local/share/zsh/site-functions/_sheaf"
var cliToolPrivileged = runCLIToolPrivileged

type cliToolState int

const (
	cliToolAbsent cliToolState = iota
	cliToolOurs
	cliToolForeign
)

type cliToolLink struct {
	source, target string
}

func inspectCLITool(target string) (cliToolState, string, error) {
	st, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return cliToolAbsent, "", nil
	}
	if err != nil {
		return cliToolAbsent, "", fmt.Errorf("inspect command line tool: %w", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return cliToolForeign, "", nil
	}
	dest, err := os.Readlink(target)
	if err != nil {
		return cliToolForeign, "", fmt.Errorf("read command line tool link: %w", err)
	}
	resolved := resolveCLITool(target, dest)
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(resolved)))
	name := filepath.Base(target)
	if (name == "sheaf" || name == "_sheaf") && filepath.Base(bundle) == "Sheaf.app" && resolved == filepath.Join(bundle, "Contents", "Resources", name) {
		return cliToolOurs, dest, nil
	}
	return cliToolForeign, dest, nil
}

func resolveCLITool(target, dest string) string {
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(target), dest)
	}
	return filepath.Clean(dest)
}

func bundledCLITool() []cliToolLink {
	if appdir.Dev(mygo.App.Name()) {
		return nil
	}
	resources, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return nil
	}
	return []cliToolLink{
		{filepath.Join(resources, "sheaf"), cliToolTarget},
		{filepath.Join(resources, "_sheaf"), cliCompletionTarget},
	}
}

func commandLineToolState(links []cliToolLink) settingswin.CLITool {
	tool := settingswin.CLITool{Path: cliToolTarget}
	if len(links) == 0 {
		return tool
	}
	tool.Available, tool.Installed = true, true
	for _, link := range links {
		st, err := os.Lstat(link.source)
		if err != nil || !st.Mode().IsRegular() {
			tool.Available, tool.Installed = false, false
			return tool
		}
		state, dest, err := inspectCLITool(link.target)
		if err != nil || state != cliToolOurs || resolveCLITool(link.target, dest) != filepath.Clean(link.source) {
			tool.Installed = false
		}
	}
	return tool
}

func foreignCLITool(target string) error {
	return &os.PathError{Op: "change command line tool", Path: target, Err: settingswin.ErrForeignCLITool}
}

func changeCLITool(ctx context.Context, links []cliToolLink, install bool) (bool, error) {
	if install {
		for _, link := range links {
			if _, err := os.Lstat(link.source); err != nil {
				return false, fmt.Errorf("inspect bundled command line tool: %w", err)
			}
		}
	}
	before, err := inspectCLIToolLinks(links, install)
	if err != nil {
		return false, err
	}
	var attempted []int
	for i, link := range links {
		if !install && before[i] == "" {
			continue
		}
		attempted = append(attempted, i)
		if err := writeCLITool(link.source, link.target, install); err != nil {
			// Restore a partial direct attempt before a password prompt can be cancelled.
			if rollbackErr := restoreCLIToolLinks(links, before, attempted); rollbackErr != nil {
				return false, fmt.Errorf("restore command line tool: %w", errors.Join(err, rollbackErr))
			}
			if !errors.Is(err, os.ErrPermission) {
				return false, err
			}
			dests, err := inspectCLIToolLinks(links, install)
			if err != nil {
				return false, err
			}
			cancelled, err := cliToolPrivileged(ctx, cliToolLinksShellCommand(links, dests, install))
			if err != nil {
				return false, err
			}
			return !cancelled, nil
		}
	}
	return len(attempted) != 0, nil
}

func inspectCLIToolLinks(links []cliToolLink, install bool) ([]string, error) {
	dests := make([]string, len(links))
	for i, link := range links {
		state, dest, err := inspectCLITool(link.target)
		if err != nil {
			return nil, err
		}
		if state == cliToolForeign && install {
			return nil, foreignCLITool(link.target)
		}
		if state == cliToolOurs {
			dests[i] = dest
		}
	}
	return dests, nil
}

func restoreCLIToolLinks(links []cliToolLink, before []string, attempted []int) error {
	for j := len(attempted) - 1; j >= 0; j-- {
		i := attempted[j]
		link := links[i]
		state, dest, err := inspectCLITool(link.target)
		if err != nil {
			return err
		}
		if (state == cliToolAbsent && before[i] == "") || (state == cliToolOurs && dest == before[i]) {
			continue
		}
		if state == cliToolForeign || (state == cliToolOurs && dest != link.source) {
			return foreignCLITool(link.target)
		}
		if state == cliToolOurs {
			if err := os.Remove(link.target); err != nil {
				return fmt.Errorf("remove partial command line tool link: %w", err)
			}
		}
		if before[i] != "" {
			if err := os.Symlink(before[i], link.target); err != nil {
				return fmt.Errorf("restore command line tool link: %w", err)
			}
		}
	}
	return nil
}

func writeCLITool(source, target string, install bool) error {
	if install {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create command line tool directory: %w", err)
		}
	}
	state, _, err := inspectCLITool(target)
	if err != nil {
		return err
	}
	if state == cliToolForeign {
		if install {
			return foreignCLITool(target)
		}
		return nil
	}
	if state == cliToolOurs {
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("remove command line tool link: %w", err)
		}
	}
	if install {
		if err := os.Symlink(source, target); err != nil {
			return fmt.Errorf("link command line tool: %w", err)
		}
	}
	return nil
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
}

func cliToolLinkGuard(target, dest string) string {
	qtarget := shellQuote(target)
	if dest != "" {
		// A privileged prompt gives another process time to replace the link.
		return "[ -L " + qtarget + " ] && [ \"$(/usr/bin/readlink " + qtarget + ")\" = " + shellQuote(dest) + " ]"
	}
	return "[ ! -e " + qtarget + " ] && [ ! -L " + qtarget + " ]"
}

func cliToolLinksShellCommand(links []cliToolLink, dests []string, install bool) string {
	var guards, commands []string
	for i, link := range links {
		if !install && dests[i] == "" {
			continue
		}
		guards = append(guards, "("+cliToolLinkGuard(link.target, dests[i])+")")
		commands = append(commands, cliToolShellCommand(link.source, link.target, dests[i], install))
	}
	if len(commands) == 0 {
		return ":"
	}
	return "if " + strings.Join(guards, " && ") + "; then " + strings.Join(commands, " && ") + "; else exit 1; fi"
}

func cliToolShellCommand(source, target, dest string, install bool) string {
	qtarget := shellQuote(target)
	command := "if " + cliToolLinkGuard(target, dest) + "; then "
	if dest != "" {
		command += "/bin/rm -- " + qtarget + " && "
	}
	if install {
		command += "/bin/ln -s -- " + shellQuote(source) + " " + qtarget
	} else {
		command += ":"
	}
	command += "; else exit 1; fi"
	if install {
		command = "/bin/mkdir -p -- " + shellQuote(filepath.Dir(target)) + " && " + command
	}
	return command
}

func cliToolAppleScript(command string) string {
	return "do shell script " + strconv.Quote(command) + " with administrator privileges"
}

func runCLIToolPrivileged(ctx context.Context, command string) (bool, error) {
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", cliToolAppleScript(command)).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "(-128)") {
			return true, nil
		}
		return false, fmt.Errorf("install or remove command line tool (%s): %w", strings.TrimSpace(string(out)), err)
	}
	return false, nil
}
