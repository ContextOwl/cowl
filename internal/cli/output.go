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

// printJSON pretty-prints a raw API response.
func (a *App) printJSON(raw json.RawMessage) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not JSON (unexpected): emit verbatim.
		fmt.Fprintln(a.Out, strings.TrimSpace(string(raw)))
		return nil
	}
	fmt.Fprintln(a.Out, buf.String())
	return nil
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

// confirm asks before a destructive action. --yes bypasses; without a TTY it
// refuses so scripts must be explicit.
func (a *App) confirm(what string, yes bool) error {
	if yes {
		return nil
	}
	if !a.TTY {
		return usageError("refusing to " + what + " without --yes in a non-interactive session")
	}
	fmt.Fprintf(a.Err, "%s? [y/N] ", what)
	line, _ := bufio.NewReader(a.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return fmt.Errorf("aborted")
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

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
