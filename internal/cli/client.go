package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	// MissingToken marks errors that are really "no token": the server's
	// CSRF layer rejects tokenless writes as cross_origin before auth runs.
	MissingToken bool
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s: %s", e.Code, e.Message)
	if e.StatusCode == http.StatusUnauthorized || e.MissingToken {
		msg += " (run 'cowl auth login' or set COWL_PAT)"
	}
	return msg
}

// request performs one API call and returns the raw response body.
// body may be nil, json.RawMessage / []byte (sent verbatim), or any value to
// marshal. A single retry is attempted on 429, honoring Retry-After.
func (a *App) request(method, path string, query url.Values, body any) (json.RawMessage, error) {
	payload, err := encodeBody(body)
	if err != nil {
		return nil, err
	}
	raw, retryAfter, err := a.do(method, path, query, payload)
	if retryAfter > 0 {
		sleepFn(retryAfter)
		raw, _, err = a.do(method, path, query, payload)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Code == "cross_origin" && a.token == "" {
		apiErr.MissingToken = true
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
		req.Header.Set("Content-Type", "application/json")
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("cowl/%s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH))
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request %s: %w", a.baseURL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, 0, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, retryDelay(resp.Header.Get("Retry-After")), apiErrorFrom(raw, resp.StatusCode)
	}
	return nil, 0, apiErrorFrom(raw, resp.StatusCode)
}

func apiErrorFrom(raw []byte, status int) *APIError {
	var env errorEnvelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Error.Code != "" {
		return &APIError{Code: env.Error.Code, Message: env.Error.Message, StatusCode: status}
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
