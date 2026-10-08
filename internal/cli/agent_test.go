package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const badAgentNote = "COWL_AGENT is not a valid agent name, so cowl sends no ContextOwl-Agent header. " +
	"Use 1 to 40 letters, digits, spaces, dots, dashes or underscores, with a letter or a digit first."

func TestAgentHeader(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no agent", nil, ""},
		{"Claude Code", map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{"another CLAUDECODE value", map[string]string{"CLAUDECODE": "0"}, ""},
		{"COWL_AGENT", map[string]string{"COWL_AGENT": "support-bot"}, "support-bot"},
		{"COWL_AGENT wins", map[string]string{"COWL_AGENT": "Support Bot 2.1_a", "CLAUDECODE": "1"}, "Support Bot 2.1_a"},
		{"COWL_AGENT is trimmed", map[string]string{"COWL_AGENT": "  support-bot  "}, "support-bot"},
		{"COWL_AGENT=none", map[string]string{"COWL_AGENT": "none", "CLAUDECODE": "1"}, ""},
		{"COWL_AGENT=NONE", map[string]string{"COWL_AGENT": "NONE", "CLAUDECODE": "1"}, ""},
		{"COWL_AGENT with a bad character", map[string]string{"COWL_AGENT": "bot<script>", "CLAUDECODE": "1"}, ""},
		{"COWL_AGENT with a line break", map[string]string{"COWL_AGENT": "bot\nX-Other: 1"}, ""},
		{"COWL_AGENT that starts with a dot", map[string]string{"COWL_AGENT": ".bot"}, ""},
		{"COWL_AGENT of 40 characters", map[string]string{"COWL_AGENT": strings.Repeat("a", 40)}, strings.Repeat("a", 40)},
		{"COWL_AGENT of 41 characters", map[string]string{"COWL_AGENT": strings.Repeat("a", 41)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: `{"semantic":false,"results":[]}`}
			if _, errOut, code := run(t, f, []string{"search", "sso"}, runOpts{env: tt.env}); code != 0 {
				t.Fatalf("a bad agent name must never stop a command: exit %d: %s", code, errOut)
			}
			if got := lastReq(t, f).Agent; got != tt.want {
				t.Errorf("%s = %q, want %q", agentHeader, got, tt.want)
			}
		})
	}
}

// Every request of every command goes through one function, so each request
// carries the header: reads, writes, uploads, cowl api, cowl doctor and cowl
// auth login.
func TestAgentHeaderOnEveryRequest(t *testing.T) {
	image := filepath.Join(t.TempDir(), "diagram.png")
	if err := os.WriteFile(image, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"search", "sso"},
		{"articles", "get", "api-keys"},
		{"articles", "update", "intro", "--status", "STABLE"},
		{"landing", "set", "--file", "-"},
		{"changelog", "delete", "7", "--yes"},
		{"analytics", "gap", "How do I rotate a key?"},
		{"insights"},
		{"analytics", "questions", "--all"},
		{"uploads", "image", image},
		{"api", "GET", "workspaces"},
		{"whoami"},
		{"auth", "status"},
		{"doctor"},
		{"auth", "login", "--with-token"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &fakeAPI{body: meJSON}
			env := map[string]string{"CLAUDECODE": "1"}
			stdin := `{"headline":"New"}`
			if args[0] == "auth" && args[1] == "login" {
				stdin = testToken + "\n"
			}
			run(t, f, args, runOpts{env: env, stdin: stdin})
			reqs := f.requests()
			if len(reqs) == 0 {
				t.Fatal("no request reached the API")
			}
			for _, r := range reqs {
				if r.Agent != "claude-code" {
					t.Errorf("%s %s: %s = %q, want claude-code", r.Method, r.Path, agentHeader, r.Agent)
				}
			}
		})
	}
}

func TestDoctorAgent(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		agent string
		from  string
		line  string
		note  bool
	}{
		{"no agent", nil, "none", "", "agent none", false},
		{"Claude Code", map[string]string{"CLAUDECODE": "1"}, "claude-code", "env CLAUDECODE", "agent claude-code (from env CLAUDECODE)", false},
		{"COWL_AGENT", map[string]string{"COWL_AGENT": "support-bot", "CLAUDECODE": "1"}, "support-bot", "env COWL_AGENT", "agent support-bot (from env COWL_AGENT)", false},
		{"COWL_AGENT=none", map[string]string{"COWL_AGENT": "none", "CLAUDECODE": "1"}, "none", "env COWL_AGENT", "agent none (from env COWL_AGENT)", false},
		{"bad COWL_AGENT", map[string]string{"COWL_AGENT": "bot!", "CLAUDECODE": "1"}, "none", "env COWL_AGENT", "agent none (from env COWL_AGENT)", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: meJSON}
			out, errOut, code := run(t, f, []string{"doctor"}, runOpts{env: tt.env})
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			rep := doctorJSON(t, out)
			if rep.Agent != tt.agent || rep.AgentFrom != tt.from || slices.Contains(rep.Notes, badAgentNote) != tt.note {
				t.Errorf("report = %+v, want agent %q from %q and the note %v", rep, tt.agent, tt.from, tt.note)
			}
			if tt.from == "" && strings.Contains(out, "agentFrom") {
				t.Errorf("the report must leave out agentFrom without a source:\n%s", out)
			}
			term, _, _ := run(t, f, []string{"doctor"}, runOpts{env: tt.env, term: true})
			if got := doctorLine(term, "agent"); got != tt.line {
				t.Errorf("agent line = %q, want %q\n%s", got, tt.line, term)
			}
		})
	}
	t.Run("a bad name is the only problem", func(t *testing.T) {
		healthy := strings.NewReplacer(`"expiresAt":"2026-11-03T09:12:00Z"`, `"expiresAt":null`,
			`,"blocked":[{"permission":"workspace.create","reason":"plan"}]`, `,"blocked":[]`).Replace(meJSON)
		out, _, _ := run(t, &fakeAPI{body: healthy}, []string{"doctor"}, runOpts{})
		if rep := doctorJSON(t, out); !slices.Equal(rep.Notes, []string{"No problems found."}) {
			t.Fatalf("the fixture must be healthy: notes = %q", rep.Notes)
		}
		out, _, _ = run(t, &fakeAPI{body: healthy}, []string{"doctor"}, runOpts{env: map[string]string{"COWL_AGENT": "bot!"}})
		if rep := doctorJSON(t, out); !slices.Equal(rep.Notes, []string{badAgentNote}) {
			t.Errorf("notes = %q, want only the agent note", rep.Notes)
		}
	})
	t.Run("without a key", func(t *testing.T) {
		out, _, _ := run(t, &fakeAPI{}, []string{"doctor"}, runOpts{env: map[string]string{"CONTEXTOWL_PAT": "", "CLAUDECODE": "1"}})
		if rep := doctorJSON(t, out); rep.Agent != "claude-code" || rep.API.Result != "skipped" {
			t.Errorf("doctor names the agent before it calls the API: %+v", rep)
		}
	})
}

// doctorLine returns the line of a terminal doctor report with label, with
// its whitespace collapsed, or "" when the report has no such line.
func doctorLine(out, label string) string {
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == label {
			return strings.Join(fields, " ")
		}
	}
	return ""
}

func TestRootHelpNamesCowlAgent(t *testing.T) {
	out, _, code := run(t, &fakeAPI{}, []string{"help"}, runOpts{})
	if code != 0 || !strings.Contains(out, "COWL_AGENT names the AI agent that runs cowl") || !strings.Contains(out, "COWL_AGENT=none sends no name") {
		t.Errorf("root help must name COWL_AGENT:\n%s", out)
	}
}
