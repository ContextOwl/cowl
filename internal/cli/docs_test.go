package cli

import (
	"os"
	"strings"
	"testing"
)

const readmePath = "../../README.md"

// doctorLeavesOut is what cowl doctor never prints. README.md and SKILL.md
// both say it, because both tell people to paste the report into a public
// issue.
const doctorLeavesOut = "never prints the key, the names of the key, the org or the workspaces, hosts or paths"

func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// paragraphWith returns the first paragraph of a Markdown text that holds
// needle, or "" when no paragraph holds it.
func paragraphWith(text, needle string) string {
	for _, p := range strings.Split(text, "\n\n") {
		if strings.Contains(p, needle) {
			return p
		}
	}
	return ""
}

// lineWith returns the first line of text that starts with prefix, or "".
func lineWith(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// cowl finds only the agents of agentDetection by itself, so README.md names
// each of them and tells the user of any other agent to set COWL_AGENT.
func TestReadmeNamesTheDetectedAgents(t *testing.T) {
	para := paragraphWith(readDoc(t, readmePath), "`ContextOwl-Agent`")
	if para == "" {
		t.Fatal("README.md must describe the ContextOwl-Agent header")
	}
	for _, d := range agentDetection {
		if !strings.Contains(para, "`"+d.agent+"`") {
			t.Errorf("README.md must name the agent %s, which cowl finds through %s=%s:\n%s", d.agent, d.env, d.value, para)
		}
	}
	if !strings.Contains(para, "In another agent, set `COWL_AGENT`") {
		t.Errorf("README.md must tell the user of another agent to set COWL_AGENT:\n%s", para)
	}
}

func TestDocsSayThatDoctorPrintsTheAgentName(t *testing.T) {
	out, errOut, code := run(t, &fakeAPI{body: meJSON}, []string{"doctor"}, runOpts{env: map[string]string{"COWL_AGENT": "acme-bot"}, term: true})
	if code != 0 || doctorLine(out, "agent") != "agent acme-bot (from env COWL_AGENT)" {
		t.Fatalf("cowl doctor prints the COWL_AGENT name. If that changes, change README.md, SKILL.md and this test: exit %d: %s%s", code, out, errOut)
	}
	for _, path := range []string{readmePath, skillPath} {
		para := paragraphWith(readDoc(t, path), doctorLeavesOut)
		if para == "" {
			t.Errorf("%s must say that cowl doctor %s", path, doctorLeavesOut)
			continue
		}
		for _, want := range []string{
			"It prints the agent name that cowl sends.",
			"When that name comes from `COWL_AGENT` and names a customer or a project, remove it from the issue.",
		} {
			if !strings.Contains(para, want) {
				t.Errorf("%s must say %q after what cowl doctor leaves out:\n%s", path, want, para)
			}
		}
	}
}
