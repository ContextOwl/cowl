package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// APIError is a decoded {"error":{...}} envelope from the server.
type APIError struct {
	Code       string
	Message    string
	StatusCode int
	Details    json.RawMessage
	envelope   []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// request performs one API call and returns the raw response body.
// body may be nil, json.RawMessage / []byte (sent verbatim), or any value to
// marshal. A single retry is attempted on 429, honoring Retry-After.
func (a *App) request(method, path string, query url.Values, body any) (json.RawMessage, error) {
	payload, err := encodeBody(body)
	if err != nil {
		return nil, err
	}
	return a.requestPayload(method, path, query, payload, "application/json")
}

// requestFile submits one file as multipart/form-data, keeping uploads within
// the same authentication and retry behavior as JSON API operations.
func (a *App) requestFile(method, path, filename string, content []byte) (json.RawMessage, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	file, err := mw.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(content); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return a.requestPayload(method, path, nil, body.Bytes(), mw.FormDataContentType())
}

func (a *App) requestPayload(method, path string, query url.Values, payload []byte, contentType string) (json.RawMessage, error) {
	raw, retryAfter, err := a.doWithContentType(method, path, query, payload, contentType)
	if retryAfter > 0 {
		sleepFn(retryAfter)
		raw, _, err = a.doWithContentType(method, path, query, payload, contentType)
	}
	return raw, err
}

// sleepFn is stubbed in tests so the 429 retry does not slow the suite.
var sleepFn = time.Sleep

func encodeBody(body any) ([]byte, error) {
	switch b := body.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return b, nil
	case []byte:
		return b, nil
	default:
		return json.Marshal(body)
	}
}

// do returns (body, 0, nil) on success, (nil, delay, err) when the caller
// should retry after delay, or (nil, 0, err) on a terminal error.
func (a *App) do(method, path string, query url.Values, payload []byte) (json.RawMessage, time.Duration, error) {
	return a.doWithContentType(method, path, query, payload, "application/json")
}

func (a *App) doWithContentType(method, path string, query url.Values, payload []byte, contentType string) (json.RawMessage, time.Duration, error) {
	u := a.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, u, rd)
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("cowl/%s (%s/%s)", version(), runtime.GOOS, runtime.GOARCH))
	if name, _, _ := a.agent(); name != "" {
		req.Header.Set(agentHeader, name)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, 0, networkError(a.baseURL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, 0, networkError(a.baseURL, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, 0, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, retryDelay(resp.Header.Get("Retry-After")), apiErrorFrom(raw, resp.StatusCode)
	}
	return nil, 0, apiErrorFrom(raw, resp.StatusCode)
}

func networkError(base string, err error) error {
	inner := err
	var ue *url.Error
	if errors.As(err, &ue) {
		inner = ue.Err
	}
	return &cliError{code: "network_error", message: "cannot reach " + base + ": " + inner.Error(), exit: exitServer, cause: err}
}

func apiErrorFrom(raw []byte, status int) *APIError {
	var env struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Error.Code != "" {
		return &APIError{Code: env.Error.Code, Message: env.Error.Message, StatusCode: status, Details: env.Error.Details, envelope: raw}
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = http.StatusText(status)
	}
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	return &APIError{Code: "http_" + strconv.Itoa(status), Message: msg, StatusCode: status}
}

func retryDelay(header string) time.Duration {
	header = strings.TrimSpace(header)
	if s, err := strconv.Atoi(header); err == nil && s > 0 {
		return clampDelay(time.Duration(s) * time.Second)
	}
	if t, err := http.ParseTime(header); err == nil {
		return clampDelay(time.Until(t))
	}
	return time.Second
}

func clampDelay(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	if d > 5*time.Second {
		return 5 * time.Second
	}
	return d
}

// ws is the /api/v1/workspaces/{workspace} path prefix for the resolved
// workspace; callers append url.PathEscape'd segments.
func (a *App) ws() string {
	return "/api/v1/workspaces/" + url.PathEscape(a.workspace)
}
