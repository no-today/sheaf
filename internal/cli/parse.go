package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type options struct {
	workspace, command, action, id, file                  string
	json, fetch, help, all, archived, full, rescan, files bool
	search, repo, cursor, stage                           string
	limit                                                 int
	topic                                                 string
	version                                               bool
}

func parse(args []string) (o options, err error) {
	defer func() {
		if err != nil {
			err = argumentError(o.command, err)
		}
	}()
	fs := flag.NewFlagSet("sheaf", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, option := range allOptions() {
		switch option.name {
		case "workspace":
			fs.StringVar(&o.workspace, option.name, "", option.description)
		case "search":
			fs.StringVar(&o.search, option.name, "", option.description)
		case "repo":
			fs.StringVar(&o.repo, option.name, "", option.description)
		case "cursor":
			fs.StringVar(&o.cursor, option.name, "", option.description)
		case "stage":
			fs.StringVar(&o.stage, option.name, "", option.description)
		case "limit":
			fs.IntVar(&o.limit, option.name, 50, option.description)
		default:
			values := map[string]*bool{"json": &o.json, "rescan": &o.rescan, "fetch": &o.fetch,
				"help": &o.help, "version": &o.version, "all": &o.all, "archived": &o.archived, "full": &o.full, "files": &o.files}
			fs.BoolVar(values[option.name], option.name, false, option.description)
		}
	}
	fs.BoolVar(&o.help, "h", false, "help")
	// flag stops at the first operand; keep its parsing and validation while allowing trailing options.
	var flags, operands []string
	var flagErr error
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			operands = append(operands, arg)
			continue
		}
		name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		f := fs.Lookup(name)
		if f == nil {
			if flagErr == nil {
				flagErr = fmt.Errorf("unknown option %s", arg)
			}
			continue
		}
		flags = append(flags, arg)
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); (!ok || !b.IsBoolFlag()) && !hasValue {
			if i+1 == len(args) || startsOption(args[i+1]) {
				flagErr = fmt.Errorf("%s needs a value", arg)
				continue
			}
			i++
			flags = append(flags, args[i])
		}
	}
	if len(operands) > 0 {
		o.command = operands[0]
	}
	if flagErr != nil {
		return o, flagErr
	}
	if err := fs.Parse(flags); err != nil {
		return o, err
	}
	if len(operands) == 0 {
		args = nil
		if o.version {
			o.command = "version"
		} else {
			o.help = true
			return o, nil
		}
	} else {
		args = operands[1:]
	}
	c := findCommand(o.command)
	if c == nil {
		return o, fmt.Errorf("unknown command %q; available commands: %s", o.command, commandNames())
	}
	var optionErr error
	fs.Visit(func(f *flag.Flag) {
		if !c.allows(f.Name) {
			optionErr = fmt.Errorf("--%s is not supported by %s", f.Name, o.command)
		}
	})
	if optionErr != nil {
		return o, optionErr
	}
	if o.help {
		o.topic = o.command
		return o, nil
	}
	if o.all && o.archived {
		return o, fmt.Errorf("--all and --archived are mutually exclusive")
	}
	valid := false
	switch o.command {
	case "status", "version", "workspaces":
		valid = len(args) == 0
	case "help":
		valid = len(args) <= 1
		if len(args) == 1 {
			o.topic = args[0]
			if o.topic != "json" && findCommand(o.topic) == nil {
				return o, fmt.Errorf("unknown help topic %q; available topics: %s, json", o.topic, commandNames())
			}
		}
		o.help = true
	case "check":
		valid = len(args) <= 1
		if len(args) == 1 {
			o.id = args[0]
		}
	case "show", "commits", "track", "untrack", "archive", "unarchive":
		valid = len(args) == 1
		if valid {
			o.id = args[0]
		}
	case "completion":
		valid = len(args) == 1 && (args[0] == "zsh" || args[0] == "bash")
		if valid {
			o.action = args[0]
		}
	case "config":
		if len(args) > 0 {
			o.action = args[0]
		}
		valid = o.action == "show" && len(args) == 1 || o.action == "apply" && len(args) == 2
		if valid && o.action == "apply" {
			o.file = args[1]
		}
	}
	if !valid {
		return o, fmt.Errorf("invalid arguments for %s", o.command)
	}
	if o.limit <= 0 {
		return o, fmt.Errorf("--limit must be positive")
	}
	return o, nil
}

func argumentError(command string, err error) error {
	if c := findCommand(command); c != nil {
		return fmt.Errorf("%w\nUsage: sheaf %s\nRun 'sheaf help %s' for details.", err, c.usage, c.name)
	}
	return fmt.Errorf("%w\n%s\nRun 'sheaf help' for details.", err, rootUsage)
}

func startsOption(arg string) bool {
	return strings.HasPrefix(arg, "--") || arg == "-h"
}

// jsonRequested also works when parsing failed before reaching --json.
func jsonRequested(args []string) bool {
	requested := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		name, value, equal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if strings.HasPrefix(arg, "-") && name == "json" {
			requested = !equal || value != "false"
		}
		if !equal {
			switch name {
			case "workspace", "search", "repo", "limit", "cursor", "stage":
				if i+1 < len(args) && !startsOption(args[i+1]) {
					i++
				}
			}
		}
	}
	return requested
}
