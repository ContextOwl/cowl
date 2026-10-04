package cli

import (
	"fmt"
	"strconv"
	"time"
)

type agentQuestion struct {
	Query      string    `json:"query"`
	Count      int       `json:"count"`
	Unanswered int       `json:"unanswered"`
	LastSeen   time.Time `json:"lastSeen"`
}

type agentInsights struct {
	Days               int             `json:"days"`
	RecordsQueries     bool            `json:"recordsQueries"`
	Calls              int             `json:"calls"`
	Searches           int             `json:"searches"`
	Reads              int             `json:"reads"`
	UnansweredSearches int             `json:"unansweredSearches"`
	UnansweredShare    int             `json:"unansweredShare"`
	Keys               int             `json:"keys"`
	Questions          []agentQuestion `json:"questions"`
	Unanswered         []agentQuestion `json:"unanswered"`
	MostRead           []struct {
		Slug       string `json:"slug"`
		Title      string `json:"title"`
		AgentReads int    `json:"agentReads"`
		HumanViews int    `json:"humanViews"`
	} `json:"mostRead"`
	Clients   []countItem `json:"clients"`
	KeyLabels []countItem `json:"keyLabels"`
}

type countItem struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

func cmdInsights() *Command {
	return &Command{
		Name: "insights", OpIDs: []string{"getAgentInsights"},
		Summary: "Show what agents asked a workspace in the last 30 days, and what went unanswered",
		Usage:   "cowl insights",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/agent-insights", nil, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(in agentInsights) error {
				if in.Calls == 0 {
					fmt.Fprintf(a.Out, "no agent calls in the last %d days\n", in.Days)
					return nil
				}
				fmt.Fprintf(a.Out, "last %d days: %s, %s, %s, %s. %d%% of searches unanswered (%d).\n",
					in.Days, count(in.Calls, "call", "calls"), count(in.Searches, "search", "searches"),
					count(in.Reads, "read", "reads"), count(in.Keys, "key", "keys"), in.UnansweredShare, in.UnansweredSearches)

				if len(in.Unanswered) > 0 {
					fmt.Fprintln(a.Out, "\nunanswered:")
					rows := make([][]string, 0, len(in.Unanswered))
					for _, q := range in.Unanswered {
						rows = append(rows, []string{q.Query, strconv.Itoa(q.Count), timeLabel(q.LastSeen)})
					}
					a.table([]string{"QUESTION", "SEARCHES", "LAST SEEN"}, rows)
				}
				if len(in.Questions) > 0 {
					fmt.Fprintln(a.Out, "\nquestions:")
					rows := make([][]string, 0, len(in.Questions))
					for _, q := range in.Questions {
						rows = append(rows, []string{q.Query, strconv.Itoa(q.Count), strconv.Itoa(q.Unanswered)})
					}
					a.table([]string{"QUESTION", "SEARCHES", "UNANSWERED"}, rows)
				}
				if len(in.MostRead) > 0 {
					fmt.Fprintln(a.Out, "\nmost read by agents:")
					rows := make([][]string, 0, len(in.MostRead))
					for _, r := range in.MostRead {
						rows = append(rows, []string{r.Slug, r.Title, strconv.Itoa(r.AgentReads), strconv.Itoa(r.HumanViews)})
					}
					a.table([]string{"SLUG", "TITLE", "AGENT READS", "HUMAN READS"}, rows)
				}
				printCounts(a, "clients", "CLIENT", in.Clients)
				printCounts(a, "keys", "KEY", in.KeyLabels)
				if !in.RecordsQueries {
					fmt.Fprintln(a.Err, "note: this organization does not save agent search text, so questions are not listed")
				}
				return nil
			})
		},
	}
}

func printCounts(a *App, title, header string, items []countItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(a.Out, "\n%s:\n", title)
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{it.Label, strconv.Itoa(it.Value)})
	}
	a.table([]string{header, "CALLS"}, rows)
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
