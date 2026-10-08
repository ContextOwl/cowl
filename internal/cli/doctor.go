package cli

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// doctorTimeout bounds the one API call of cowl doctor.
var doctorTimeout = 5 * time.Second

// doctorReport is safe to paste into a public issue: it never holds the key,
// its prefix, a name, a workspace id, the base URL or the config path.
type doctorReport struct {
	Version     string        `json:"version"`
	OS          string        `json:"os"`
	Arch        string        `json:"arch"`
	Config      string        `json:"config"`
	BaseURL     string        `json:"baseUrl"`
	Token       string        `json:"token"`
	API         doctorAPI     `json:"api"`
	Role        string        `json:"role,omitempty"`
	Plan        string        `json:"plan,omitempty"`
	KeyScope    string        `json:"keyScope,omitempty"`
	Workspaces  *int          `json:"workspaces,omitempty"`
	Permissions []string      `json:"permissions,omitempty"`
	Blocked     []blockedPerm `json:"blocked,omitempty"`
	ReadsDrafts *bool         `json:"readsDrafts,omitempty"`
	Notes       []string      `json:"notes"`
}

type doctorAPI struct {
	Result    string `json:"result"`
	Status    int    `json:"status,omitempty"`
	Code      string `json:"code,omitempty"`
	ElapsedMS int64  `json:"elapsedMs,omitempty"`
}

func (r *doctorReport) note(s string) { r.Notes = append(r.Notes, s) }

func cmdDoctor() *Command {
	return &Command{
		Name: "doctor", OpIDs: []string{"getMe"}, Local: true,
		Summary: "Check the setup and the connection, and print a report that is safe to share",
		Usage:   "cowl doctor",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			rep := a.diagnose()
			if a.jsonOut() {
				return a.printValue(rep)
			}
			rep.print(a)
			return nil
		},
	}
}

func (a *App) diagnose() doctorReport {
	rep := doctorReport{Version: version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Config: "ok", Token: "none", API: doctorAPI{Result: "skipped"}}
	if err := a.loadConfigFile(); err != nil {
		rep.Config = "invalid"
		rep.note("cowl cannot read the config file. Run 'cowl auth login' to write a new one.")
	} else if !a.cfgFound {
		rep.Config = "none"
	}
	baseErr := a.applySettings()
	if a.tokenFrom != "" {
		rep.Token = a.tokenFrom
	}
	switch {
	case baseErr == nil:
		rep.BaseURL = baseURLKind(a.baseURL)
	case a.baseFrom == fromConfig:
		rep.BaseURL = "invalid"
		rep.note("The base URL in the config file is not valid. Run 'cowl auth login --base-url URL' to replace it.")
	default:
		rep.BaseURL = "invalid"
		rep.note("CONTEXTOWL_BASE_URL or COWL_BASE_URL is not a valid base URL. Set an absolute http or https URL without credentials, a query or a fragment.")
	}
	if a.token == "" {
		rep.note("No agent key. Run 'cowl auth login', or set CONTEXTOWL_PAT.")
	}
	if baseErr != nil || a.token == "" {
		return rep
	}
	if err := a.checkKey(); err != nil {
		if a.tokenFrom == fromConfig {
			rep.note("The saved key belongs to a different host than CONTEXTOWL_BASE_URL or COWL_BASE_URL, so cowl sends no request. Unset the variable, or run 'cowl auth login' for that host.")
		} else {
			rep.note("The key from the environment goes only to the default host or to CONTEXTOWL_BASE_URL, but the base URL comes from the config file, so cowl sends no request. Set CONTEXTOWL_BASE_URL.")
		}
		return rep
	}
	a.http = &http.Client{Timeout: doctorTimeout}
	start := time.Now()
	raw, _, err := a.do("GET", "/api/v1/me", nil, nil)
	rep.API.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		rep.checkFailure(err)
		return rep
	}
	me, err := decodeMe(raw)
	if err != nil {
		rep.API = doctorAPI{Result: "invalid_response", Status: http.StatusOK, ElapsedMS: rep.API.ElapsedMS}
		rep.note("The server answered with a body that cowl cannot read. Check that the base URL points to ContextOwl.")
		return rep
	}
	rep.API.Result, rep.API.Status = "ok", http.StatusOK
	rep.checkKeyInfo(me)
	return rep
}

func (r *doctorReport) checkFailure(err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		r.API.Result, r.API.Status, r.API.Code = "http_error", apiErr.StatusCode, apiErr.Code
		switch s := apiErr.StatusCode; {
		case s == http.StatusUnauthorized:
			r.note("The server does not accept the key. An admin can revoke a key, and a key can expire. Create a new key in Admin > Settings > API, then run 'cowl auth login'.")
		case s == http.StatusNotFound:
			r.note("The server has no GET /api/v1/me. Update the server, or check the base URL.")
		case s == http.StatusTooManyRequests:
			r.note("The server limits the request rate of this key. Wait one minute, then try again.")
		case s >= 500:
			r.note("The server failed. Try again later. If the problem continues, report it with this output.")
		default:
			r.note("The server refused the request. Check the key in Admin > Settings > API.")
		}
		return
	}
	r.API.Result, r.API.Code = "network_error", networkKind(err)
	switch r.API.Code {
	case "timeout":
		r.note(fmt.Sprintf("The API did not answer in %s. Check the network, the VPN or proxy, and the base URL.", doctorTimeout))
	case "dns":
		r.note("The host name of the API does not resolve. Check the base URL and the network.")
	case "refused":
		r.note("The host refused the connection. Check that the server runs and that the base URL is correct.")
	case "tls":
		r.note("The TLS handshake failed. Check the certificate of the server and the system clock.")
	default:
		r.note("cowl cannot reach the API. Check the network and the base URL.")
	}
}

func (r *doctorReport) checkKeyInfo(me meInfo) {
	r.Role, r.Plan = me.Role, me.Org.Plan
	r.KeyScope = "org"
	if me.BoundWorkspace != nil {
		r.KeyScope = "workspace"
	}
	n := len(me.Workspaces)
	r.Workspaces = &n
	r.Permissions, r.Blocked, r.ReadsDrafts = me.Permissions, me.Blocked, me.ReadsDrafts
	if n == 0 {
		r.note("The key cannot reach a workspace. Ask an admin to check the scope of the key.")
	}
	var plan, twoFactor, role []string
	for _, b := range me.Blocked {
		switch b.Reason {
		case "plan":
			plan = append(plan, b.Permission)
		case "two_factor":
			twoFactor = append(twoFactor, b.Permission)
		default:
			role = append(role, b.Permission)
		}
	}
	if len(plan) > 0 {
		r.note("The plan blocks these permissions of the key: " + strings.Join(plan, ", ") + ".")
	}
	if len(twoFactor) > 0 {
		r.note("The two-factor policy of the organization blocks these permissions of the key: " + strings.Join(twoFactor, ", ") +
			". The key owner must turn on two-factor authentication in the account settings.")
	}
	if len(role) > 0 {
		r.note("The role of the key owner blocks these permissions of the key: " + strings.Join(role, ", ") + ".")
	}
	if exp := me.Key.ExpiresAt; exp != nil && time.Until(*exp) < 7*24*time.Hour {
		r.note("The key expires on " + exp.UTC().Format("2006-01-02") + ". Create a new key before then.")
	}
	if len(r.Notes) == 0 {
		r.note("No problems found.")
	}
}

// networkKind names the class of a network failure without the host name.
func networkKind(err error) string {
	var netErr net.Error
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var recordErr tls.RecordHeaderError
	switch {
	case errors.As(err, &dnsErr):
		return "dns"
	case errors.As(err, &netErr) && netErr.Timeout(), errors.Is(err, os.ErrDeadlineExceeded):
		return "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.As(err, &certErr), errors.As(err, &unknownAuth), errors.As(err, &hostErr), errors.As(err, &recordErr):
		return "tls"
	}
	return "network"
}

func (r doctorReport) print(a *App) {
	api := r.API.Result
	switch r.API.Result {
	case "ok":
		api = fmt.Sprintf("ok, HTTP %d in %d ms", r.API.Status, r.API.ElapsedMS)
	case "http_error":
		api = fmt.Sprintf("HTTP %d %s in %d ms", r.API.Status, r.API.Code, r.API.ElapsedMS)
	case "network_error":
		api = "network error: " + r.API.Code
	}
	pairs := [][2]string{
		{"version", r.Version},
		{"platform", r.OS + "/" + r.Arch},
		{"config", r.Config},
		{"base url", r.BaseURL},
		{"token", r.Token},
		{"api", api},
	}
	if r.API.Result == "ok" {
		blocked := make([]string, 0, len(r.Blocked))
		for _, b := range r.Blocked {
			blocked = append(blocked, b.Permission+" ("+b.Reason+")")
		}
		pairs = append(pairs,
			[2]string{"role", r.Role},
			[2]string{"plan", r.Plan},
			[2]string{"key scope", r.KeyScope},
			[2]string{"workspaces", fmt.Sprint(*r.Workspaces)},
			[2]string{"permissions", dash(strings.Join(r.Permissions, ", "))},
			[2]string{"blocked", dash(strings.Join(blocked, ", "))},
		)
		if r.ReadsDrafts != nil {
			pairs = append(pairs, [2]string{"drafts", draftsLabel(*r.ReadsDrafts)})
		}
	}
	fmt.Fprintln(a.Out, "cowl doctor")
	a.fields(pairs)
	fmt.Fprintln(a.Out, "\nnotes")
	for _, n := range r.Notes {
		fmt.Fprintf(a.Out, "- %s\n", n)
	}
}
