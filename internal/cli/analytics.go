package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type reportTotals struct {
	Reads           int `json:"reads"`
	Readers         int `json:"readers"`
	AgentReads      int `json:"agentReads"`
	AgentSearches   int `json:"agentSearches"`
	CrawlerHits     int `json:"crawlerHits"`
	Searches        int `json:"searches"`
	SearchNoResults int `json:"searchNoResults"`
	NotFound        int `json:"notFound"`

	// AICrawlerHits is nil when the server does not send it, so the report
	// of an older server prints as it did.
	AICrawlerHits *int `json:"aiCrawlerHits"`
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
		Path   string `json:"path"`
		People int    `json:"people"`
		Agents int    `json:"agents"`
	} `json:"notFound"`
	Previous *reportTotals `json:"previous"`
	Growth   *reportGrowth `json:"growth"`
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
		Workspace  string      `json:"workspace"`
		Path       string      `json:"path"`
		Slug       string      `json:"slug"`
		People     int         `json:"people"`
		Agents     int         `json:"agents"`
		Suggestion *articleRef `json:"suggestion"`
	} `json:"missing"`
	Rising *risingTopics `json:"rising"`
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
		Summary: "Show how people and agents used a workspace: reads, what is rising, searches, AI assistants, and pages not found",
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
					count(t.AgentReads, "read", "reads"), crawlerVisits(t))
				fmt.Fprintf(a.Out, "%s by people, %d found nothing. %s by agents. %s not found.\n",
					count(t.Searches, "search", "searches"), t.SearchNoResults, count(t.AgentSearches, "search", "searches"),
					count(t.NotFound, "page", "pages"))
				printComparison(a, r.Days, t, r.Previous)
				printReportGrowth(a, r.Days, r.Growth)
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
					rows := make([][]string, 0, len(r.NotFound))
					for _, p := range r.NotFound {
						rows = append(rows, []string{p.Path, strconv.Itoa(p.People), strconv.Itoa(p.Agents)})
					}
					a.table([]string{"PATH", "PEOPLE", "AGENTS"}, rows)
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
		Summary: "List the next steps for the docs: questions to answer, pages to update, missing pages to fix, and rising topics",
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
				if len(in.Topics)+len(in.Pages)+len(in.Missing) == 0 && in.Rising.empty() {
					fmt.Fprintf(a.Out, "nothing to write, update, or fix in the last %d days\n", in.Days)
					printRisingTopics(a, in.Days, in.Rising)
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
					rows := make([][]string, 0, len(in.Missing))
					for _, m := range in.Missing {
						target := "-"
						if m.Suggestion != nil {
							target = m.Suggestion.Slug
						}
						rows = append(rows, []string{m.Path, strconv.Itoa(m.People + m.Agents), target})
					}
					a.table([]string{"PATH", "ASKED", "REDIRECT TO"}, rows)
				}
				printRisingTopics(a, in.Days, in.Rising)
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
