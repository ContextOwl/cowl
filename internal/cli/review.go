package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// An org can review the changes that agent keys make to live content. A
// write under review answers 202 with the proposal that holds the change,
// and nothing changes for readers until an editor approves it.

// noteHelp is the help of --note on each write that the org can review.
const noteHelp = "note for the reviewer when the org reviews agent changes: what the change does and why. A direct write ignores it"

// noteBody adds --note to the body of a write. cowl sends it only when the
// user sets it, because an older server rejects an unknown body field.
func noteBody(body map[string]any, note string) {
	if note = strings.TrimSpace(note); note != "" {
		body["note"] = note
	}
}

// noteQuery is the query of a write that takes --note as a query parameter,
// because its body cannot carry it. An older server ignores the parameter.
func noteQuery(note string) url.Values {
	if note = strings.TrimSpace(note); note != "" {
		return url.Values{"note": {note}}
	}
	return nil
}

// pendingReview is the 202 body of a write that the org reviews.
type pendingReview struct {
	Pending  bool `json:"pendingReview"`
	Proposal struct {
		ID        int64  `json:"id"`
		Summary   string `json:"summary"`
		ReviewURL string `json:"reviewUrl"`
		Outcome   string `json:"outcome"`
	} `json:"proposal"`
}

// receipt says what happened to the proposal. The outcome tells a new
// proposal from the working copy of the key that the write changed.
func (p pendingReview) receipt() string {
	line := fmt.Sprintf("proposal %d ", p.Proposal.ID)
	switch p.Proposal.Outcome {
	case "updated":
		line += "now holds this change too and waits for review"
	case "unchanged":
		line += "already holds this change and waits for review"
	case "rebased":
		line += "now holds only this change and waits for review"
	default:
		line += "waits for review"
	}
	if p.Proposal.ReviewURL != "" {
		line += ": " + p.Proposal.ReviewURL
	}
	return line
}

// emitWrite prints the answer of a write that the org can review: the
// receipt of human, or the proposal that holds the change when the server
// answers 202. The command then exits 0, because the server accepted the
// change.
func (a *App) emitWrite(raw []byte, httpStatus int, human func() error) error {
	if httpStatus == http.StatusAccepted {
		return a.emitPending(raw)
	}
	return a.emit(raw, human)
}

// emitWriteAs is emitWrite for a receipt that decodes the response into v.
func emitWriteAs[T any](a *App, raw []byte, httpStatus int, human func(v T) error) error {
	if httpStatus == http.StatusAccepted {
		return a.emitPending(raw)
	}
	return emitAs(a, raw, human)
}

// emitPending prints the proposal of a write under review. In a pipe and
// with --json, it prints the REST body, which has pendingReview true. A 202
// body without a proposal prints as JSON.
func (a *App) emitPending(raw []byte) error {
	return emitAs(a, raw, func(p pendingReview) error {
		if !p.Pending || p.Proposal.ID == 0 {
			return a.printJSON(raw)
		}
		fmt.Fprintln(a.Out, p.receipt())
		if summary := strings.Join(strings.Fields(p.Proposal.Summary), " "); summary != "" {
			fmt.Fprintln(a.Out, "  "+summary)
		}
		if p.Proposal.Outcome == "rebased" {
			fmt.Fprintf(a.Err, "note: the live content changed after the earlier change of this key, so proposal %d no longer holds the earlier change.\n", p.Proposal.ID)
		}
		return nil
	})
}

// withdrawnProposal is the pending proposal of the key that an article
// update withdrew, because the change needs no review now.
type withdrawnProposal struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// note explains the withdrawal to a person.
func (w withdrawnProposal) note() string {
	switch w.Reason {
	case "draft":
		return fmt.Sprintf("note: this change withdrew proposal %d, because it changes only a draft, which saved directly. "+
			"If that proposal held a publish request, run the update again with --status to file it.\n", w.ID)
	case "live":
		return fmt.Sprintf("note: this change withdrew proposal %d, because the change now matches the live article.\n", w.ID)
	}
	return fmt.Sprintf("note: this change withdrew proposal %d.\n", w.ID)
}

// writeFields describes how the changes of a key to live content apply. An
// older server sends neither writes nor publishingKey, and then the list is
// empty.
func writeFields(me meInfo) [][2]string {
	var out [][2]string
	if me.Writes != "" {
		out = append(out, [2]string{"writes:", writesLabel(me.Writes)})
	}
	if me.PublishingKey != nil && *me.PublishingKey {
		out = append(out, [2]string{"publishing key:", "yes"})
	}
	return out
}

func writesLabel(writes string) string {
	switch writes {
	case "none":
		return "none, the key cannot change content that readers see"
	case "review":
		return "review, changes to live content wait for an editor"
	case "direct":
		return "direct, changes to live content apply at once"
	}
	return writes
}

// reviewNote is the doctor note for a key whose changes wait for review.
const reviewNote = "The organization reviews agent changes, so a change of this key to live content waits in a proposal for an editor. " +
	"An admin can approve the key as a publishing key in Admin > Settings > API."
