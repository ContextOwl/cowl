package cli

import (
	"flag"
	"io"
	"os"
	"strings"
)

// readFileArg reads a --file argument; "-" means stdin.
func (a *App) readFileArg(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(a.In)
	}
	return os.ReadFile(path)
}

// contentFrom resolves body content from --file (or stdin via "-") or an
// inline flag value. inlineSet must come from flagWasSet so an explicit
// empty inline value (--markdown="") still counts as provided, which is
// how a PATCH clears a body.
func (a *App) contentFrom(file, inline string, inlineSet bool) (string, bool, error) {
	if file != "" && inlineSet {
		return "", false, usageError("use either --file or the inline content flag, not both")
	}
	if file != "" {
		data, err := a.readFileArg(file)
		if err != nil {
			return "", false, err
		}
		return string(data), true, nil
	}
	return inline, inlineSet, nil
}

// flagWasSet reports whether the named flag was explicitly passed, so PATCH
// commands only send fields the user asked to change.
func (a *App) flagWasSet(name string) bool {
	set := false
	a.fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// oneArg enforces exactly one positional argument. Multi-word values must be
// quoted; that strictness turns misuses like a stray "false" after a bool
// flag into a loud error instead of a silently mangled value.
func oneArg(args []string, name string) (string, error) {
	if len(args) != 1 {
		return "", usageError("expected exactly one " + name + " argument (quote multi-word values)")
	}
	return args[0], nil
}

// joinArgs turns positional words into one string. Only search-style
// commands use it, where absorbing extra words into the query is harmless.
func joinArgs(args []string) string {
	return strings.TrimSpace(strings.Join(args, " "))
}

func csv(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
