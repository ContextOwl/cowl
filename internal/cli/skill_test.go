package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const skillPath = "../../skills/contextowl/SKILL.md"

// readOnlySkillTools is the exact allowed-tools list: read commands only, so
// the skill never pre-approves a write.
var readOnlySkillTools = []string{
	"Bash(cowl search *)", "Bash(cowl articles get *)", "Bash(cowl articles list *)", "Bash(cowl articles list)",
	"Bash(cowl changelog list *)", "Bash(cowl changelog list)", "Bash(cowl changelog get *)",
	"Bash(cowl proposals list *)", "Bash(cowl proposals list)", "Bash(cowl proposals get *)",
	"Bash(cowl whoami)", "Bash(cowl doctor)", "Bash(cowl insights)",
	"Bash(cowl analytics report)", "Bash(cowl analytics report *)", "Bash(cowl analytics next)", "Bash(cowl analytics next *)",
}

type skillFile struct {
	scalars map[string]string
	lists   map[string][]string
	body    string
}

// parseSkill reads the YAML front matter of SKILL.md. It handles the two
// shapes the skill uses: "key: value" and a "key:" followed by "  - item".
func parseSkill(t *testing.T, text string) skillFile {
	t.Helper()
	if !strings.HasPrefix(text, "---\n") {
		t.Fatal("SKILL.md must start with YAML front matter")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		t.Fatal("SKILL.md front matter has no closing ---")
	}
	sf := skillFile{scalars: map[string]string{}, lists: map[string][]string{}, body: text[4+end+5:]}
	var listKey string
	for _, line := range strings.Split(text[4:4+end], "\n") {
		if item, ok := strings.CutPrefix(line, "  - "); ok && listKey != "" {
			sf.lists[listKey] = append(sf.lists[listKey], strings.TrimSpace(item))
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			t.Fatalf("front matter line not understood: %q", line)
		}
		if value = strings.TrimSpace(value); value == "" {
			listKey = key
			continue
		}
		listKey = ""
		sf.scalars[key] = value
	}
	return sf
}

func TestSkillFrontMatter(t *testing.T) {
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if n := strings.Count(text, "\n"); n >= 150 {
		t.Errorf("SKILL.md has %d lines, want under 150", n)
	}
	sf := parseSkill(t, text)
	for key := range sf.scalars {
		if key != "name" && key != "description" && key != "compatibility" {
			t.Errorf("unexpected front matter key %q", key)
		}
	}
	for key := range sf.lists {
		if key != "allowed-tools" {
			t.Errorf("unexpected front matter list %q", key)
		}
	}
	if got := sf.scalars["name"]; got != "contextowl" || filepath.Base(filepath.Dir(skillPath)) != got {
		t.Errorf("name = %q, want contextowl, the name of the skill folder", got)
	}
	desc := sf.scalars["description"]
	if desc == "" || len(desc) > 1024 || strings.ContainsAny(desc, "<>") {
		t.Errorf("description must be 1 to 1024 characters without angle brackets, got %d characters", len(desc))
	}
	if !strings.Contains(desc, "Use it when") {
		t.Error("description must say when to use the skill")
	}
	compat := sf.scalars["compatibility"]
	if compat == "" || len(compat) > 500 || !strings.Contains(compat, "cowl") || !strings.Contains(compat, "agent key") {
		t.Errorf("compatibility must name cowl and the agent key in under 500 characters: %q", compat)
	}
	got := append([]string(nil), sf.lists["allowed-tools"]...)
	want := append([]string(nil), readOnlySkillTools...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("allowed-tools must list exactly the read commands:\n got %q\nwant %q", got, want)
	}
}

var inlineCode = regexp.MustCompile("`([^`]+)`")

func TestSkillNamesOnlyRealCommandsAndFlags(t *testing.T) {
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	sf := parseSkill(t, string(data))
	var commands, bareFlags []string
	inFence := false
	for _, line := range strings.Split(sf.body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			commands = append(commands, shellCommands(line)...)
			continue
		}
		for _, m := range inlineCode.FindAllStringSubmatch(line, -1) {
			switch code := m[1]; {
			case strings.HasPrefix(code, "cowl "):
				commands = append(commands, code)
			case strings.HasPrefix(code, "-"):
				bareFlags = append(bareFlags, strings.Fields(code)[0])
			}
		}
	}
	for _, tool := range sf.lists["allowed-tools"] {
		inner := strings.TrimSuffix(strings.TrimPrefix(tool, "Bash("), ")")
		commands = append(commands, strings.TrimSuffix(inner, " *"))
	}
	if len(commands) < 20 {
		t.Fatalf("found only %d cowl commands in SKILL.md; the parser is broken", len(commands))
	}
	for _, c := range commands {
		if err := checkInvocation(shellWords(c)); err != "" {
			t.Errorf("SKILL.md: %q: %s", c, err)
		}
	}
	for _, flagName := range bareFlags {
		if !flagExists(strings.TrimLeft(flagName, "-")) {
			t.Errorf("SKILL.md names flag %s, but no command has it", flagName)
		}
	}
}

// shellCommands returns the cowl commands on one line of a shell example.
func shellCommands(line string) []string {
	var out []string
	for _, seg := range splitShell(stripComment(line)) {
		if seg = strings.TrimSpace(seg); seg == "cowl" || strings.HasPrefix(seg, "cowl ") {
			out = append(out, seg)
		}
	}
	return out
}

func stripComment(line string) string {
	inQuote := rune(0)
	for i, r := range line {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			}
		case r == '\'' || r == '"':
			inQuote = r
		case r == '#' && (i == 0 || line[i-1] == ' '):
			return line[:i]
		}
	}
	return line
}

// splitShell splits a line at pipes and command separators outside quotes.
func splitShell(line string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := rune(0)
	for _, r := range line {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			}
		case r == '\'' || r == '"':
			inQuote = r
		case r == '|' || r == ';' || r == '&':
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	return append(parts, cur.String())
}

// shellWords splits a command into words and removes quotes.
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	inWord, inQuote := false, rune(0)
	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			inQuote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// checkInvocation resolves "cowl ..." words against the command table and
// returns a problem, or "" when the command and all of its flags exist.
func checkInvocation(words []string) string {
	if len(words) < 2 || words[0] != "cowl" {
		return "not a cowl command"
	}
	if words[1] == "help" {
		if len(words) > 2 && !printGroupHelp(&strings.Builder{}, words[2]) {
			return "no help topic " + words[2]
		}
		return ""
	}
	cmd, rest, err := dispatch(words[1:])
	if err != nil {
		return err.Error()
	}
	fs := cmd.flagSet(&globals{})
	for _, w := range rest {
		if w == "--" {
			break
		}
		if !strings.HasPrefix(w, "-") || w == "-" {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(w, "-"), "=")
		if fs.Lookup(name) == nil {
			return "cowl " + cmd.full() + " has no flag " + w
		}
	}
	return ""
}

func flagExists(name string) bool {
	for _, c := range allCommands() {
		if c.flagSet(&globals{}).Lookup(name) != nil {
			return true
		}
	}
	return false
}

func TestSkillCheckerCatchesMistakes(t *testing.T) {
	for _, bad := range []string{
		`cowl memory list`,
		`cowl articles get x --token cowl_pat_x`,
		`cowl search "q" --limt 5`,
		`cowl proposals`,
	} {
		if checkInvocation(shellWords(bad)) == "" {
			t.Errorf("checkInvocation(%q) found no problem", bad)
		}
	}
	if got := shellCommands(`cowl search "a | b" --limit 5 | jq . # cowl bogus`); len(got) != 1 || shellWords(got[0])[2] != "a | b" {
		t.Errorf("shellCommands = %q", got)
	}
	if flagExists("bogus") || !flagExists("w") || !flagExists("allow-shrink") {
		t.Error("flagExists is wrong")
	}
}
