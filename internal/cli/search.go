package cli

import (
	"flag"
	"fmt"
	"net/url"
	"slices"
	"strconv"
)

type searchResponse struct {
	Semantic    bool             `json:"semantic"`
	Results     []searchHit      `json:"results"`
	Suggestions []slugSuggestion `json:"suggestions"`
}

type searchHit struct {
	Type    string   `json:"type"`
	Slug    string   `json:"slug"`
	ID      int64    `json:"id"`
	Title   string   `json:"title"`
	Status  string   `json:"status"`
	Snippet string   `json:"snippet"`
	URL     string   `json:"url"`
	Score   *float64 `json:"score"`
	// Learned is true when people and agents opened the article after
	// similar searches, so it ranks higher. The API leaves it out when false.
	Learned bool `json:"learned"`
}

type slugSuggestion struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ref is the slug of an article hit, or the id of a changelog hit.
func (h searchHit) ref() string {
	if h.Slug != "" {
		return h.Slug
	}
	if h.ID != 0 {
		return strconv.FormatInt(h.ID, 10)
	}
	return "-"
}

func cmdSearch() *Command {
	var semantic, publishedOnly bool
	var limit int
	return &Command{
		Name: "search", OpIDs: []string{"searchDocs"},
		Summary: "Search a workspace's articles and changelog (full-text, or --semantic)",
		Usage:   "cowl search QUERY [--limit N] [--semantic] [--published-only]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&semantic, "semantic", false, "rank by meaning. Without embeddings on the server, cowl gets full-text results")
			fs.IntVar(&limit, "limit", 10, "max results, 1 to 50")
			fs.BoolVar(&publishedOnly, "published-only", false, "leave out DRAFT and IN REVIEW articles")
		},
		Run: func(a *App, args []string) error {
			query := joinArgs(args)
			if query == "" {
				return usageError("QUERY is required")
			}
			if limit < 1 || limit > 50 {
				return usageError("--limit must be 1 to 50")
			}
			q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
			if semantic {
				q.Set("semantic", "true")
			}
			if publishedOnly {
				q.Set("published_only", "true")
			}
			raw, err := a.request("GET", a.ws()+"/search", q, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(resp searchResponse) error {
				if len(resp.Results) == 0 {
					fmt.Fprintf(a.Out, "no results for %q\n", query)
					if len(resp.Suggestions) > 0 {
						fmt.Fprintln(a.Out, "\nclose titles:")
						rows := make([][]string, 0, len(resp.Suggestions))
						for _, s := range resp.Suggestions {
							rows = append(rows, []string{s.Slug, s.Title, dash(s.URL)})
						}
						a.table([]string{"SLUG", "TITLE", "URL"}, rows)
					}
					return nil
				}
				headers := []string{"SLUG/ID", "TYPE", "STATUS", "TITLE", "SNIPPET", "URL"}
				if resp.Semantic {
					headers = append(headers, "SCORE")
				}
				learned := slices.ContainsFunc(resp.Results, func(h searchHit) bool { return h.Learned })
				if learned {
					headers = append(headers, "LEARNED")
				}
				rows := make([][]string, 0, len(resp.Results))
				for _, r := range resp.Results {
					row := []string{r.ref(), r.Type, dash(r.Status), r.Title, dash(snippetText(r.Snippet)), dash(r.URL)}
					if resp.Semantic {
						score := "-"
						if r.Score != nil {
							score = strconv.FormatFloat(*r.Score, 'f', 3, 64)
						}
						row = append(row, score)
					}
					if learned {
						row = append(row, yesNo(r.Learned))
					}
					rows = append(rows, row)
				}
				a.table(headers, rows)
				if semantic && !resp.Semantic {
					fmt.Fprintln(a.Err, "note: semantic ranking is not available, so these are full-text results")
				}
				return nil
			})
		},
	}
}
