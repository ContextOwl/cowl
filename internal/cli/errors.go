package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Exit codes. Agents branch on them, so they are part of the CLI contract.
const (
	exitOK        = 0
	exitError     = 1
	exitUsage     = 2
	exitNotFound  = 3
	exitAuth      = 4
	exitConflict  = 5
	exitRateLimit = 6
	exitServer    = 7
)

type usageError string

func (e usageError) Error() string { return string(e) }

// cliError is a failure that is not an HTTP error, such as a missing key, a
// network failure or a body that cowl cannot read. It uses the error
// envelope of the server with status 0.
type cliError struct {
	code    string
	message string
	exit    int
	cause   error
}

func (e *cliError) Error() string { return e.code + ": " + e.message }
func (e *cliError) Unwrap() error { return e.cause }

func noKeyError() error {
	return &cliError{code: "no_key", message: "no agent key: run 'cowl auth login', or set CONTEXTOWL_PAT", exit: exitAuth}
}

func invalidResponse(err error) error {
	return &cliError{code: "invalid_response", message: "cowl cannot read the server response: " + err.Error(), exit: exitError, cause: err}
}

func exitCode(err error) int {
	var u usageError
	var c *cliError
	var a *APIError
	switch {
	case errors.As(err, &u):
		return exitUsage
	case errors.As(err, &c):
		return c.exit
	case errors.As(err, &a):
		return httpExitCode(a.StatusCode)
	}
	return exitError
}

func httpExitCode(status int) int {
	switch {
	case status == http.StatusBadRequest, status == http.StatusRequestEntityTooLarge, status == http.StatusUnprocessableEntity:
		return exitUsage
	case status == http.StatusNotFound:
		return exitNotFound
	case status == http.StatusUnauthorized, status == http.StatusPaymentRequired, status == http.StatusForbidden:
		return exitAuth
	case status == http.StatusConflict:
		return exitConflict
	case status == http.StatusTooManyRequests:
		return exitRateLimit
	case status >= 500:
		return exitServer
	}
	return exitError
}

// fail writes err to stderr and returns the exit code. On a terminal the
// error is text. Otherwise it is one JSON line: the envelope of the server
// as sent, or the same shape that cowl builds. A failure that is not an HTTP
// error has status 0. usage is the synopsis of the command, or "" when no
// command was resolved.
func (a *App) fail(err error, usage string) int {
	if a.ErrTTY {
		a.writeErrorText(err, usage)
	} else {
		a.writeErrorJSON(err, usage)
	}
	return exitCode(err)
}

func (a *App) writeErrorJSON(err error, usage string) {
	var apiErr *APIError
	if errors.As(err, &apiErr) && len(apiErr.envelope) > 0 {
		var buf bytes.Buffer
		if json.Compact(&buf, apiErr.envelope) == nil {
			buf.WriteByte('\n')
			a.Err.Write(buf.Bytes())
			return
		}
	}
	env := errorEnvelope{Error: errorDetail{Code: "error", Message: err.Error()}}
	var u usageError
	var c *cliError
	switch {
	case errors.As(err, &u):
		env.Error.Code, env.Error.Message = "usage", string(u)
		if usage != "" {
			env.Error.Details = map[string]any{"usage": usage}
		}
	case errors.As(err, &c):
		env.Error.Code, env.Error.Message = c.code, c.message
	case apiErr != nil:
		env.Error.Code, env.Error.Message, env.Error.Status = apiErr.Code, apiErr.Message, apiErr.StatusCode
	}
	line, _ := json.Marshal(env)
	a.Err.Write(append(line, '\n'))
}

func (a *App) writeErrorText(err error, usage string) {
	var u usageError
	var c *cliError
	var apiErr *APIError
	switch {
	case errors.As(err, &u):
		fmt.Fprintf(a.Err, "cowl: %s\n", u)
		if usage != "" {
			fmt.Fprintf(a.Err, "usage: %s\n", usage)
		} else {
			fmt.Fprintln(a.Err, "Run 'cowl help' for usage.")
		}
	case errors.As(err, &apiErr):
		fmt.Fprintf(a.Err, "cowl: %s: %s\n", apiErr.Code, apiErr.Message)
		for _, h := range apiErrorHints(apiErr) {
			fmt.Fprintf(a.Err, "  %s\n", h)
		}
	case errors.As(err, &c):
		fmt.Fprintf(a.Err, "cowl: %s: %s\n", c.code, c.message)
	default:
		fmt.Fprintf(a.Err, "cowl: %s\n", err)
	}
}

// apiErrorHints turns the details of an error into short next steps for a
// person at a terminal. Agents read the details from the JSON envelope.
func apiErrorHints(e *APIError) []string {
	var hints []string
	details := map[string]json.RawMessage{}
	if len(e.Details) > 0 {
		_ = json.Unmarshal(e.Details, &details)
	}
	var suggestions []struct {
		Slug string `json:"slug"`
	}
	if json.Unmarshal(details["suggestions"], &suggestions) == nil && len(suggestions) > 0 {
		slugs := make([]string, 0, len(suggestions))
		for _, s := range suggestions {
			slugs = append(slugs, s.Slug)
		}
		hints = append(hints, "did you mean: "+strings.Join(slugs, ", "))
	}
	if list := stringList(details["anchors"]); len(list) > 0 {
		hints = append(hints, "anchors: "+strings.Join(list, ", "))
	}
	if list := stringList(details["allowed"]); len(list) > 0 {
		hints = append(hints, "allowed: "+strings.Join(list, ", "))
	}
	var current string
	if json.Unmarshal(details["currentRevision"], &current) == nil && current != "" {
		hints = append(hints, "current revision: "+current)
	}
	edit := "an edit"
	var index int
	if json.Unmarshal(details["index"], &index) == nil {
		edit = fmt.Sprintf("edits[%d]", index)
	}
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		hints = append(hints, "Run 'cowl auth login', or set CONTEXTOWL_PAT.")
	case e.Code == "stale_revision":
		hints = append(hints, "Read the article again, then do the change again on the current revision.")
	case e.Code == "large_removal":
		hints = append(hints, "To remove this much text, pass --allow-shrink.")
	case e.Code == "slug_taken":
		hints = append(hints, "Pass a different --slug, or leave out --slug to get a free slug.")
	case e.Code == "edit_not_found":
		hints = append(hints, "Read the article again and copy the old text of "+edit+" exactly.")
	case e.Code == "edit_ambiguous":
		hints = append(hints, "Add text around the old text of "+edit+" so that it occurs only once.")
	}
	return hints
}

// stringList reads a JSON array of strings, or of objects with a key or a
// slug, as plain strings.
func stringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var plain []string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var objs []struct {
		Key  string `json:"key"`
		Slug string `json:"slug"`
	}
	if json.Unmarshal(raw, &objs) != nil {
		return nil
	}
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		if v := firstOf(o.Key, o.Slug); v != "" {
			out = append(out, v)
		}
	}
	return out
}
