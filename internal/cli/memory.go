package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type memoryRow struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	Tags      []string  `json:"tags"`
	Size      int       `json:"size"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func memoryTable(a *App, rows []memoryRow) {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{
			r.Path, r.Kind, dash(strings.Join(r.Tags, ",")),
			strconv.Itoa(r.Size), timeLabel(r.UpdatedAt),
		})
	}
	a.table([]string{"PATH", "KIND", "TAGS", "SIZE", "UPDATED"}, out)
}

func cmdMemoryList() *Command {
	var prefix, kind string
	return &Command{
		Group: "memory", Name: "list", OpIDs: []string{"listMemory"},
		Summary: "List shared workspace memory notes (the index)",
		Usage:   "cowl memory list [--prefix P] [--kind K]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&prefix, "prefix", "", "only paths starting with this prefix")
			fs.StringVar(&kind, "kind", "", "only this kind (user, feedback, project, reference, note)")
		},
		Run: func(a *App, args []string) error {
			q := url.Values{}
			if prefix != "" {
				q.Set("prefix", prefix)
			}
			if kind != "" {
				q.Set("kind", kind)
			}
			raw, err := a.request("GET", a.ws()+"/memory", q, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var rows []memoryRow
			if err := json.Unmarshal(raw, &rows); err != nil {
				return err
			}
			memoryTable(a, rows)
			return nil
		},
	}
}

func cmdMemorySearch() *Command {
	var limit int
	return &Command{
		Group: "memory", Name: "search", OpIDs: []string{"searchMemory"},
		Summary: "Full-text search shared workspace memory notes",
		Usage:   "cowl memory search QUERY [--limit N]",
		Flags: func(fs *flag.FlagSet) {
			fs.IntVar(&limit, "limit", 10, "max results (max 50)")
		},
		Run: func(a *App, args []string) error {
			query := joinArgs(args)
			if query == "" {
				return usageError("QUERY is required")
			}
			q := url.Values{"q": {query}}
			if a.flagWasSet("limit") {
				q.Set("limit", strconv.Itoa(limit))
			}
			raw, err := a.request("GET", a.ws()+"/memory/search", q, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var resp struct {
				Results []memoryRow `json:"results"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return err
			}
			if len(resp.Results) == 0 {
				fmt.Fprintln(a.Out, "no results")
				return nil
			}
			memoryTable(a, resp.Results)
			return nil
		},
	}
}

func cmdMemoryGet() *Command {
	return &Command{
		Group: "memory", Name: "get", OpIDs: []string{"getMemory"},
		Summary: "Print one memory note's body (--json for the full note)",
		Usage:   "cowl memory get PATH",
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one PATH argument")
			}
			raw, err := a.request("GET", a.ws()+"/memory/note", url.Values{"path": {args[0]}}, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var note struct {
				Body string `json:"body"`
			}
			if err := json.Unmarshal(raw, &note); err != nil {
				return err
			}
			fmt.Fprintln(a.Out, note.Body)
			return nil
		},
	}
}

func cmdMemoryWrite() *Command {
	var opts struct{ kind, tags, links, file, body string }
	return &Command{
		Group: "memory", Name: "write", OpIDs: []string{"writeMemory"},
		Summary: "Create or update a shared workspace memory note",
		Usage:   "cowl memory write PATH [--file FILE|-] [--body TEXT] [--kind K] [--tags a,b] [--links p,q]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.file, "file", "", "note body file, - for stdin")
			fs.StringVar(&opts.body, "body", "", "inline note body")
			fs.StringVar(&opts.kind, "kind", "", "user, feedback, project, reference, or note")
			fs.StringVar(&opts.tags, "tags", "", "comma-separated tags")
			fs.StringVar(&opts.links, "links", "", "comma-separated linked note paths")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one PATH argument")
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.body, a.flagWasSet("body"))
			if err != nil {
				return err
			}
			if !hasContent {
				return usageError("note content is required: pass --file or --body")
			}
			body := map[string]any{"path": args[0], "body": content}
			if opts.kind != "" {
				body["kind"] = opts.kind
			}
			if a.flagWasSet("tags") {
				body["tags"] = csv(opts.tags)
			}
			if a.flagWasSet("links") {
				body["links"] = csv(opts.links)
			}
			raw, err := a.request("PUT", a.ws()+"/memory/note", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "wrote memory note %s\n", args[0])
			return nil
		},
	}
}

func cmdMemoryDelete() *Command {
	var yes bool
	return &Command{
		Group: "memory", Name: "delete", OpIDs: []string{"deleteMemory"},
		Summary: "Delete a shared workspace memory note",
		Usage:   "cowl memory delete PATH [--yes]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one PATH argument")
			}
			if err := a.confirm("delete memory note "+args[0], yes); err != nil {
				return err
			}
			raw, err := a.request("DELETE", a.ws()+"/memory/note", url.Values{"path": {args[0]}}, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "deleted memory note %s\n", args[0])
			return nil
		},
	}
}
