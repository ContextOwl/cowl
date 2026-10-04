package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// readFileArg reads a --file argument; "-" means stdin.
func (a *App) readFileArg(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(a.In)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, usageError(fmt.Sprintf("cannot read %s: %v", path, unwrapPathError(err)))
	}
	return data, nil
}

func unwrapPathError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
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

// noArgs rejects stray positional arguments, such as a workspace id given
// without -w.
func noArgs(args []string) error {
	if len(args) != 0 {
		return usageError("unexpected argument: " + args[0] + ". This command takes flags only")
	}
	return nil
}

// idArg reads exactly one positive integer ID argument.
func idArg(args []string) (string, error) {
	if len(args) != 1 {
		return "", usageError("expected exactly one ID argument")
	}
	if n, err := strconv.ParseInt(args[0], 10, 64); err != nil || n <= 0 {
		return "", usageError("ID must be a positive integer")
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

type textEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

const (
	maxEdits      = 20
	maxEditOldLen = 4000
)

// readEdits reads --edits, a JSON array of 1 to 20 {"old","new"} objects.
// Each old text must have 1 to 4,000 characters. The server checks that
// each old text occurs exactly once.
func (a *App) readEdits(path string) ([]textEdit, error) {
	data, err := a.readFileArg(path)
	if err != nil {
		return nil, err
	}
	var edits []textEdit
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&edits); err != nil {
		return nil, usageError(`--edits must be a JSON array of {"old","new"} objects: ` + err.Error())
	}
	if dec.More() {
		return nil, usageError("--edits must hold one JSON array and nothing after it")
	}
	if len(edits) == 0 || len(edits) > maxEdits {
		return nil, usageError(fmt.Sprintf("--edits must hold 1 to %d edits, got %d", maxEdits, len(edits)))
	}
	for i, e := range edits {
		if n := utf8.RuneCountInString(e.Old); n == 0 || n > maxEditOldLen {
			return nil, usageError(fmt.Sprintf("edits[%d].old must be 1 to %d characters, got %d", i, maxEditOldLen, n))
		}
	}
	return edits, nil
}

// revisionArg checks a --base-revision value: 12 to 64 hex characters.
func revisionArg(rev string) (string, error) {
	rev = strings.ToLower(strings.TrimSpace(rev))
	if len(rev) < 12 || len(rev) > 64 || strings.Trim(rev, "0123456789abcdef") != "" {
		return "", usageError("--base-revision must be 12 to 64 hex characters, as in the revision line of 'cowl articles get'")
	}
	return rev, nil
}

// bodyContent adds the article body to a request: markdown from --file or
// --markdown, or exact text edits from --edits, never both.
func (a *App) bodyContent(body map[string]any, file, markdown, edits string) error {
	if edits != "" && (file != "" || a.flagWasSet("markdown")) {
		return usageError("use --edits or a full body from --file or --markdown, not both")
	}
	if edits != "" {
		list, err := a.readEdits(edits)
		if err != nil {
			return err
		}
		body["edits"] = list
		return nil
	}
	content, hasContent, err := a.contentFrom(file, markdown, a.flagWasSet("markdown"))
	if err != nil {
		return err
	}
	if hasContent {
		body["markdown"] = content
	}
	return nil
}

// revisionFlags adds --base-revision and --allow-shrink to a request body.
func (a *App) revisionFlags(body map[string]any, baseRevision string, allowShrink bool) error {
	if a.flagWasSet("base-revision") {
		rev, err := revisionArg(baseRevision)
		if err != nil {
			return err
		}
		body["base_revision"] = rev
	}
	if allowShrink {
		body["allow_shrink"] = true
	}
	return nil
}
