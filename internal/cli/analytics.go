package cli

import (
	"flag"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// reportTotals sums the report period. The 4 NotFound fields after NotFound
// split it by who asked. They are nil when the server does not split it.
type reportTotals struct {
	Reads            int  `json:"reads"`
	Readers          int  `json:"readers"`
	AgentReads       int  `json:"agentReads"`
	AgentSearches    int  `json:"agentSearches"`
	CrawlerHits      int  `json:"crawlerHits"`
	Searches         int  `json:"searches"`
	SearchNoResults  int  `json:"searchNoResults"`
	NotFound         int  `json:"notFound"`
	NotFoundPeople   *int `json:"notFoundPeople"`
	NotFoundAgents   *int `json:"notFoundAgents"`
	NotFoundCrawlers *int `json:"notFoundCrawlers"`
	NotFoundOther    *int `json:"notFoundOther"`
}

// notFoundSplit returns the not-found counts by who asked, or "" when the
// server does not split them.
func (t reportTotals) notFoundSplit() string {
	if t.NotFoundPeople == nil || t.NotFoundAgents == nil || t.NotFoundCrawlers == nil || t.NotFoundOther == nil {
		return ""
	}
	return fmt.Sprintf(" (people %d, AI agents %d, crawlers %d, other %d)",
		*t.NotFoundPeople, *t.NotFoundAgents, *t.NotFoundCrawlers, *t.NotFoundOther)
}

// whoMissed counts the requests for one path that answered not found, by who
// asked. A server that splits the counts sends crawlers and other, and then
// agents counts AI agents only. Other counts integrations and scripts. An
// older server sends only people and agents, and agents counts every caller
// that is not a person.
type whoMissed struct {
	People   int  `json:"people"`
	Agents   int  `json:"agents"`
	Crawlers *int `json:"crawlers"`
	Other    *int `json:"other"`
}

// splitMissed reports whether the server split the not-found counts of rows.
func splitMissed(rows []whoMissed) bool {
	return slices.ContainsFunc(rows, func(w whoMissed) bool { return w.Crawlers != nil || w.Other != nil })
}

// missedHeaders returns the column headers of the not-found counts.
func missedHeaders(split bool) []string {
	if split {
		return []string{"PEOPLE", "AI AGENTS", "CRAWLERS", "OTHER"}
	}
	return []string{"PEOPLE", "AGENTS"}
}

// cells returns the not-found counts in the order of missedHeaders.
func (w whoMissed) cells(split bool) []string {
	if split {
		return []string{strconv.Itoa(w.People), strconv.Itoa(w.Agents), optCount(w.Crawlers), optCount(w.Other)}
	}
	return []string{strconv.Itoa(w.People), strconv.Itoa(w.Agents)}
}

type analyticsReport struct {
	From        string       `json:"from"`
	To          string       `json:"to"`
	Days        int          `json:"days"`
	Totals      reportTotals `json:"totals"`
	TopArticles []countItem  `json:"topArticles"`
	TopSearches []struct {
		Label     string `json:"label"`
		Value     int    `json:"value"`
		NoResults int    `json:"noResults"`
	} `json:"topSearchTerms"`
	AIReferrals struct {
		Reads      int `json:"reads"`
		Assistants []struct {
			Assistant string `json:"assistant"`
			Reads     int    `json:"reads"`
			Readers   int    `json:"readers"`
		} `json:"assistants"`
	} `json:"aiReferrals"`
	NotFound []struct {
		Path string `json:"path"`
		whoMissed
	} `json:"notFound"`
}

type articleRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type contentInsights struct {
	Days     int  `json:"days"`
	TextDays int  `json:"textDays"`
	Semantic bool `json:"semantic"`
	Topics   []struct {
		Workspace  string      `json:"workspace"`
		Topic      string      `json:"topic"`
		Variants   []string    `json:"variants"`
		Unanswered int         `json:"unanswered"`
		People     int         `json:"people"`
		Agents     int         `json:"agents"`
		Reports    int         `json:"reports"`
		Closest    *articleRef `json:"closest"`
	} `json:"topics"`
	Pages []struct {
		Workspace   string    `json:"workspace"`
		Slug        string    `json:"slug"`
		Title       string    `json:"title"`
		Reasons     []string  `json:"reasons"`
		PeopleReads int       `json:"peopleReads"`
		AgentReads  int       `json:"agentReads"`
		VotesUp     int       `json:"votesUp"`
		VotesDown   int       `json:"votesDown"`
		UpdatedAt   time.Time `json:"updatedAt"`
	} `json:"pages"`
	Missing []struct {
		Workspace string `json:"workspace"`
		Path      string `json:"path"`
		Slug      string `json:"slug"`
		whoMissed
		Suggestion *articleRef `json:"suggestion"`
	} `json:"missing"`
}

// daysFlag registers --days with the range of an analytics operation.
func daysFlag(fs *flag.FlagSet, days *int, maxDays int) {
	fs.IntVar(days, "days", 30, fmt.Sprintf("days up to and including today, 1 to %d", maxDays))
}

func daysQuery(days, maxDays int) (url.Values, error) {
	if days < 1 || days > maxDays {
		return nil, usageError(fmt.Sprintf("--days must be 1 to %d", maxDays))
	}
	return url.Values{"days": {strconv.Itoa(days)}}, nil
}

func cmdAnalyticsReport() *Command {
	var days int
	return &Command{
		Group: "analytics", Name: "report", OpIDs: []string{"getAnalyticsReport"},
		Summary: "Show how people and agents used a workspace: reads, searches, AI assistants, and pages not found",
		Usage:   "cowl analytics report [--days N]",
		Flags:   func(fs *flag.FlagSet) { daysFlag(fs, &days, 731) },
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			q, err := daysQuery(days, 731)
			if err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/analytics", q, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(r analyticsReport) error {
				t := r.Totals
				fmt.Fprintf(a.Out, "%s to %s: %s by people (%s), %s by AI agents, %s.\n",
					r.From, r.To, count(t.Reads, "read", "reads"), count(t.Readers, "reader", "readers"),
					count(t.AgentReads, "read", "reads"), count(t.CrawlerHits, "crawler visit", "crawler visits"))
				fmt.Fprintf(a.Out, "%s by people, %d found nothing. %s by agents. %s not found%s.\n",
					count(t.Searches, "search", "searches"), t.SearchNoResults, count(t.AgentSearches, "search", "searches"),
					count(t.NotFound, "page", "pages"), t.notFoundSplit())
				if len(r.AIReferrals.Assistants) > 0 {
					fmt.Fprintln(a.Out, "\nfrom AI assistants:")
					rows := make([][]string, 0, len(r.AIReferrals.Assistants))
					for _, as := range r.AIReferrals.Assistants {
						rows = append(rows, []string{as.Assistant, strconv.Itoa(as.Reads), strconv.Itoa(as.Readers)})
					}
					a.table([]string{"ASSISTANT", "READS", "READERS"}, rows)
				}
				if len(r.TopArticles) > 0 {
					fmt.Fprintln(a.Out, "\nmost read by people:")
					rows := make([][]string, 0, len(r.TopArticles))
					for _, it := range r.TopArticles {
						rows = append(rows, []string{it.Label, strconv.Itoa(it.Value)})
					}
					a.table([]string{"TITLE", "READS"}, rows)
				}
				if len(r.TopSearches) > 0 {
					fmt.Fprintln(a.Out, "\ntop searches:")
					rows := make([][]string, 0, len(r.TopSearches))
					for _, s := range r.TopSearches {
						rows = append(rows, []string{s.Label, strconv.Itoa(s.Value), strconv.Itoa(s.NoResults)})
					}
					a.table([]string{"QUERY", "SEARCHES", "NO RESULTS"}, rows)
				}
				if len(r.NotFound) > 0 {
					fmt.Fprintln(a.Out, "\nnot found:")
					missed := make([]whoMissed, 0, len(r.NotFound))
					for _, p := range r.NotFound {
						missed = append(missed, p.whoMissed)
					}
					split := splitMissed(missed)
					rows := make([][]string, 0, len(r.NotFound))
					for _, p := range r.NotFound {
						rows = append(rows, append([]string{p.Path}, p.cells(split)...))
					}
					a.table(append([]string{"PATH"}, missedHeaders(split)...), rows)
				}
				return nil
			})
		},
	}
}

func cmdAnalyticsNext() *Command {
	var days int
	return &Command{
		Group: "analytics", Name: "next", OpIDs: []string{"getContentInsights"},
		Summary: "List the next steps for the docs: questions to answer, pages to update, missing pages to fix",
		Usage:   "cowl analytics next [--days N]",
		Flags:   func(fs *flag.FlagSet) { daysFlag(fs, &days, 90) },
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			q, err := daysQuery(days, 90)
			if err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/content-insights", q, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(in contentInsights) error {
				if len(in.Topics)+len(in.Pages)+len(in.Missing) == 0 {
					fmt.Fprintf(a.Out, "nothing to write, update, or fix in the last %d days\n", in.Days)
					return nil
				}
				grouping := "wording"
				if in.Semantic {
					grouping = "meaning"
				}
				fmt.Fprintf(a.Out, "last %d days. questions grouped by %s.\n", in.Days, grouping)
				if len(in.Topics) > 0 {
					fmt.Fprintln(a.Out, "\nwrite:")
					rows := make([][]string, 0, len(in.Topics))
					for _, t := range in.Topics {
						closest := "-"
						if t.Closest != nil {
							closest = t.Closest.Slug
						}
						rows = append(rows, []string{t.Topic, strconv.Itoa(t.Unanswered), strconv.Itoa(t.People),
							strconv.Itoa(t.Agents), strconv.Itoa(t.Reports), closest})
					}
					a.table([]string{"QUESTION", "UNANSWERED", "PEOPLE", "AGENTS", "REPORTS", "CLOSEST"}, rows)
				}
				if len(in.Pages) > 0 {
					fmt.Fprintln(a.Out, "\nupdate:")
					rows := make([][]string, 0, len(in.Pages))
					for _, p := range in.Pages {
						var why []string
						for _, r := range p.Reasons {
							switch r {
							case "unhelpful":
								why = append(why, fmt.Sprintf("%d of %d said not helpful", p.VotesDown, p.VotesUp+p.VotesDown))
							case "stale":
								why = append(why, fmt.Sprintf("read %d times, unchanged since %s", p.PeopleReads+p.AgentReads, p.UpdatedAt.Format("2006-01-02")))
							}
						}
						rows = append(rows, []string{p.Slug, p.Title, strings.Join(why, "; ")})
					}
					a.table([]string{"SLUG", "TITLE", "REASON"}, rows)
				}
				if len(in.Missing) > 0 {
					fmt.Fprintln(a.Out, "\nfix:")
					missed := make([]whoMissed, 0, len(in.Missing))
					for _, m := range in.Missing {
						missed = append(missed, m.whoMissed)
					}
					split := splitMissed(missed)
					headers := []string{"PATH", "ASKED", "REDIRECT TO"}
					if split {
						headers = slices.Concat([]string{"PATH"}, missedHeaders(true), []string{"REDIRECT TO"})
					}
					rows := make([][]string, 0, len(in.Missing))
					for _, m := range in.Missing {
						target := "-"
						if m.Suggestion != nil {
							target = m.Suggestion.Slug
						}
						asked := []string{strconv.Itoa(m.People + m.Agents)}
						if split {
							asked = m.cells(true)
						}
						rows = append(rows, slices.Concat([]string{m.Path}, asked, []string{target}))
					}
					a.table(headers, rows)
				}
				if days > in.TextDays && len(in.Topics) > 0 {
					fmt.Fprintf(a.Err, "note: question text is kept %d days, so older questions are not listed\n", in.TextDays)
				}
				return nil
			})
		},
	}
}

func cmdAnalyticsGap() *Command {
	var slug string
	return &Command{
		Group: "analytics", Name: "gap", OpIDs: []string{"reportContentGap"},
		Summary: "Tell the docs team about a question that the docs did not answer",
		Usage:   "cowl analytics gap QUESTION [--slug SLUG]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&slug, "slug", "", "slug of the article that came closest, if one did")
		},
		Run: func(a *App, args []string) error {
			question := joinArgs(args)
			if question == "" {
				return usageError("QUESTION is required")
			}
			body := map[string]any{"question": question}
			if slug != "" {
				body["slug"] = slug
			}
			raw, err := a.request("POST", a.ws()+"/content-gaps", nil, body)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(r struct {
				Message string `json:"message"`
			}) error {
				fmt.Fprintln(a.Out, r.Message)
				return nil
			})
		},
	}
}
