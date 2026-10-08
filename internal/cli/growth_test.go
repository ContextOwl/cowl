package cli

import (
	"strings"
	"testing"
)

const (
	// todayReportJSON is a getAnalyticsReport body without previous, growth
	// and aiCrawlerHits.
	todayReportJSON = `{"from":"2026-09-30","to":"2026-10-06","days":7,` +
		`"totals":{"reads":120,"readers":40,"agentReads":33,"agentSearches":9,"crawlerHits":210,"searches":18,"searchNoResults":4,"notFound":3},` +
		`"topArticles":[{"label":"MCP Quickstart","value":51,"bar":100}],` +
		`"topSearchTerms":[{"label":"sso with okta","value":5,"bar":100,"noResults":5}],` +
		`"aiReferrals":{"reads":14,"readers":11,"previous":6,"assistants":[{"assistant":"ChatGPT","reads":10,"readers":8,"previous":4,"bar":100}]},` +
		`"notFound":[{"path":"/docs/platform/rotate-key","people":2,"agents":1}]}`

	// previousJSON is the previous field that servers of today send.
	previousJSON = `"previous":{"reads":96,"readers":30,"agentReads":33,"agentSearches":12,"crawlerHits":190,"searches":18,"searchNoResults":2,"notFound":1}`

	growthRuleJSON = `"rule":{"minCount":5,"minChange":25,"minScore":2,"minBaseline":10}`

	growthReportJSON = `{"from":"2026-09-30","to":"2026-10-06","days":7,"textDays":90,` +
		`"totals":{"reads":120,"readers":40,"agentReads":33,"agentReadsVerified":5,"agentSearches":9,"crawlerHits":210,"aiCrawlerHits":80,` +
		`"searches":18,"searchNoResults":4,"notFound":3,"notFoundPeople":2,"notFoundAgents":1,"notFoundCrawlers":0,"notFoundOther":0},` +
		`"previous":{"reads":100,"readers":35,"agentReads":23,"agentReadsVerified":2,"agentSearches":0,"crawlerHits":200,"aiCrawlerHits":70,` +
		`"searches":20,"searchNoResults":3,"notFound":4,"notFoundPeople":4,"notFoundAgents":0,"notFoundCrawlers":0,"notFoundOther":0},` +
		`"trend":[],"channels":[],"topArticles":[{"label":"MCP Quickstart","value":51,"bar":100}],"topSearchTerms":[],` +
		`"aiReferrals":{"reads":14,"readers":11,"previous":6,"assistants":[{"assistant":"ChatGPT","reads":10,"readers":8,"previous":4,"bar":100}]},` +
		`"notFound":[],"growth":{"days":7,"searchDays":7,` + growthRuleJSON + `,` +
		`"people":{"pages":{"status":"ready","items":[{"workspace":"platform","slug":"mcp","label":"MCP Quickstart","count":12,"previous":2,"events":40,"unanswered":0,"new":false,"change":500,"score":2.7}]},` +
		`"searches":{"status":"ready","items":[{"label":"sso with okta","count":9,"previous":0,"events":14,"unanswered":5,"new":true,"change":0,"score":3}]}},` +
		`"agents":{"pages":{"status":"ready","items":[{"workspace":"platform","slug":"api-keys","label":"Agent Keys","count":23,"previous":9,"events":61,"unanswered":0,"new":false,"change":156,"score":2.5}]},` +
		`"searches":{"status":"ready","items":[{"label":"rotate a key without downtime","count":7,"previous":0,"events":22,"unanswered":4,"new":true,"change":0,"score":2.6}]}}}}`

	ruleLine = "a rising row needs 5 or more readers or agents, each counted once a day, at least 25% more than before, and a score of 2 or more.\n"

	// searchWindowReportJSON is a report of 90 days. Its search lists compare
	// the last 30 days with the 30 days before.
	searchWindowReportJSON = `{"from":"2026-07-09","to":"2026-10-06","days":90,"textDays":90,` +
		`"totals":{"reads":900,"readers":300,"agentReads":120,"agentSearches":40,"crawlerHits":0,"aiCrawlerHits":0,"searches":60,"searchNoResults":10,"notFound":0},` +
		`"previous":{"reads":600,"readers":200,"agentReads":150,"agentSearches":40,"crawlerHits":0,"aiCrawlerHits":0,"searches":80,"searchNoResults":5,"notFound":0},` +
		`"topArticles":[],"topSearchTerms":[],"aiReferrals":{"reads":0,"assistants":[]},"notFound":[],` +
		`"growth":{"days":90,"searchDays":30,` + growthRuleJSON + `,` +
		`"people":{"pages":{"status":"ready","items":[{"workspace":"platform","slug":"sso","label":"Single sign-on","count":40,"previous":12,"events":95,"unanswered":0,"new":false,"change":233,"score":3.9}]},` +
		`"searches":{"status":"ready","items":[{"label":"okta scim","count":8,"previous":1,"events":11,"unanswered":3,"new":false,"change":700,"score":2.3}]}},` +
		`"agents":{"pages":{"status":"ready","items":[]},"searches":{"status":"no_baseline","items":[]}}}}`
)

// runTerm runs a command on a terminal and requires exit 0.
func runTerm(t *testing.T, body string, args ...string) (stdout, stderr string) {
	t.Helper()
	out, errOut, code := run(t, &fakeAPI{body: body}, args, runOpts{term: true})
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	return out, errOut
}

// growthBody builds a getAnalyticsReport body whose 4 rising lists have the
// given statuses and no items.
func growthBody(days, searchDays int, peoplePages, agentPages, peopleSearches, agentSearches string) string {
	list := func(status string) string { return `{"status":"` + status + `","items":[]}` }
	return `{"from":"2026-07-09","to":"2026-10-06","days":` + itoa(days) + `,` +
		`"totals":{"reads":3,"readers":2,"agentReads":1,"agentSearches":0,"crawlerHits":0,"aiCrawlerHits":0,"searches":0,"searchNoResults":0,"notFound":0},` +
		`"topArticles":[],"topSearchTerms":[],"aiReferrals":{"reads":0,"assistants":[]},"notFound":[],` +
		`"growth":{"days":` + itoa(days) + `,"searchDays":` + itoa(searchDays) + `,` + growthRuleJSON + `,` +
		`"people":{"pages":` + list(peoplePages) + `,"searches":` + list(peopleSearches) + `},` +
		`"agents":{"pages":` + list(agentPages) + `,"searches":` + list(agentSearches) + `}}}`
}

func TestAnalyticsReportGrowth(t *testing.T) {
	out, errOut := runTerm(t, growthReportJSON, "analytics", "report", "-w", "platform", "--days", "7")
	want := "2026-09-30 to 2026-10-06: 120 reads by people (40 readers), 33 reads by AI agents, 210 crawler visits (80 pages read by AI crawlers).\n" +
		"18 searches by people, 4 found nothing. 9 searches by agents. 3 pages not found.\n" +
		"compared with the 7 days before: reads by people +20%, reads by AI agents +43%, searches by people -10%, searches by agents new.\n" +
		"\nrising pages, people:\n" +
		"SLUG  TITLE           READERS  BEFORE  CHANGE  READS\n" +
		"mcp   MCP Quickstart  12       2       +500%   40\n" +
		"\nrising pages, AI agents:\n" +
		"SLUG      TITLE       AGENTS  BEFORE  CHANGE  READS\n" +
		"api-keys  Agent Keys  23      9       +156%   61\n" +
		"\nrising searches, people:\n" +
		"QUERY          READERS  BEFORE  CHANGE  SEARCHES  UNANSWERED\n" +
		"sso with okta  9        0       new     14        5\n" +
		"\nrising questions, AI agents:\n" +
		"QUESTION                       AGENTS  BEFORE  CHANGE  SEARCHES  UNANSWERED\n" +
		"rotate a key without downtime  7       0       new     22        4\n" +
		"\n" + ruleLine +
		"\nfrom AI assistants:\n" +
		"ASSISTANT  READS  READERS\n" +
		"ChatGPT    10     8\n" +
		"\nmost read by people:\n" +
		"TITLE           READS\n" +
		"MCP Quickstart  51\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if errOut != "" {
		t.Errorf("every list is ready, so stderr must stay empty: %q", errOut)
	}

	out, errOut, code := run(t, &fakeAPI{body: growthReportJSON}, []string{"analytics", "report"}, runOpts{})
	if want := compactJSON(t, growthReportJSON) + "\n"; code != 0 || out != want || errOut != "" {
		t.Errorf("a pipe gets the body with growth and no note: exit=%d stderr=%q\n%q", code, errOut, out)
	}
}

// The comparison line and the page lists cover the range of the report. The
// search lists cover growth.searchDays, which can be shorter.
func TestAnalyticsReportSearchWindow(t *testing.T) {
	out, errOut := runTerm(t, searchWindowReportJSON, "analytics", "report", "--days", "90")
	want := "2026-07-09 to 2026-10-06: 900 reads by people (300 readers), 120 reads by AI agents, 0 crawler visits (0 pages read by AI crawlers).\n" +
		"60 searches by people, 10 found nothing. 40 searches by agents. 0 pages not found.\n" +
		"compared with the 90 days before: reads by people +50%, reads by AI agents -20%, searches by people -25%, searches by agents 0%.\n" +
		"\nrising pages, people:\n" +
		"SLUG  TITLE           READERS  BEFORE  CHANGE  READS\n" +
		"sso   Single sign-on  40       12      +233%   95\n" +
		"\nrising searches, people (last 30 days):\n" +
		"QUERY      READERS  BEFORE  CHANGE  SEARCHES  UNANSWERED\n" +
		"okta scim  8        1       +700%   11        3\n" +
		"\n" + ruleLine
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if want := "note: rising questions, AI agents: not listed, because the 30 days before had fewer than 10 searches by AI agents\n"; errOut != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", errOut, want)
	}
}

// A server without the growth change sends no growth and no aiCrawlerHits.
// The oldest servers also send no previous. Lines 1 and 2 stay as they are.
func TestAnalyticsReportWithoutGrowth(t *testing.T) {
	line1 := "2026-09-30 to 2026-10-06: 120 reads by people (40 readers), 33 reads by AI agents, 210 crawler visits."
	line2 := "18 searches by people, 4 found nothing. 9 searches by agents. 3 pages not found."
	withPrevious := strings.Replace(todayReportJSON, `"topArticles"`, previousJSON+`,"topArticles"`, 1)
	tests := []struct {
		name, body, line3 string
	}{
		{"without previous", todayReportJSON, ""},
		{"with previous", withPrevious,
			"compared with the 7 days before: reads by people +25%, reads by AI agents 0%, searches by people 0%, searches by agents -25%."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errOut := runTerm(t, tt.body, "analytics", "report", "-w", "platform", "--days", "7")
			lines := strings.Split(out, "\n")
			if len(lines) < 4 || lines[0] != line1 || lines[1] != line2 || lines[2] != tt.line3 {
				t.Fatalf("lines 1 to 3 = %q, want %q, %q and %q", lines[:min(len(lines), 3)], line1, line2, tt.line3)
			}
			if tt.line3 != "" && lines[3] != "" {
				t.Errorf("line 4 = %q, want the blank line before the tables", lines[3])
			}
			if strings.Contains(out, "rising") || strings.Contains(out, "AI crawlers") {
				t.Errorf("a body without growth prints no rising list and no AI crawler count:\n%s", out)
			}
			if !strings.Contains(out, "\nfrom AI assistants:\n") || !strings.Contains(out, "\nnot found:\n") {
				t.Errorf("the tables of today are missing:\n%s", out)
			}
			if errOut != "" {
				t.Errorf("stderr = %q, want empty", errOut)
			}
		})
	}
}

// ContextOwl keeps daily totals for 25 months. So the report compares only a
// range of up to 365 days with the period before, as the admin Analytics page
// does.
func TestAnalyticsReportComparesUpTo365Days(t *testing.T) {
	tests := []struct {
		days        int
		from, line3 string
	}{
		{365, "2025-10-07", "compared with the 365 days before: reads by people +25%, reads by AI agents 0%, searches by people 0%, searches by agents -25%."},
		{366, "2025-10-06", ""},
		{731, "2024-10-06", ""},
	}
	for _, tt := range tests {
		t.Run(itoa(tt.days)+" days", func(t *testing.T) {
			body := strings.Replace(todayReportJSON, `"from":"2026-09-30","to":"2026-10-06","days":7,`,
				`"from":"`+tt.from+`","to":"2026-10-06","days":`+itoa(tt.days)+`,`+previousJSON+`,`, 1)
			out, errOut := runTerm(t, body, "analytics", "report", "--days", itoa(tt.days))
			lines := strings.Split(out, "\n")
			if len(lines) < 4 || !strings.HasPrefix(lines[0], tt.from+" to 2026-10-06: ") || lines[2] != tt.line3 {
				t.Errorf("want line 1 from %s and line 3 = %q:\n%s", tt.from, tt.line3, out)
			}
			if errOut != "" {
				t.Errorf("stderr = %q, want empty", errOut)
			}
		})
	}
}

func TestAnalyticsReportNotesGrowthStatus(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		notes []string
	}{
		{
			name: "a range longer than the search window",
			body: growthBody(90, 30, "no_history", "no_history", "no_baseline", "no_history"),
			notes: []string{
				"note: rising pages, people: not listed, because ContextOwl recorded reads for only part of the 90 days before",
				"note: rising pages, AI agents: not listed, because ContextOwl recorded reads for only part of the 90 days before",
				"note: rising searches, people: not listed, because the 30 days before had fewer than 10 searches by people",
				"note: rising questions, AI agents: not listed, because ContextOwl kept search text for only part of the 30 days before",
			},
		},
		{
			name: "too few reads before and search text that is not saved",
			body: growthBody(7, 7, "no_baseline", "no_baseline", "text_off", "text_off"),
			notes: []string{
				"note: rising pages, people: not listed, because the 7 days before had fewer than 10 reads by people",
				"note: rising pages, AI agents: not listed, because the 7 days before had fewer than 10 reads by AI agents",
				"note: rising searches, people: not listed, because this organization does not save search text",
				"note: rising questions, AI agents: not listed, because this organization does not save search text",
			},
		},
		{
			name: "search lists and a status that cowl does not know",
			body: growthBody(1, 1, "ready", "warming_up", "no_history", "no_baseline"),
			notes: []string{
				"note: rising searches, people: not listed, because ContextOwl kept search text for only part of the day before",
				"note: rising questions, AI agents: not listed, because the day before had fewer than 10 searches by AI agents",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errOut := runTerm(t, tt.body, "analytics", "report", "--days", "90")
			if want := strings.Join(tt.notes, "\n") + "\n"; errOut != want {
				t.Errorf("stderr:\n%s\nwant:\n%s", errOut, want)
			}
			if strings.Contains(out, "rising") {
				t.Errorf("a list that is not ready has no table, and no table needs the rule:\n%s", out)
			}
			out, errOut, code := run(t, &fakeAPI{body: tt.body}, []string{"analytics", "report"}, runOpts{})
			if code != 0 || out != compactJSON(t, tt.body)+"\n" || errOut != "" {
				t.Errorf("a pipe gets the body and no note: exit=%d stderr=%q", code, errOut)
			}
		})
	}
}

const risingNextJSON = `{"from":"2026-07-09","to":"2026-10-06","days":90,"textDays":90,"semantic":true,` +
	`"topics":[{"workspace":"platform","topic":"rate limit","variants":["rate limits"],"searches":9,"unanswered":7,"people":3,"agents":4,"reports":1,"lastSeen":"2026-10-05T10:00:00Z","closest":{"slug":"ratelimit","title":"Rate limits"}}],` +
	`"pages":[],"missing":[],"rising":{"searchDays":30,` + growthRuleJSON + `,` +
	`"people":{"status":"ready","items":[{"workspace":"platform","topic":"sso okta","variants":["okta sso"],"count":12,"previous":3,"events":20,"unanswered":6,"reports":0,"new":false,"change":300,"score":2.3,"closest":{"slug":"sso","title":"Single sign-on"}}]},` +
	`"agents":{"status":"ready","items":[{"workspace":"platform","topic":"webhook retries","variants":[],"count":8,"previous":0,"events":31,"unanswered":8,"reports":2,"new":true,"change":0,"score":2.8}]}}}`

func TestAnalyticsNextRisingTopics(t *testing.T) {
	f := &fakeAPI{body: risingNextJSON}
	out, errOut, code := run(t, f, []string{"analytics", "next", "--days", "90"}, runOpts{term: true})
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	if req := lastReq(t, f); req.Path != "/api/v1/workspaces/-/content-insights" || req.Query != "days=90" {
		t.Errorf("request = %s?%s", req.Path, req.Query)
	}
	want := "last 90 days. questions grouped by meaning.\n" +
		"\nwrite:\n" +
		"QUESTION    UNANSWERED  PEOPLE  AGENTS  REPORTS  CLOSEST\n" +
		"rate limit  7           3       4       1        ratelimit\n" +
		"\nrising topics, people (last 30 days):\n" +
		"TOPIC     READERS  BEFORE  CHANGE  SEARCHES  UNANSWERED  CLOSEST\n" +
		"sso okta  12       3       +300%   20        6           sso\n" +
		"\nrising topics, AI agents (last 30 days):\n" +
		"TOPIC            AGENTS  BEFORE  CHANGE  SEARCHES  UNANSWERED  REPORTS  CLOSEST\n" +
		"webhook retries  8       0       new     31        8           2        -\n" +
		"\n" + ruleLine
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
}

func TestAnalyticsNextNothingToDo(t *testing.T) {
	nothing := `{"days":7,"textDays":90,"semantic":false,"topics":[],"pages":[],"missing":[]`
	rising := func(people, agents string) string {
		return nothing + `,"rising":{"searchDays":7,` + growthRuleJSON + `,"people":` + people + `,"agents":` + agents + `}}`
	}
	empty := func(status string) string { return `{"status":"` + status + `","items":[]}` }
	tests := []struct {
		name, body, stdout, stderr string
	}{
		{"a server without rising topics", nothing + "}", "nothing to write, update, or fix in the last 7 days\n", ""},
		{"no rising topic", rising(empty("ready"), empty("ready")), "nothing to write, update, or fix in the last 7 days\n", ""},
		{
			"rising lists that cannot compare", rising(empty("no_baseline"), empty("text_off")),
			"nothing to write, update, or fix in the last 7 days\n",
			"note: rising topics, people: not listed, because the 7 days before had fewer than 10 searches by people\n" +
				"note: rising topics, AI agents: not listed, because this organization does not save search text\n",
		},
		{
			"only rising topics",
			rising(empty("ready"), `{"status":"ready","items":[{"workspace":"platform","topic":"sso okta","variants":[],"count":6,"previous":0,"events":9,"unanswered":0,"reports":0,"new":true,"change":0,"score":2.4}]}`),
			"last 7 days. questions grouped by wording.\n" +
				"\nrising topics, AI agents:\n" +
				"TOPIC     AGENTS  BEFORE  CHANGE  SEARCHES  UNANSWERED  REPORTS  CLOSEST\n" +
				"sso okta  6       0       new     9         0           0        -\n" +
				"\n" + ruleLine,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errOut := runTerm(t, tt.body, "analytics", "next", "--days", "7")
			if out != tt.stdout {
				t.Errorf("stdout:\n%s\nwant:\n%s", out, tt.stdout)
			}
			if errOut != tt.stderr {
				t.Errorf("stderr:\n%s\nwant:\n%s", errOut, tt.stderr)
			}
		})
	}
}

func TestChange(t *testing.T) {
	tests := []struct {
		before, now int
		want        string
	}{
		{0, 0, "none"},
		{0, 5, "new"},
		{100, 120, "+20%"},
		{20, 18, "-10%"},
		{10, 10, "0%"},
		{3, 0, "-100%"},
		{3, 4, "+33%"},
	}
	for _, tt := range tests {
		if got := change(tt.before, tt.now); got != tt.want {
			t.Errorf("change(%d, %d) = %q, want %q", tt.before, tt.now, got, tt.want)
		}
	}
	if got := daysBefore(1); got != "the day before" {
		t.Errorf("daysBefore(1) = %q", got)
	}
	if got := daysBefore(30); got != "the 30 days before" {
		t.Errorf("daysBefore(30) = %q", got)
	}
}

func TestAnalyticsHelpNamesRisingLists(t *testing.T) {
	out, _, code := run(t, &fakeAPI{}, []string{"help", "analytics"}, runOpts{})
	for _, want := range []string{"reads, what is rising, searches", "missing pages to fix, and rising topics"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("analytics help missing %q:\n%s", want, out)
		}
	}
}
