package cli

import (
	"encoding/json"
	"time"
)

const apiKeyPrefix = "cowl_pat_"

func apiKeyDisplayPrefix(token string) string {
	const n = len(apiKeyPrefix) + 6
	if len(token) < n {
		return token
	}
	return token[:n]
}

// errorEnvelope is the REST error shape. cowl writes the same shape for
// failures it finds itself, with status 0.
type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Status  int            `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

type meInfo struct {
	Key struct {
		Name      string     `json:"name"`
		Prefix    string     `json:"prefix"`
		ExpiresAt *time.Time `json:"expiresAt"`
	} `json:"key"`
	Org struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Plan string `json:"plan"`
	} `json:"org"`
	Role           string        `json:"role"`
	BoundWorkspace *string       `json:"boundWorkspace"`
	Workspaces     []meWorkspace `json:"workspaces"`
	Permissions    []string      `json:"permissions"`
	Blocked        []blockedPerm `json:"blocked"`
	// ReadsDrafts is nil when the server is older than the draft rule.
	ReadsDrafts *bool `json:"readsDrafts"`
}

type meWorkspace struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AccessMode string `json:"accessMode"`
}

type blockedPerm struct {
	Permission string `json:"permission"`
	Reason     string `json:"reason"`
}

func (m meInfo) reaches(workspace string) bool {
	for _, w := range m.Workspaces {
		if w.ID == workspace {
			return true
		}
	}
	return false
}

func (m meInfo) workspaceIDs() []string {
	ids := make([]string, 0, len(m.Workspaces))
	for _, w := range m.Workspaces {
		ids = append(ids, w.ID)
	}
	return ids
}

func decodeMe(raw json.RawMessage) (meInfo, error) {
	var me meInfo
	if err := json.Unmarshal(raw, &me); err != nil {
		return me, invalidResponse(err)
	}
	return me, nil
}
