package cli

import (
	csvfile "encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxQuestionLimit is the largest page of listQuestions.
const maxQuestionLimit = 1000

// questionActors are the values of --actor, with the words of the list
// header.
var questionActors = []struct{ value, label string }{
	{"agents", "AI agents"},
	{"people", "people"},
	{"tools", "tools such as cowl and scripts"},
}

// questionList is the response of listQuestions.
type questionList struct {
	From           string          `json:"from"`
	To             string          `json:"to"`
	Total          int             `json:"total"`
	Actor          string          `json:"actor"`
	Principal      string          `json:"principal"`
	Unanswered     bool            `json:"unanswered"`
	RecordsQueries bool            `json:"recordsQueries"`
	Questions      []agentQuestion `json:"questions"`
}

func cmdAnalyticsQuestions() *Command {
	var opts struct {
		days, limit, offset  int
		actor, principal     string
		unanswered, all, csv bool
	}
	return &Command{
		Group: "analytics", Name: "questions", OpIDs: []string{"listQuestions"},
		Summary: "List the questions that people and agents asked a workspace (--csv for a spreadsheet)",
		Usage:   "cowl analytics questions [--days N] [--actor A] [--principal P] [--unanswered] [--limit N] [--offset N] [--all] [--csv]",
		Notes:   questionTextNote,
		Flags: func(fs *flag.FlagSet) {
			daysFlag(fs, &opts.days, 90)
			fs.StringVar(&opts.actor, "actor", "agents", "whose questions: AI agents (agents), people (people), or cowl, the GitHub Action and scripts (tools)")
			fs.StringVar(&opts.principal, "principal", "", principalFlagHelp)
			fs.BoolVar(&opts.unanswered, "unanswered", false, "only the questions with an unanswered search or a gap report, most unanswered first")
			fs.IntVar(&opts.limit, "limit", 100, fmt.Sprintf("questions per request, 1 to %d", maxQuestionLimit))
			fs.IntVar(&opts.offset, "offset", 0, "questions to skip")
			fs.BoolVar(&opts.all, "all", false, fmt.Sprintf("get every page until the list ends (pages of --limit, default %d)", maxQuestionLimit))
			fs.BoolVar(&opts.csv, "csv", false, "print the questions as CSV for a spreadsheet, also on a terminal")
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if opts.csv && a.g.jsonOut {
				return usageError("use --csv or --json, not both")
			}
			if opts.limit < 1 || opts.limit > maxQuestionLimit {
				return usageError(fmt.Sprintf("--limit must be 1 to %d", maxQuestionLimit))
			}
			if opts.offset < 0 {
				return usageError("--offset must be 0 or more")
			}
			q := url.Values{}
			if err := setDays(a, q, opts.days, 90); err != nil {
				return err
			}
			if a.flagWasSet("actor") {
				actor := strings.TrimSpace(opts.actor)
				if questionActorLabel(actor) == "" {
					return usageError("--actor must be agents, people or tools")
				}
				q.Set("actor", actor)
			}
			if err := setPrincipal(a, q, opts.principal); err != nil {
				return err
			}
			if opts.unanswered {
				q.Set("unanswered", "true")
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
				pageSize := maxQuestionLimit
				if a.flagWasSet("limit") {
					pageSize = opts.limit
				}
				raw, err = a.questionPages(q, pageSize, opts.offset)
			} else {
				raw, err = a.request("GET", a.ws()+"/questions", q, nil)
			}
			if err != nil {
				return err
			}
			if !opts.csv {
				return emitAs(a, raw, func(list questionList) error {
					list.print(a, opts.offset, opts.all)
					return nil
				})
			}
			var list questionList
			if err := json.Unmarshal(raw, &list); err != nil {
				return invalidResponse(err)
			}
			if err := writeQuestionsCSV(a, list.Questions); err != nil {
				return err
			}
			if note := list.pageNote(opts.offset, opts.all); note != "" && a.ErrTTY {
				fmt.Fprintln(a.Err, note)
			}
			return nil
		},
	}
}

// questionActorLabel returns the words of an actor group, or "" for a value
// that is not a group.
func questionActorLabel(actor string) string {
	for _, g := range questionActors {
		if g.value == actor {
			return g.label
		}
	}
	return ""
}

// print writes one page of questions for a person, or every question after
// --all.
func (l questionList) print(a *App, offset int, all bool) {
	who := firstOf(questionActorLabel(l.Actor), l.Actor, "AI agents")
	if l.Principal != "" {
		who += " (who asked: " + principalLabel(l.Principal) + ")"
	}
	filter := ""
	if l.Unanswered {
		filter = ", only unanswered or reported"
	}
	fmt.Fprintf(a.Out, "%s to %s: %s from %s%s.\n", l.From, l.To, count(l.Total, "question", "questions"), who, filter)
	if len(l.Questions) > 0 {
		fmt.Fprintln(a.Out)
		printQuestions(a, l.Questions, false)
	}
	if note := l.pageNote(offset, all); note != "" {
		fmt.Fprintln(a.Err, note)
	}
}

// pageNote says when the list holds more questions than this page, or when
// the organization saves no search text. It is "" otherwise.
func (l questionList) pageNote(offset int, all bool) string {
	shown := offset + len(l.Questions)
	switch {
	case !l.RecordsQueries:
		return "note: this organization does not save search text, so no questions are listed"
	case all:
		return ""
	case len(l.Questions) == 0 && offset > 0 && l.Total > 0:
		return fmt.Sprintf("note: --offset %d is past the last question", offset)
	case shown < l.Total:
		return fmt.Sprintf("note: this page holds questions %d to %d of %d. Add --offset %d for the next page, or --all for every question",
			offset+1, shown, l.Total, shown)
	}
	return ""
}

// questionPages pages listQuestions with limit and offset until the list
// ends. It returns the first page with the questions of every page, and
// leaves out a question that a later page repeats. A full page with no new
// question means that the server ignores offset, so paging stops there.
func (a *App) questionPages(q url.Values, pageSize, offset int) (json.RawMessage, error) {
	var first map[string]json.RawMessage
	var all []json.RawMessage
	seen := map[string]bool{}
	for {
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("offset", strconv.Itoa(offset))
		raw, err := a.request("GET", a.ws()+"/questions", q, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Total     int               `json:"total"`
			Questions []json.RawMessage `json:"questions"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, invalidResponse(err)
		}
		if first == nil {
			if err := json.Unmarshal(raw, &first); err != nil {
				return nil, invalidResponse(err)
			}
		}
		fresh := 0
		for _, item := range page.Questions {
			var row struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(item, &row); err != nil {
				return nil, invalidResponse(err)
			}
			if !seen[row.Query] {
				seen[row.Query] = true
				fresh++
				all = append(all, item)
			}
		}
		offset += len(page.Questions)
		if len(page.Questions) < pageSize || offset >= page.Total {
			break
		}
		if fresh == 0 {
			return nil, &cliError{code: "invalid_response", message: "the server sent the same page of questions again, so cowl stopped paging", exit: exitError}
		}
	}
	first["questions"] = joinJSONArray(all)
	return json.Marshal(first)
}

// questionsCSVHeader names the CSV columns as the export of Admin >
// Analytics names them.
var questionsCSVHeader = []string{"question", "searches", "unanswered", "reports", "first seen", "last seen", "clients", "principals", "keys"}

// writeQuestionsCSV prints questions as CSV with CRLF line ends (RFC 4180).
// Readers and agents type the question texts, so each text cell goes
// through csvText.
func writeQuestionsCSV(a *App, qs []agentQuestion) error {
	w := csvfile.NewWriter(a.Out)
	w.UseCRLF = true
	if err := w.Write(questionsCSVHeader); err != nil {
		return err
	}
	for _, q := range qs {
		lastSeen := ""
		if !q.LastSeen.IsZero() {
			lastSeen = q.LastSeen.Format(time.RFC3339Nano)
		}
		reports := ""
		if q.Reports != nil {
			reports = strconv.Itoa(*q.Reports)
		}
		if err := w.Write([]string{
			csvText(q.Query), strconv.Itoa(q.Count), strconv.Itoa(q.Unanswered), reports, csvText(q.FirstSeen), lastSeen,
			csvText(strings.Join(q.Clients, "; ")), csvText(strings.Join(q.Principals, "; ")), csvText(strings.Join(q.Keys, "; ")),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// csvText keeps a text cell as text in a spreadsheet. A spreadsheet runs a
// cell that starts with =, +, -, @, a tab or a carriage return as a formula.
// Such a cell gets a single quote first, as in the export of Admin >
// Analytics.
func csvText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
