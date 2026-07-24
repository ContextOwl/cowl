package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type proposalRow struct {
	ID         int64     `json:"id"`
	ObjectType string    `json:"objectType"`
	Note       string    `json:"note"`
	Status     string    `json:"status"`
	Author     string    `json:"author"`
	CreatedAt  time.Time `json:"createdAt"`
}

func cmdProposalsList() *Command {
	var status string
	return &Command{
		Group: "proposals", Name: "list", OpIDs: []string{"listProposals"},
		Summary: "List edit proposals",
		Usage:   "cowl proposals list [--status pending|approved|rejected|all]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&status, "status", "", "pending (default), approved, rejected, or all")
		},
		Run: func(a *App, args []string) error {
			q := url.Values{}
			if status != "" {
				q.Set("status", status)
			}
			raw, err := a.request("GET", a.ws()+"/proposals", q, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var rows []proposalRow
			if err := json.Unmarshal(raw, &rows); err != nil {
				return err
			}
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				out = append(out, []string{
					strconv.FormatInt(r.ID, 10), r.ObjectType, r.Status,
					dash(r.Author), dash(snippetText(r.Note)), timeLabel(r.CreatedAt),
				})
			}
			a.table([]string{"ID", "TYPE", "STATUS", "AUTHOR", "NOTE", "CREATED"}, out)
			return nil
		},
	}
}

func cmdProposalsCreate() *Command {
	var opts struct{ slug, title, note, file, markdown string }
	return &Command{
		Group: "proposals", Name: "create", OpIDs: []string{"proposeArticleEdit"},
		Summary: "Propose an article edit (--slug) or a new draft (--title) for editor review",
		Usage:   "cowl proposals create [--slug SLUG | --title TITLE] --file FILE|- [--markdown TEXT] [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.slug, "slug", "", "existing article to propose an edit to")
			fs.StringVar(&opts.title, "title", "", "title (required for a new-article proposal)")
			fs.StringVar(&opts.note, "note", "", "note for the reviewer")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
		},
		Run: func(a *App, args []string) error {
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			if !hasContent {
				return usageError("markdown content is required: pass --file or --markdown")
			}
			body := map[string]any{"markdown": content}
			if opts.slug != "" {
				body["slug"] = opts.slug
			}
			if opts.title != "" {
				body["title"] = opts.title
			}
			if opts.note != "" {
				body["note"] = opts.note
			}
			raw, err := a.request("POST", a.ws()+"/proposals", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var ref struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal(raw, &ref); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "created proposal %d (%s)\n", ref.ID, ref.Status)
			return nil
		},
	}
}
