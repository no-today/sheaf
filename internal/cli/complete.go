package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sheaf/internal/appdir"
	"sheaf/internal/session"
)

func allOptions() []optionSpec {
	var out []optionSpec
	seen := map[string]bool{}
	for _, c := range commands {
		for _, option := range c.options {
			if !seen[option.name] {
				seen[option.name] = true
				out = append(out, option)
			}
		}
	}
	return out
}

func globalOptions() []optionSpec {
	out := queryOptions()
	for _, option := range findCommand("version").options {
		if option.name == "version" {
			out = append(out, option)
		}
	}
	return out
}

type completionCandidate struct {
	value, description string
}

type completionRequest struct {
	command, workspace, pending string
	operands                    []string
	current                     string
	literal                     bool
}

// The last word is the word at the cursor, including an empty word after a space.
func parseCompletion(words []string) (completionRequest, bool) {
	var r completionRequest
	if len(words) == 0 {
		return r, false
	}
	r.current = words[len(words)-1]
	options := map[string]optionSpec{}
	for _, option := range allOptions() {
		options[option.name] = option
	}
	var used []string
	for _, word := range words[:len(words)-1] {
		if r.pending != "" {
			if r.pending == "workspace" {
				r.workspace = word
			}
			r.pending = ""
			continue
		}
		if !r.literal && word == "--" {
			r.literal = true
			continue
		}
		if !r.literal && strings.HasPrefix(word, "-") && word != "-" {
			name, value, equal := strings.Cut(strings.TrimLeft(word, "-"), "=")
			if name == "h" {
				name = "help"
			}
			option, ok := options[name]
			if !ok {
				return r, false
			}
			used = append(used, name)
			if option.value != "" {
				if !equal {
					r.pending = name
				} else if name == "workspace" {
					r.workspace = value
				}
			}
			continue
		}
		if r.command == "" {
			r.command = word
		} else {
			r.operands = append(r.operands, word)
		}
	}
	if r.command != "" {
		c := findCommand(r.command)
		if c == nil {
			return r, false
		}
		for _, name := range used {
			if !c.allows(name) {
				return r, false
			}
		}
	}
	return r, true
}

func completionCandidates(name string, words []string) ([]completionCandidate, bool) {
	r, ok := parseCompletion(words)
	if !ok {
		return nil, false
	}
	var out []completionCandidate
	add := func(value, description string) {
		if strings.HasPrefix(value, r.current) && !strings.ContainsAny(value, "\r\n") {
			out = append(out, completionCandidate{value, description})
		}
	}
	if !r.literal && r.pending == "" && strings.HasPrefix(r.current, "--") {
		options := globalOptions()
		if c := findCommand(r.command); c != nil {
			options = c.options
		}
		for _, option := range options {
			add("--"+option.name, option.description)
		}
		return out, false
	}
	if r.pending == "" && !r.literal && strings.HasPrefix(r.current, "-") {
		return nil, false
	}
	if r.pending == "" {
		switch r.command {
		case "", "help":
			if len(r.operands) == 0 {
				for _, c := range commands {
					add(c.name, c.summary)
				}
				if r.command == "help" {
					add("json", "JSON contract")
				}
			}
			return out, false
		case "config":
			if len(r.operands) == 0 {
				add("show", "Show configuration")
				add("apply", "Replace configuration from a file")
			}
			return out, len(r.operands) == 1 && r.operands[0] == "apply"
		case "completion":
			if len(r.operands) == 0 {
				add("zsh", "zsh completion script")
				add("bash", "bash completion script")
			}
			return out, false
		case "show", "commits", "check", "untrack", "archive", "unarchive":
			if len(r.operands) != 0 {
				return nil, false
			}
		default:
			return nil, false
		}
	} else if r.pending != "workspace" && r.pending != "stage" && r.pending != "repo" {
		return nil, false
	}
	root := appdir.Root(appdir.Dev(filepath.Base(name)))
	data, cache := appdir.Data(root), appdir.Cache(root)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, false
	}
	if r.pending == "workspace" {
		items, err := session.KnownWorkspaces(data, cache, cwd)
		if err != nil {
			return nil, false
		}
		for _, item := range items {
			if item.Exists {
				add(item.Root, "")
			}
		}
		return out, false
	}
	workspace := r.workspace
	if workspace == "" {
		matches, err := session.FindWorkspace(data, cache, cwd)
		if err != nil || len(matches) != 1 {
			return nil, false
		}
		workspace = matches[0]
	}
	workspace = session.CanonicalRoot(data, workspace)
	saved, err := session.CompletionData(data, cache, workspace)
	if err != nil {
		return nil, false
	}
	switch r.pending {
	case "stage":
		for _, stage := range saved.Stages {
			add(stage, "")
		}
	case "repo":
		for _, repo := range saved.Repositories {
			add(repo, "")
		}
	default:
		for _, requirement := range saved.Requirements {
			if r.command == "archive" && requirement.Archived || r.command == "unarchive" && !requirement.Archived {
				continue
			}
			add(requirement.ID, requirement.Title)
		}
	}
	return out, false
}

func writeCompletions(name string, words []string, stdout io.Writer) {
	candidates, files := completionCandidates(name, words)
	shell := os.Getenv("SHEAF_COMPLETE_SHELL")
	// Only shell adapters request this control record; direct callers receive candidate lines only.
	if files && (shell == "zsh" || shell == "bash") {
		fmt.Fprintln(stdout, ":__sheaf_files")
		return
	}
	for _, candidate := range candidates {
		if shell == "zsh" {
			escape := strings.NewReplacer("\\", "\\\\", ":", "\\:")
			description := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(candidate.description)
			fmt.Fprintf(stdout, "%s:%s\n", escape.Replace(candidate.value), description)
		} else {
			fmt.Fprintln(stdout, candidate.value)
		}
	}
}

func completionScript(name, shell string) string {
	quoted := "'" + strings.ReplaceAll(name, "'", "'\\''") + "'"
	if shell == "zsh" {
		return `#compdef ` + name + `
# Generated by Sheaf.
_sheaf_complete() {
  local -a request candidates
  request=("${(@)words[2,CURRENT]}")
  request[-1]=$PREFIX
  candidates=("${(@f)$(SHEAF_COMPLETE_SHELL=zsh "${words[1]}" __complete "${request[@]}" 2>/dev/null)}")
  if [[ ${candidates[1]} == :__sheaf_files ]]; then
    _files
  elif [[ -n ${candidates[1]} ]]; then
    _describe 'sheaf' candidates -V sheaf
  fi
}
if [[ $funcstack[1] == _sheaf ]]; then
  _sheaf_complete "$@"
else
  compdef _sheaf_complete ` + quoted + "\nfi\n"
	}
	return `# Generated by Sheaf.
_sheaf_complete() {
  local candidate
  COMPREPLY=()
  while IFS= read -r candidate; do
    if [[ $candidate == :__sheaf_files ]]; then
      while IFS= read -r candidate; do
        COMPREPLY+=("$candidate")
      done < <(compgen -f -- "${COMP_WORDS[COMP_CWORD]}")
      compopt -o filenames 2>/dev/null || :
    else
      COMPREPLY+=("$candidate")
    fi
  done < <(SHEAF_COMPLETE_SHELL=bash "${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
}
complete -F _sheaf_complete ` + quoted + "\n"
}
