package cli

import (
	"fmt"
	"math"
	"strconv"
)

// The statuses of a rising list that cannot compare with the period before.
// Such a list has no items.
const (
	growthNoBaseline = "no_baseline"
	growthNoHistory  = "no_history"
	growthTextOff    = "text_off"
)

// growthRule is the rise rule that the server applied to the rising lists.
type growthRule struct {
	MinCount    int     `json:"minCount"`
	MinChange   int     `json:"minChange"`
	MinScore    float64 `json:"minScore"`
	MinBaseline int     `json:"minBaseline"`
}

// risingItem is a page or a search that rose. Count and Previous are readers
// or agents, each counted once a day. Events is the reads or searches.
type risingItem struct {
	Slug       string `json:"slug"`
	Label      string `json:"label"`
	Count      int    `json:"count"`
	Previous   int    `json:"previous"`
	Events     int    `json:"events"`
	Unanswered int    `json:"unanswered"`
	New        bool   `json:"new"`
	Change     int    `json:"change"`
}

type risingList struct {
	Status string       `json:"status"`
	Items  []risingItem `json:"items"`
}

type growthSide struct {
	Pages    risingList `json:"pages"`
	Searches risingList `json:"searches"`
}

// reportGrowth lists the rising pages and searches of a report. Pages
// compare Days days and searches SearchDays days, each with the same number
// of days before.
type reportGrowth struct {
	Days       int        `json:"days"`
	SearchDays int        `json:"searchDays"`
	Rule       growthRule `json:"rule"`
	People     growthSide `json:"people"`
	Agents     growthSide `json:"agents"`
}

// risingTopic is a topic of searches that rose. It counts like risingItem,
// and Reports counts the gap reports of agents.
type risingTopic struct {
	Topic      string      `json:"topic"`
	Count      int         `json:"count"`
	Previous   int         `json:"previous"`
	Events     int         `json:"events"`
	Unanswered int         `json:"unanswered"`
	Reports    int         `json:"reports"`
	New        bool        `json:"new"`
	Change     int         `json:"change"`
	Closest    *articleRef `json:"closest"`
}

type risingTopicList struct {
	Status string        `json:"status"`
	Items  []risingTopic `json:"items"`
}

// risingTopics lists the rising topics of the next steps. They compare
// SearchDays days with the SearchDays days before.
type risingTopics struct {
	SearchDays int             `json:"searchDays"`
	Rule       growthRule      `json:"rule"`
	People     risingTopicList `json:"people"`
	Agents     risingTopicList `json:"agents"`
}

// empty reports whether no rising topic is listed. A server without rising
// topics lists none.
func (r *risingTopics) empty() bool {
	return r == nil || len(r.People.Items)+len(r.Agents.Items) == 0
}

// audience names the readers of a rising list in its title, its count
// column and its notes. Only agents send gap reports.
type audience struct {
	name    string
	counted string
	reports bool
}

var (
	byPeople = audience{name: "people", counted: "READERS"}
	byAgents = audience{name: "AI agents", counted: "AGENTS", reports: true}
)

// risingTable is one rising list as cowl prints it. events is what the list
// counts besides readers or agents: reads for pages, searches for searches
// and topics. days is the window that the list compares.
type risingTable struct {
	title   string
	who     audience
	events  string
	days    int
	status  string
	headers []string
	rows    [][]string
}

func (g *reportGrowth) tables() []risingTable {
	return []risingTable{
		pageTable(byPeople, g.Days, g.People.Pages),
		pageTable(byAgents, g.Days, g.Agents.Pages),
		searchTable(byPeople, "searches", "QUERY", g.SearchDays, g.People.Searches),
		searchTable(byAgents, "questions", "QUESTION", g.SearchDays, g.Agents.Searches),
	}
}

func (r *risingTopics) tables() []risingTable {
	return []risingTable{
		topicTable(byPeople, r.SearchDays, r.People),
		topicTable(byAgents, r.SearchDays, r.Agents),
	}
}

func pageTable(who audience, days int, l risingList) risingTable {
	t := risingTable{title: "rising pages, " + who.name, who: who, events: "reads", days: days, status: l.Status,
		headers: []string{"SLUG", "TITLE", who.counted, "BEFORE", "CHANGE", "READS"}}
	for _, it := range l.Items {
		t.rows = append(t.rows, []string{it.Slug, it.Label, strconv.Itoa(it.Count), strconv.Itoa(it.Previous),
			riseLabel(it.New, it.Change), strconv.Itoa(it.Events)})
	}
	return t
}

func searchTable(who audience, noun, header string, days int, l risingList) risingTable {
	t := risingTable{title: "rising " + noun + ", " + who.name, who: who, events: "searches", days: days, status: l.Status,
		headers: []string{header, who.counted, "BEFORE", "CHANGE", "SEARCHES", "UNANSWERED"}}
	for _, it := range l.Items {
		t.rows = append(t.rows, []string{it.Label, strconv.Itoa(it.Count), strconv.Itoa(it.Previous),
			riseLabel(it.New, it.Change), strconv.Itoa(it.Events), strconv.Itoa(it.Unanswered)})
	}
	return t
}

func topicTable(who audience, days int, l risingTopicList) risingTable {
	t := risingTable{title: "rising topics, " + who.name, who: who, events: "searches", days: days, status: l.Status,
		headers: []string{"TOPIC", who.counted, "BEFORE", "CHANGE", "SEARCHES", "UNANSWERED"}}
	if who.reports {
		t.headers = append(t.headers, "REPORTS")
	}
	t.headers = append(t.headers, "CLOSEST")
	for _, it := range l.Items {
		row := []string{it.Topic, strconv.Itoa(it.Count), strconv.Itoa(it.Previous), riseLabel(it.New, it.Change),
			strconv.Itoa(it.Events), strconv.Itoa(it.Unanswered)}
		if who.reports {
			row = append(row, strconv.Itoa(it.Reports))
		}
		closest := "-"
		if it.Closest != nil {
			closest = it.Closest.Slug
		}
		t.rows = append(t.rows, append(row, closest))
	}
	return t
}

// printReportGrowth prints the rising lists of a report. A server without
// growth prints nothing.
func printReportGrowth(a *App, rangeDays int, g *reportGrowth) {
	if g != nil {
		printRising(a, rangeDays, g.Rule, g.tables())
	}
}

// printRisingTopics prints the rising topics of the next steps. A server
// without rising topics prints nothing.
func printRisingTopics(a *App, rangeDays int, r *risingTopics) {
	if r != nil {
		printRising(a, rangeDays, r.Rule, r.tables())
	}
}

// printRising prints each rising table that has rows, then the rule of the
// rows, then a note on stderr for each list that cannot compare. A table
// that compares fewer days than the command's range names its window.
func printRising(a *App, rangeDays int, rule growthRule, tables []risingTable) {
	printed := false
	for _, t := range tables {
		if len(t.rows) == 0 {
			continue
		}
		title := t.title
		if t.days != rangeDays {
			title += " (last " + count(t.days, "day", "days") + ")"
		}
		fmt.Fprintf(a.Out, "\n%s:\n", title)
		a.table(t.headers, t.rows)
		printed = true
	}
	if printed {
		fmt.Fprintf(a.Out, "\na rising row needs %d or more readers or agents, each counted once a day, at least %d%% more than before, and a score of %s or more.\n",
			rule.MinCount, rule.MinChange, strconv.FormatFloat(rule.MinScore, 'f', -1, 64))
	}
	for _, t := range tables {
		if why := t.notListed(rule.MinBaseline); why != "" {
			fmt.Fprintf(a.Err, "note: %s: not listed, because %s\n", t.title, why)
		}
	}
}

// notListed says why a list that cannot compare has no rows. It is empty for
// a ready list and for a status that cowl does not know.
func (t risingTable) notListed(minBaseline int) string {
	switch t.status {
	case growthNoBaseline:
		return fmt.Sprintf("%s had fewer than %d %s by %s", daysBefore(t.days), minBaseline, t.events, t.who.name)
	case growthNoHistory:
		if t.events == "reads" {
			return "ContextOwl recorded reads for only part of " + daysBefore(t.days)
		}
		return "ContextOwl kept search text for only part of " + daysBefore(t.days)
	case growthTextOff:
		return "this organization does not save search text"
	}
	return ""
}

// printComparison prints the change of the reads and searches since the
// period before the range. A server without previous prints nothing.
func printComparison(a *App, days int, now reportTotals, before *reportTotals) {
	if before == nil {
		return
	}
	fmt.Fprintf(a.Out, "compared with %s: reads by people %s, reads by AI agents %s, searches by people %s, searches by agents %s.\n",
		daysBefore(days), change(before.Reads, now.Reads), change(before.AgentReads, now.AgentReads),
		change(before.Searches, now.Searches), change(before.AgentSearches, now.AgentSearches))
}

// crawlerVisits names the crawler visits of a report, and the pages that AI
// crawlers read when the server sends that count.
func crawlerVisits(t reportTotals) string {
	visits := count(t.CrawlerHits, "crawler visit", "crawler visits")
	if t.AICrawlerHits == nil {
		return visits
	}
	return visits + " (" + count(*t.AICrawlerHits, "page", "pages") + " read by AI crawlers)"
}

// daysBefore names the period just before a range of days.
func daysBefore(days int) string {
	if days == 1 {
		return "the day before"
	}
	return fmt.Sprintf("the %d days before", days)
}

// change is the change from before to now in percent. It is "new" when
// nothing came before, and "none" when nothing came in either period.
func change(before, now int) string {
	switch {
	case before == 0 && now == 0:
		return "none"
	case before == 0:
		return "new"
	}
	return signedPercent(int(math.Round(float64(now-before) * 100 / float64(before))))
}

// riseLabel is the CHANGE cell of a rising row.
func riseLabel(isNew bool, pct int) string {
	if isNew {
		return "new"
	}
	return signedPercent(pct)
}

func signedPercent(pct int) string {
	if pct > 0 {
		return "+" + strconv.Itoa(pct) + "%"
	}
	return strconv.Itoa(pct) + "%"
}
