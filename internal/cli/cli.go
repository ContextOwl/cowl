// Package cli implements the cowl command-line client for the public REST
// API (/api/v1). It talks only HTTP: no store, mcp, or server imports, so the
// binary stays small and free of database dependencies.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Version is stamped at build time via
// -ldflags "-X github.com/ContextOwl/cowl/internal/cli.Version=v1.2.3".
var Version = "dev"

const defaultBaseURL = "https://contextowl.co"

// IO carries the process environment so tests can run Main hermetically.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	Env func(string) string
	TTY bool
}

// Command is one CLI verb. OpIDs names the REST operations it drives. Local
// commands (version, completion) need no config or network, so a
// missing config dir or corrupt config file must not break them.
type Command struct {
	Group   string // "" for top-level commands (search, api, version, ...)
	Name    string
	OpIDs   []string
	Summary string
	Usage   string
	Local   bool
	Flags   func(fs *flag.FlagSet)
	Run     func(a *App, args []string) error
}

func (c *Command) full() string {
	if c.Group == "" {
		return c.Name
	}
	return c.Group + " " + c.Name
}

type usageError string

func (e usageError) Error() string { return string(e) }

// Main parses args, dispatches, and returns the process exit code:
// 0 ok, 1 API/runtime error, 2 usage error.
func Main(args []string, io IO) int {
	a := &App{IO: io}
	if len(args) == 0 {
		printRootHelp(io.Err)
		return 2
	}
	if h := args[0]; h == "help" || h == "-h" || h == "--help" {
		topic := ""
		if len(args) > 1 {
			topic = args[1]
		}
		if topic == "" {
			printRootHelp(io.Out)
			return 0
		}
		if !printGroupHelp(io.Out, topic) {
			fmt.Fprintf(io.Err, "cowl: unknown help topic: %s\n", topic)
			return 2
		}
		return 0
	}
	// "cowl articles --help" reads as a help request for the group.
	if len(args) > 1 && (args[1] == "-h" || args[1] == "--help" || args[1] == "help") {
		if printGroupHelp(io.Out, args[0]) {
			return 0
		}
	}
	cmd, rest, derr := dispatch(args)
	if derr != nil {
		fmt.Fprintf(io.Err, "cowl: %s\nRun 'cowl help' for usage.\n", derr)
		return 2
	}
	fs := flag.NewFlagSet("cowl "+cmd.full(), flag.ContinueOnError)
	fs.SetOutput(io.Err)
	fs.Usage = func() { printCommandHelp(io.Err, cmd, fs) }
	registerGlobals(fs, &a.g)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	pos, perr := parseInterleaved(fs, rest)
	if perr != nil {
		if errors.Is(perr, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	a.fs = fs
	if !cmd.Local {
		if err := a.resolve(); err != nil {
			fmt.Fprintf(io.Err, "cowl: %s\n", err)
			return 1
		}
	}
	if err := cmd.Run(a, pos); err != nil {
		var u usageError
		if errors.As(err, &u) {
			fmt.Fprintf(io.Err, "cowl: %s\nusage: %s\n", u, cmd.Usage)
			return 2
		}
		fmt.Fprintf(io.Err, "cowl: %s\n", err)
		return 1
	}
	return 0
}

func dispatch(args []string) (*Command, []string, error) {
	cmds := allCommands()
	head := args[0]
	for _, c := range cmds {
		if c.Group == "" && c.Name == head {
			return c, args[1:], nil
		}
	}
	var group []*Command
	for _, c := range cmds {
		if c.Group == head {
			group = append(group, c)
		}
	}
	if len(group) == 0 {
		return nil, nil, usageError("unknown command: " + head)
	}
	if len(args) < 2 {
		return nil, nil, usageError(head + " needs a subcommand: " + subcommandNames(group))
	}
	for _, c := range group {
		if c.Name == args[1] {
			return c, args[2:], nil
		}
	}
	return nil, nil, usageError("unknown command: " + head + " " + args[1] + " (expected " + subcommandNames(group) + ")")
}

func subcommandNames(group []*Command) string {
	names := make([]string, 0, len(group))
	for _, c := range group {
		names = append(names, c.Name)
	}
	return strings.Join(names, "|")
}

// parseInterleaved parses flags allowing positional arguments and flags in
// any order (stdlib flag stops at the first non-flag argument). A bare "--"
// ends flag parsing for good: everything after it is positional, matching
// the usual Unix convention.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		// fs.Parse stops either at the first non-flag or just past a "--"
		// it consumed; in the latter case all of rest is positional.
		if stop := len(args) - len(rest); stop > 0 && args[stop-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func printRootHelp(w io.Writer) {
	fmt.Fprintf(w, "cowl %s — command-line client for the ContextOwl REST API\n\n", Version)
	fmt.Fprint(w, "usage: cowl <command> [subcommand] [flags] [args]\n\ncommands:\n")
	byGroup := map[string][]*Command{}
	for _, c := range allCommands() {
		key := c.Group
		if key == "" {
			key = c.Name
		}
		byGroup[key] = append(byGroup[key], c)
	}
	for _, key := range sortedKeys(byGroup) {
		cmds := byGroup[key]
		if cmds[0].Group == "" {
			fmt.Fprintf(w, "  %-14s %s\n", cmds[0].Name, cmds[0].Summary)
			continue
		}
		fmt.Fprintf(w, "  %-14s %s\n", cmds[0].Group, subcommandNames(cmds))
	}
	fmt.Fprint(w, "\nglobal flags: --base-url URL  --token PAT  -w/--workspace ID  --json  --config PATH\n")
	fmt.Fprint(w, "environment:  COWL_PAT (or CONTEXTOWL_PAT), COWL_BASE_URL, COWL_WORKSPACE, COWL_CONFIG\n")
	fmt.Fprint(w, "\nRun 'cowl help <command>' or 'cowl <command> <subcommand> --help' for details.\n")
}

func printGroupHelp(w io.Writer, group string) bool {
	var cmds []*Command
	for _, c := range allCommands() {
		if c.Group == group || (c.Group == "" && c.Name == group) {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return false
	}
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-56s %s\n", c.Usage, c.Summary)
	}
	return true
}

func printCommandHelp(w io.Writer, c *Command, fs *flag.FlagSet) {
	fmt.Fprintf(w, "%s\n\nusage: %s\n\nflags:\n", c.Summary, c.Usage)
	fs.PrintDefaults()
}

// RESTOpIDs returns every REST operation ID the CLI covers, sorted and
// deduplicated.
func RESTOpIDs() []string {
	seen := map[string]bool{}
	var ids []string
	for _, c := range allCommands() {
		for _, id := range c.OpIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}
