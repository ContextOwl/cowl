// Package cli implements the cowl command-line client for the public REST
// API (/api/v1). It talks only HTTP: no store, mcp, or server imports, so the
// binary stays small and free of database dependencies.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"sort"
	"strings"
)

// Version is stamped at build time via
// -ldflags "-X github.com/ContextOwl/cowl/internal/cli.Version=v1.2.3".
// Without it, version reads the module version from the build info.
var Version = "dev"

var readBuildInfo = debug.ReadBuildInfo

const defaultBaseURL = "https://contextowl.co"

// IO carries the process environment so tests can run Main hermetically.
type IO struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Env    func(string) string
	TTY    bool // stdin is a terminal, so cowl can prompt
	OutTTY bool // stdout is a terminal: tables and prose instead of JSON
	ErrTTY bool // stderr is a terminal: text errors instead of JSON lines
}

// Command is one CLI verb. OpIDs names the REST operations it drives.
// Main loads the config and requires a trusted key before Run. Local
// commands skip both and load what they need themselves, so a broken config
// cannot stop version, completion, doctor or auth login. NoKey commands load
// the config but run without a key. auth status and auth logout are NoKey
// commands.
type Command struct {
	Group   string // "" for top-level commands (search, api, version, ...)
	Name    string
	OpIDs   []string
	Summary string
	Usage   string
	Local   bool
	NoKey   bool
	Flags   func(fs *flag.FlagSet)
	Run     func(a *App, args []string) error
}

func (c *Command) full() string {
	if c.Group == "" {
		return c.Name
	}
	return c.Group + " " + c.Name
}

// flagSet builds the parser for c with the global flags. It prints nothing:
// Main reports parse errors and help itself.
func (c *Command) flagSet(g *globals) *flag.FlagSet {
	fs := flag.NewFlagSet("cowl "+c.full(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	registerGlobals(fs, g)
	if c.Flags != nil {
		c.Flags(fs)
	}
	return fs
}

// Main parses args, dispatches, and returns the process exit code. See the
// exit* constants for the meaning of each code.
func Main(args []string, io IO) int {
	a := &App{IO: io}
	if len(args) == 0 {
		if io.ErrTTY {
			printRootHelp(io.Err)
			return exitUsage
		}
		return a.fail(usageError("no command given. Run 'cowl help' for the command list"), "")
	}
	if h := args[0]; h == "help" || h == "-h" || h == "--help" {
		if len(args) < 2 || args[1] == "" {
			printRootHelp(io.Out)
			return exitOK
		}
		if !printGroupHelp(io.Out, args[1]) {
			return a.fail(usageError("unknown help topic: "+args[1]), "")
		}
		return exitOK
	}
	// "cowl articles --help" reads as a help request for the group.
	if len(args) > 1 && (args[1] == "-h" || args[1] == "--help" || args[1] == "help") {
		if printGroupHelp(io.Out, args[0]) {
			return exitOK
		}
	}
	cmd, rest, derr := dispatch(args)
	if derr != nil {
		return a.fail(derr, "")
	}
	fs := cmd.flagSet(&a.g)
	pos, perr := parseInterleaved(fs, rest)
	if perr != nil {
		if errors.Is(perr, flag.ErrHelp) {
			printCommandHelp(io.Out, cmd, fs)
			return exitOK
		}
		return a.fail(usageError(perr.Error()), cmd.Usage)
	}
	a.fs = fs
	if !cmd.Local {
		if err := a.resolve(); err != nil {
			return a.fail(err, "")
		}
		if !cmd.NoKey {
			if err := a.checkKey(); err != nil {
				return a.fail(err, "")
			}
		}
	}
	if err := cmd.Run(a, pos); err != nil {
		return a.fail(err, cmd.Usage)
	}
	return exitOK
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
	fmt.Fprintf(w, "cowl %s: command-line client for the ContextOwl REST API\n\n", version())
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
	fmt.Fprint(w, `
global flags: -w, --workspace ID  --json  --config PATH
environment:  CONTEXTOWL_PAT, CONTEXTOWL_WORKSPACE, CONTEXTOWL_BASE_URL, CONTEXTOWL_CONFIG
              The COWL_* names of these variables also work.

When stdout is not a terminal, or with --json, a command that prints a table
or a receipt prints one line of JSON. articles get, changelog get and
openapi spec print their content, and JSON only with --json. When stderr is
not a terminal, an error is one JSON line: {"error":{"code","message","status"}}.

exit codes:
  0  success
  1  other error
  2  usage error, or HTTP 400, 413 or 422
  3  HTTP 404
  4  no key, untrusted host, or HTTP 401, 402 or 403
  5  HTTP 409
  6  HTTP 429 after one retry
  7  HTTP 5xx or a network failure

Run 'cowl help <command>' or 'cowl <command> <subcommand> --help' for details.
`)
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
	fs.SetOutput(w)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)
}

// version is the release version from -ldflags, else the module version
// that go install records, else the VCS revision of a local build.
func version() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	info, ok := readBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return "dev-" + rev
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
