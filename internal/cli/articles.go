package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
)

// validArticleStatuses mirrors store.ValidStatuses; the REST endpoint stores
// the value unvalidated, so the CLI guards the vocabulary itself.
var validArticleStatuses = map[string]bool{
	"DRAFT": true, "IN REVIEW": true, "BETA": true, "STABLE": true, "DEPRECATED": true,
}

var validArticleVisibilities = map[string]bool{
	"public": true, "internal": true, "private": true,
}

type articleRow struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Section   string `json:"section"`
	Nav       string `json:"nav"`
	Status    string `json:"status"`
	Encrypted bool   `json:"encrypted"`
}

func cmdArticlesList() *Command {
	return &Command{
		Group: "articles", Name: "list", OpIDs: []string{"listArticles"},
		Summary: "List a workspace's articles",
		Usage:   "cowl articles list [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			raw, err := a.request("GET", a.ws()+"/articles", nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var rows []articleRow
			if err := json.Unmarshal(raw, &rows); err != nil {
				return err
			}
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				enc := ""
				if r.Encrypted {
					enc = "yes"
				}
				out = append(out, []string{r.Slug, r.Title, dash(r.Section), dash(r.Nav), r.Status, enc})
			}
			a.table([]string{"SLUG", "TITLE", "SECTION", "NAV", "STATUS", "ENCRYPTED"}, out)
			return nil
		},
	}
}

func cmdArticlesGet() *Command {
	return &Command{
		Group: "articles", Name: "get", OpIDs: []string{"getArticle"},
		Summary: "Print one article's markdown (--json for the full object)",
		Usage:   "cowl articles get SLUG [-w WORKSPACE] [--json]",
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one SLUG argument")
			}
			raw, err := a.request("GET", a.ws()+"/articles/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var art struct {
				Markdown string `json:"markdown"`
			}
			if err := json.Unmarshal(raw, &art); err != nil {
				return err
			}
			fmt.Fprintln(a.Out, art.Markdown)
			return nil
		},
	}
}

func cmdArticlesCreate() *Command {
	var opts struct{ title, section, file, markdown string }
	return &Command{
		Group: "articles", Name: "create", OpIDs: []string{"createArticle"},
		Summary: "Create a draft article",
		Usage:   "cowl articles create --title TITLE [--section SECTION] [--file FILE|-] [--markdown TEXT]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "article title (required)")
			fs.StringVar(&opts.section, "section", "", "nav section label")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
		},
		Run: func(a *App, args []string) error {
			if opts.title == "" {
				return usageError("--title is required")
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			body := map[string]any{"title": opts.title}
			if opts.section != "" {
				body["section"] = opts.section
			}
			if hasContent {
				body["markdown"] = content
			}
			raw, err := a.request("POST", a.ws()+"/articles", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var ref struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(raw, &ref); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "created article %s\n", ref.Slug)
			return nil
		},
	}
}

func cmdArticlesUpdate() *Command {
	var opts struct{ title, section, status, visibility, file, markdown string }
	return &Command{
		Group: "articles", Name: "update", OpIDs: []string{"updateArticle"},
		Summary: "Update an article (changing --status needs article.publish)",
		Usage:   "cowl articles update SLUG [--title T] [--section S] [--status STATUS] [--visibility TIER] [--file FILE|-] [--markdown TEXT]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "new title")
			fs.StringVar(&opts.section, "section", "", "new nav section label")
			fs.StringVar(&opts.status, "status", "", "DRAFT, IN REVIEW, BETA, STABLE, or DEPRECATED")
			fs.StringVar(&opts.visibility, "visibility", "", "public, internal, or private")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one SLUG argument")
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			body := map[string]any{}
			if a.flagWasSet("title") {
				body["title"] = opts.title
			}
			if a.flagWasSet("section") {
				body["section"] = opts.section
			}
			if a.flagWasSet("status") {
				if !validArticleStatuses[opts.status] {
					return usageError("--status must be one of DRAFT, IN REVIEW, BETA, STABLE, DEPRECATED")
				}
				body["status"] = opts.status
			}
			if a.flagWasSet("visibility") {
				if !validArticleVisibilities[opts.visibility] {
					return usageError("--visibility must be public, internal, or private")
				}
				body["visibility"] = opts.visibility
			}
			if hasContent {
				body["markdown"] = content
			}
			if len(body) == 0 {
				return usageError("nothing to update: pass at least one of --title, --section, --status, --visibility, --file, --markdown")
			}
			raw, err := a.request("PATCH", a.ws()+"/articles/"+url.PathEscape(args[0]), nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "updated article %s\n", args[0])
			return nil
		},
	}
}

func cmdArticlesPlace() *Command {
	var section string
	return &Command{
		Group: "articles", Name: "place", OpIDs: []string{"placeArticle"},
		Summary: "Move an article into a nav section",
		Usage:   "cowl articles place SLUG --section SECTION_KEY",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&section, "section", "", "target section key, or none to unfile (required)")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one SLUG argument")
			}
			if section == "" {
				return usageError("--section is required")
			}
			raw, err := a.request("POST", a.ws()+"/articles/"+url.PathEscape(args[0])+"/placement", nil, map[string]any{"section": section})
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "placed article %s in %s\n", args[0], section)
			return nil
		},
	}
}

func cmdSectionsCreate() *Command {
	return &Command{
		Group: "sections", Name: "create", OpIDs: []string{"createSection"},
		Summary: "Create a nav section",
		Usage:   "cowl sections create LABEL",
		Run: func(a *App, args []string) error {
			label, err := oneArg(args, "LABEL")
			if err != nil {
				return err
			}
			raw, err := a.request("POST", a.ws()+"/sections", nil, map[string]any{"label": label})
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var ref struct {
				Key   string `json:"key"`
				Label string `json:"label"`
			}
			if err := json.Unmarshal(raw, &ref); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "created section %s (%q)\n", ref.Key, ref.Label)
			return nil
		},
	}
}
