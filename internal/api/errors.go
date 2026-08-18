package api

import (
	"fmt"
	"net/http"

	"github.com/downmonitor/cli/internal/exitcode"
)

// Error is an error response from the API:
//
//	{"error": "quota_exhausted", "message": "...", "errors": {...}}
//
// The exit code is derived from the machine-readable code and the HTTP status,
// never from the wording of the message.
type Error struct {
	Status     int
	Code       string              `json:"error"`
	Message    string              `json:"message"`
	Fields     map[string][]string `json:"errors"`
	Extra      map[string]any      `json:"-"`
	RetryAfter int                 `json:"-"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}

	return fmt.Sprintf("the API answered %d (%s)", e.Status, e.Code)
}

// ExitCode implements exitcode.Coded.
//
// A rejected request is Usage; anything that means the check did not run is
// Unreachable. An API error never produces Verdict, which is reserved for the
// state of the target.
func (e *Error) ExitCode() int {
	switch e.Code {
	case "validation_failed", "unknown_region", "unsupported_check_type", "unsupported_version":
		return exitcode.Usage
	}

	switch e.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		return exitcode.Usage
	default:
		return exitcode.Unreachable
	}
}

// NothingRan reports whether the target was left untouched: a rejected key, a
// spent quota, no probe available. These are the failures --fail-open
// forgives.
func (e *Error) NothingRan() bool {
	return e.ExitCode() == exitcode.Unreachable
}

// Hints returns suggestions worth printing under the message, if the error
// code implies something the user can do about it.
func (e *Error) Hints() []string {
	switch e.Code {
	case "unauthorized":
		return []string{"Run `downmonitor login`, or set DOWNMONITOR_API_KEY."}
	case "wrong_credential":
		return []string{"check and watch take a Checks API key (dm_live_…); workspace commands take an organisation token (dmt_…)."}
	case "quota_exhausted":
		return []string{"Run `downmonitor whoami` to see your usage, or use a key on a paid plan."}
	case "no_probe_available":
		return []string{"Nothing was measured, so nothing is known about the target."}
	case "unknown_region":
		return []string{"Run `downmonitor regions` to list region codes, cities and countries."}
	case "rate_limited":
		if e.RetryAfter > 0 {
			return []string{fmt.Sprintf("Try again in %ds.", e.RetryAfter)}
		}
	}

	return nil
}

// Available returns the region codes the API reported as in service, when it
// refused a check for lack of a probe.
func (e *Error) Available() []string {
	list, _ := e.Extra["available"].([]any)
	codes := make([]string, 0, len(list))

	for _, code := range list {
		if text, ok := code.(string); ok {
			codes = append(codes, text)
		}
	}

	return codes
}
