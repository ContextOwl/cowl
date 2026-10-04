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
	ID              int64      `json:"id"`
	ObjectType      string     `json:"objectType"`
	Status          string     `json:"status"`
	Slug            string     `json:"slug"`
	Title           string     `json:"title"`
	Note            string     `json:"note"`
	ReviewNote      string     `json:"reviewNote"`
	Author          string     `json:"author"`
	CreatedAt       time.Time  `json:"createdAt"`
	ReviewedAt      *time.Time `json:"reviewedAt"`
	Stale           bool       `json:"stale"`
	Markdown        string     `json:"markdown"`
	BaseRevision    string     `json:"baseRevision"`
	CurrentRevision string     `json:"currentRevision"`
}

func cmdProposalsList() *Command {
	var status string
	return &Command{
		Group: "proposals", Name: "list", OpIDs: []string{"listProposals"},
		Summary: "List edit proposals (pending by default)",
		Usage:   "cowl proposals list [--status pending|approved|rejected|all]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&status, "status", "", "pending (default), approved, rejected, or all")
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			q := url.Values{}
			if status != "" {
				q.Set("status", status)
			}
			raw, err := a.request("GET", a.ws()+"/proposals", q, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(rows []proposalRow) error {
				out := make([][]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, []string{
						strconv.FormatInt(r.ID, 10), r.ObjectType, r.Status, dash(r.Slug), dash(r.Title),
						yesNo(r.Stale), dash(r.Author), timeLabel(r.CreatedAt), dash(snippetText(r.Note)),
					})
				}
				a.table([]string{"ID", "TYPE", "STATUS", "SLUG", "TITLE", "STALE", "AUTHOR", "CREATED", "NOTE"}, out)
				return nil
			})
		},
	}
}

func cmdProposalsGet() *Command {
	return &Command{
		Group: "proposals", Name: "get", OpIDs: []string{"listProposals"},
		Summary: "Show one proposal with its proposed Markdown",
		Usage:   "cowl proposals get ID",
		Run: func(a *App, args []string) error {
			id, err := idArg(args)
			if err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/proposals", url.Values{"id": {id}, "status": {"all"}}, nil)
			if err != nil {
				return err
			}
			var rows []proposalRow
			if err := json.Unmarshal(raw, &rows); err != nil {
				return invalidResponse(err)
			}
			if len(rows) == 0 {
				return &cliError{code: "not_found", message: "no proposal " + id + " in this workspace for this key", exit: exitNotFound}
			}
			return a.emit(raw, func() error {
				p := rows[0]
				stale := "no"
				if p.Stale {
					stale = "yes, the article changed after the proposal"
				}
				a.fields([][2]string{
					{"id:", strconv.FormatInt(p.ID, 10)},
					{"type:", p.ObjectType},
					{"status:", p.Status},
					{"slug:", dash(p.Slug)},
					{"title:", dash(p.Title)},
					{"author:", dash(p.Author)},
					{"created:", timeLabel(p.CreatedAt)},
					{"reviewed:", timePtrLabel(p.ReviewedAt)},
					{"stale:", stale},
					{"base revision:", dash(p.BaseRevision)},
					{"current revision:", dash(p.CurrentRevision)},
					{"note:", dash(p.Note)},
					{"review note:", dash(p.ReviewNote)},
				})
				if p.Markdown != "" {
					fmt.Fprintln(a.Out)
					fmt.Fprint(a.Out, p.Markdown)
					if p.Markdown[len(p.Markdown)-1] != '\n' {
						fmt.Fprintln(a.Out)
					}
				}
				return nil
			})
		},
	}
}

func cmdProposalsCreate() *Command {
	var opts struct {
		slug, title, note, file, markdown, edits, baseRevision, sectionKey string
		allowShrink                                                        bool
	}
	return &Command{
		Group: "proposals", Name: "create", OpIDs: []string{"proposeArticleEdit"},
		Summary: "Propose an article edit (--slug) or a new article (--title) for editor review",
		Usage:   "cowl proposals create (--slug SLUG | --title TITLE) (--file FILE|- | --markdown TEXT | --edits FILE|-) [--base-revision REV] [--allow-shrink] [--section-key KEY] [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.slug, "slug", "", "existing article to propose an edit to")
			fs.StringVar(&opts.title, "title", "", "title (required for a new-article proposal)")
			fs.StringVar(&opts.note, "note", "", "note for the reviewer, who did not see your conversation")
			fs.StringVar(&opts.file, "file", "", "proposed markdown body from a file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "proposed inline markdown body")
			fs.StringVar(&opts.edits, "edits", "", `JSON file with 1 to 20 exact text edits [{"old":"...","new":"..."}], - for stdin (needs --slug)`)
			fs.StringVar(&opts.baseRevision, "base-revision", "", "revision you read. The proposal fails with stale_revision when the article changed since then")
			fs.BoolVar(&opts.allowShrink, "allow-shrink", false, "allow a new body that removes more than half of the text")
			fs.StringVar(&opts.sectionKey, "section-key", "", "section key where a new article lands when a reviewer approves it")
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if opts.slug == "" && opts.title == "" {
				return usageError("pass --slug to change an article, or --title to propose a new one")
			}
			if opts.edits != "" && opts.slug == "" {
				return usageError("--edits needs --slug: edits change an existing article")
			}
			if opts.sectionKey != "" && opts.slug != "" {
				return usageError("--section-key is for a new article only: leave out --slug")
			}
			body := map[string]any{}
			if err := a.bodyContent(body, opts.file, opts.markdown, opts.edits); err != nil {
				return err
			}
			if len(body) == 0 {
				return usageError("content is required: pass --file, --markdown or --edits")
			}
			if err := a.revisionFlags(body, opts.baseRevision, opts.allowShrink); err != nil {
				return err
			}
			if opts.slug != "" {
				body["slug"] = opts.slug
			}
			if opts.title != "" {
				body["title"] = opts.title
			}
			if opts.note != "" {
				body["note"] = opts.note
			}
			if opts.sectionKey != "" {
				body["section_key"] = opts.sectionKey
			}
			raw, err := a.request("POST", a.ws()+"/proposals", nil, body)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(ref struct {
				ID        int64  `json:"id"`
				Status    string `json:"status"`
				Slug      string `json:"slug"`
				ReviewURL string `json:"reviewUrl"`
			}) error {
				target := ""
				if ref.Slug != "" {
					target = " for " + ref.Slug
				}
				fmt.Fprintf(a.Out, "created proposal %d (%s)%s%s\n", ref.ID, ref.Status, target, urlSuffix(ref.ReviewURL))
				return nil
			})
		},
	}
}
