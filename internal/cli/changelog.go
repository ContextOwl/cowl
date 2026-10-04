package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const changelogTagsHelp = "comma-separated tags: new, improved, fixed, deprecated, security. " +
	"The server also takes aliases: added, feature and features mean new. changed, improvement and improvements mean improved. " +
	"fix, fixes and bugfix mean fixed. removed and deprecation mean deprecated"

const changelogPageSize = 100

func cmdChangelogList() *Command {
	var opts struct {
		drafts, all   bool
		limit, offset int
		since         string
	}
	return &Command{
		Group: "changelog", Name: "list", OpIDs: []string{"listChangelog"},
		Summary: "List changelog entries, newest first",
		Usage:   "cowl changelog list [--since 30d] [--limit N] [--offset N] [--all] [--drafts]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&opts.drafts, "drafts", false, "include drafts (needs changelog.update)")
			fs.IntVar(&opts.limit, "limit", 50, "entries per request, 1 to 100")
			fs.IntVar(&opts.offset, "offset", 0, "entries to skip")
			fs.StringVar(&opts.since, "since", "", "only entries published since a time: RFC 3339, YYYY-MM-DD, or a duration such as 30d or 24h")
			fs.BoolVar(&opts.all, "all", false, "get every page until a page is short (pages of --limit, default 100)")
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if opts.limit < 1 || opts.limit > 100 {
				return usageError("--limit must be 1 to 100")
			}
			if opts.offset < 0 {
				return usageError("--offset must be 0 or more")
			}
			q := url.Values{}
			if opts.drafts {
				q.Set("drafts", "true")
			}
			if opts.since != "" {
				q.Set("since", opts.since)
			}
			if a.flagWasSet("limit") {
				q.Set("limit", strconv.Itoa(opts.limit))
			}
			if a.flagWasSet("offset") {
				q.Set("offset", strconv.Itoa(opts.offset))
			}
			var raw json.RawMessage
			var err error
			if opts.all {
				pageSize := changelogPageSize
				if a.flagWasSet("limit") {
					pageSize = opts.limit
				}
				raw, err = a.changelogPages(q, pageSize, opts.offset)
			} else {
				raw, err = a.request("GET", a.ws()+"/changelog", q, nil)
			}
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(rows []changelogEntry) error {
				out := make([][]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, []string{
						strconv.FormatInt(r.ID, 10), r.Title, r.Status,
						dash(strings.Join(r.Tags, ",")), timePtrLabel(r.PublishedAt), dash(r.URL),
					})
				}
				a.table([]string{"ID", "TITLE", "STATUS", "TAGS", "PUBLISHED", "URL"}, out)
				return nil
			})
		},
	}
}

// changelogPages pages listChangelog with limit and offset until a page is
// shorter than pageSize, and joins the pages into one JSON array. A full page
// with no new entry means the server ignores offset, so paging stops there.
func (a *App) changelogPages(q url.Values, pageSize, offset int) (json.RawMessage, error) {
	var all []json.RawMessage
	seen := map[int64]bool{}
	for {
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("offset", strconv.Itoa(offset))
		raw, err := a.request("GET", a.ws()+"/changelog", q, nil)
		if err != nil {
			return nil, err
		}
		var page []json.RawMessage
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, invalidResponse(err)
		}
		fresh := 0
		for _, item := range page {
			var e struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(item, &e); err != nil {
				return nil, invalidResponse(err)
			}
			if !seen[e.ID] {
				seen[e.ID] = true
				fresh++
				all = append(all, item)
			}
		}
		if len(page) < pageSize {
			break
		}
		if fresh == 0 {
			return nil, &cliError{code: "invalid_response", message: "the server sent the same changelog page again, so cowl stopped paging", exit: exitError}
		}
		offset += len(page)
	}
	return joinJSONArray(all), nil
}

func cmdChangelogGet() *Command {
	return &Command{
		Group: "changelog", Name: "get", OpIDs: []string{"getChangelog"},
		Summary: "Print one changelog entry as front matter and Markdown (--json for the API object)",
		Usage:   "cowl changelog get ID",
		Run: func(a *App, args []string) error {
			id, err := idArg(args)
			if err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/changelog/"+id, nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var entry changelogEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				return invalidResponse(err)
			}
			return writeChangelogDoc(a.Out, entry)
		},
	}
}

func changelogReceipt(a *App, verb string, raw []byte) error {
	return emitAs(a, raw, func(e changelogEntry) error {
		fmt.Fprintf(a.Out, "%s changelog entry %d (%s)%s\n", verb, e.ID, dash(e.Status), urlSuffix(e.URL))
		return nil
	})
}

func cmdChangelogCreate() *Command {
	var opts struct{ title, file, markdown, tags, status, publishedAt string }
	return &Command{
		Group: "changelog", Name: "create", OpIDs: []string{"createChangelog"},
		Summary: "Create a changelog entry (publishing needs changelog.publish)",
		Usage:   "cowl changelog create --title TITLE [--file FILE|- | --markdown TEXT] [--tags new,fixed] [--status draft|published] [--published-at RFC3339]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "entry title (required)")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
			fs.StringVar(&opts.tags, "tags", "", changelogTagsHelp)
			fs.StringVar(&opts.status, "status", "", "draft or published")
			fs.StringVar(&opts.publishedAt, "published-at", "", "publish time (RFC 3339). A future time schedules the entry")
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if opts.title == "" {
				return usageError("--title is required")
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			body := map[string]any{"title": opts.title}
			if hasContent {
				body["markdown"] = content
			}
			if a.flagWasSet("tags") {
				body["tags"] = csv(opts.tags)
			}
			if opts.status != "" {
				body["status"] = opts.status
			}
			if opts.publishedAt != "" {
				body["published_at"] = opts.publishedAt
			}
			raw, err := a.request("POST", a.ws()+"/changelog", nil, body)
			if err != nil {
				return err
			}
			return changelogReceipt(a, "created", raw)
		},
	}
}

func cmdChangelogUpdate() *Command {
	var opts struct{ title, file, markdown, tags, status, publishedAt string }
	return &Command{
		Group: "changelog", Name: "update", OpIDs: []string{"updateChangelog"},
		Summary: "Update a changelog entry (changing --status needs changelog.publish)",
		Usage:   "cowl changelog update ID [--title T] [--file FILE|- | --markdown TEXT] [--tags new,fixed] [--status S] [--published-at RFC3339]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "new title")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
			fs.StringVar(&opts.tags, "tags", "", changelogTagsHelp+". Replaces the set")
			fs.StringVar(&opts.status, "status", "", "draft or published")
			fs.StringVar(&opts.publishedAt, "published-at", "", "publish time (RFC 3339). A future time schedules the entry")
		},
		Run: func(a *App, args []string) error {
			id, err := idArg(args)
			if err != nil {
				return err
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			body := map[string]any{}
			if a.flagWasSet("title") {
				body["title"] = opts.title
			}
			if hasContent {
				body["markdown"] = content
			}
			if a.flagWasSet("tags") {
				body["tags"] = csv(opts.tags)
			}
			if a.flagWasSet("status") {
				body["status"] = opts.status
			}
			if a.flagWasSet("published-at") {
				body["published_at"] = opts.publishedAt
			}
			if len(body) == 0 {
				return usageError("nothing to update: pass at least one of --title, --file, --markdown, --tags, --status, --published-at")
			}
			raw, err := a.request("PATCH", a.ws()+"/changelog/"+id, nil, body)
			if err != nil {
				return err
			}
			return changelogReceipt(a, "updated", raw)
		},
	}
}

func cmdChangelogDelete() *Command {
	var yes bool
	return &Command{
		Group: "changelog", Name: "delete", OpIDs: []string{"deleteChangelog"},
		Summary: "Delete a changelog entry",
		Usage:   "cowl changelog delete ID [--yes]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
		},
		Run: func(a *App, args []string) error {
			id, err := idArg(args)
			if err != nil {
				return err
			}
			if err := a.confirm("delete changelog entry "+id, yes); err != nil {
				return err
			}
			raw, err := a.request("DELETE", a.ws()+"/changelog/"+id, nil, nil)
			if err != nil {
				return err
			}
			return a.emit(raw, func() error {
				fmt.Fprintf(a.Out, "deleted changelog entry %s\n", id)
				return nil
			})
		},
	}
}
