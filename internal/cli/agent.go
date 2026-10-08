package cli

import (
	"regexp"
	"strings"
)

// agentHeader names the AI agent that runs cowl. ContextOwl analytics then
// counts the call as that agent, not as cowl. The header grants nothing.
const agentHeader = "ContextOwl-Agent"

// agentEnvVar sets the agent name. The value none turns the header off.
const agentEnvVar = "COWL_AGENT"

// agentNameRE is the form of an agent name that the server accepts: 1 to 40
// letters, digits, spaces, dots, dashes or underscores, with a letter or a
// digit first. The server ignores a name of another form.
var agentNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,39}$`)

// agentDetection is the one table that finds the AI agent that runs cowl
// when COWL_AGENT is not set. Each row names an environment variable that the
// agent sets for the commands that it runs, the value of that variable, and
// the name that cowl sends.
var agentDetection = []struct{ env, value, agent string }{
	{"CLAUDECODE", "1", "claude-code"},
}

// agent returns the name that cowl sends in the ContextOwl-Agent header, or
// "" for no header, and the source of that choice. COWL_AGENT wins over the
// detection table. valid is false when COWL_AGENT holds a value that is not
// an agent name. cowl then sends no header, so a bad value never stops a
// command.
func (a *App) agent() (name, from string, valid bool) {
	if v := a.env(agentEnvVar); v != "" {
		from = "env " + agentEnvVar
		switch {
		case strings.EqualFold(v, "none"):
			return "", from, true
		case !agentNameRE.MatchString(v):
			return "", from, false
		}
		return v, from, true
	}
	for _, d := range agentDetection {
		if a.env(d.env) == d.value {
			return d.agent, "env " + d.env, true
		}
	}
	return "", "", true
}

// checkAgent adds the agent name that cowl sends to the doctor report, and a
// note when COWL_AGENT holds a value that the server does not accept.
func (r *doctorReport) checkAgent(a *App) {
	name, from, valid := a.agent()
	r.Agent, r.AgentFrom = firstOf(name, "none"), from
	if !valid {
		r.note("COWL_AGENT is not a valid agent name, so cowl sends no ContextOwl-Agent header. " +
			"Use 1 to 40 letters, digits, spaces, dots, dashes or underscores, with a letter or a digit first.")
	}
}

// agentLabel is the agent line of the doctor report on a terminal.
func (r doctorReport) agentLabel() string {
	if r.AgentFrom == "" {
		return r.Agent
	}
	return r.Agent + " (from " + r.AgentFrom + ")"
}
