package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"
)

// jsonOut reports whether table and receipt commands print JSON: with
// --json, and when stdout is not a terminal.
func (a *App) jsonOut() bool {
	return a.g.jsonOut || !a.OutTTY
}

// printJSON writes a JSON body: indented on a terminal, one compact line
// otherwise. A body that is not JSON is written as it is.
func (a *App) printJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	var buf bytes.Buffer
	var err error
	if a.OutTTY {
		err = json.Indent(&buf, raw, "", "  ")
	} else {
		err = json.Compact(&buf, raw)
	}
	if err != nil {
		buf.Reset()
		buf.Write(raw)
	}
	buf.WriteByte('\n')
	_, err = a.Out.Write(buf.Bytes())
	return err
}

// printValue marshals a value that cowl builds itself, such as the doctor
// report, and prints it like a response body.
func (a *App) printValue(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.printJSON(raw)
}

// emit prints the response body as JSON for agents and pipes, or runs human
// to print a table or a receipt on a terminal.
func (a *App) emit(raw []byte, human func() error) error {
	if a.jsonOut() {
		return a.printJSON(raw)
	}
	return human()
}

// emitAs decodes the response into v before it runs human.
func emitAs[T any](a *App, raw []byte, human func(v T) error) error {
	return a.emit(raw, func() error {
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return invalidResponse(err)
		}
		return human(v)
	})
}

// table renders rows with upper-case headers, tab-aligned. Cell values are
// server-controlled; embedded tabs/newlines would corrupt columns, so all
// whitespace runs collapse to single spaces.
func (a *App) table(headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		cells := make([]string, len(r))
		for i, c := range r {
			cells[i] = strings.Join(strings.Fields(c), " ")
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	tw.Flush()
}

// fields prints label and value pairs as aligned lines. Values collapse
// whitespace like table cells.
func (a *App) fields(pairs [][2]string) {
	tw := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	for _, p := range pairs {
		fmt.Fprintf(tw, "%s\t%s\n", p[0], strings.Join(strings.Fields(p[1]), " "))
	}
	tw.Flush()
}

// confirm asks before a destructive action. --yes bypasses; without a TTY it
// refuses so scripts must be explicit.
func (a *App) confirm(what string, yes bool) error {
	if yes {
		return nil
	}
	if !a.TTY {
		return usageError("pass --yes to " + what + ": stdin is not a terminal, so cowl cannot ask")
	}
	fmt.Fprintf(a.Err, "%s? [y/N] ", what)
	line, _ := bufio.NewReader(a.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return &cliError{code: "aborted", message: "you did not confirm, so nothing changed", exit: exitError}
}

// snippetText strips the FTS highlight sentinels (private-use codepoints)
// and squashes whitespace for one-line table cells.
func snippetText(s string) string {
	s = strings.ReplaceAll(s, "", "")
	s = strings.ReplaceAll(s, "", "")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}

func timeLabel(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func timePtrLabel(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return timeLabel(*t)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

// joinJSONArray builds one JSON array from several response bodies.
func joinJSONArray(items []json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(bytes.TrimSpace(it))
	}
	b.WriteByte(']')
	return b.Bytes()
}
