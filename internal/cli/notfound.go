package cli

import (
	"fmt"
	"slices"
	"strconv"
)

// notFoundTotals splits the not-found count of a report period by who asked.
// The fields are nil when the server does not split the count.
type notFoundTotals struct {
	NotFoundPeople   *int `json:"notFoundPeople"`
	NotFoundAgents   *int `json:"notFoundAgents"`
	NotFoundCrawlers *int `json:"notFoundCrawlers"`
	NotFoundOther    *int `json:"notFoundOther"`
}

// notFoundLabel returns the split for the title of the not found table, or
// "" when the server does not split the count.
func (n notFoundTotals) notFoundLabel() string {
	if n.NotFoundPeople == nil || n.NotFoundAgents == nil || n.NotFoundCrawlers == nil || n.NotFoundOther == nil {
		return ""
	}
	return fmt.Sprintf(" (people %d, AI agents %d, crawlers %d, other %d)",
		*n.NotFoundPeople, *n.NotFoundAgents, *n.NotFoundCrawlers, *n.NotFoundOther)
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
