package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/downmonitor/cli/internal/exitcode"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return New(server.URL+"/", "dm_live_test")
}

func TestClientSendsHeadersAndDecodesTheResponse(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/checks" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}

		for header, want := range map[string]string{
			"Authorization": "Bearer dm_live_test",
			"X-Api-Version": ContractVersion,
			"Content-Type":  "application/json",
			"Accept":        "application/json",
		} {
			if got := r.Header.Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}

		var body CheckRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Target != "https://example.com" {
			t.Errorf("body = %+v, err = %v", body, err)
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"ck_1","verdict":"up","passed":true}`))
	})

	check, err := client.Check(context.Background(), CheckRequest{Target: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if check.ID != "ck_1" || check.Verdict != "up" || !check.Passed {
		t.Errorf("check = %+v", check)
	}
}

func TestFetchCheckEscapesTheID(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/checks/ck%2F1" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}

		_, _ = w.Write([]byte(`{"id":"ck/1"}`))
	})

	if _, err := client.FetchCheck(context.Background(), "ck/1"); err != nil {
		t.Fatal(err)
	}
}

func TestErrorEnvelopeIsKeptAsIs(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"no_probe_available","message":"No region could run the check.","available":["cdg","nyc"]}`))
	})

	_, err := client.Regions(context.Background())

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v, want *Error", err, err)
	}

	if apiErr.Code != "no_probe_available" || apiErr.Message != "No region could run the check." {
		t.Errorf("apiErr = %+v", apiErr)
	}

	if apiErr.RetryAfter != 7 {
		t.Errorf("RetryAfter = %d", apiErr.RetryAfter)
	}

	if got := apiErr.Available(); len(got) != 2 || got[0] != "cdg" {
		t.Errorf("Available() = %v", got)
	}

	if exitcode.Of(err) != exitcode.Unreachable || !apiErr.NothingRan() {
		t.Errorf("exit code = %d", exitcode.Of(err))
	}
}

func TestNonJSONErrorStillHasACode(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>Bad gateway</html>"))
	})

	_, err := client.Me(context.Background())

	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "http_502" {
		t.Fatalf("err = %v", err)
	}

	if exitcode.Of(err) != exitcode.Unreachable {
		t.Errorf("exit code = %d, want %d", exitcode.Of(err), exitcode.Unreachable)
	}
}

func TestUnreadableSuccessIsUnreachable(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})

	if _, err := client.Me(context.Background()); exitcode.Of(err) != exitcode.Unreachable {
		t.Errorf("err = %v, exit code = %d", err, exitcode.Of(err))
	}
}

func TestConnectionFailureIsUnreachable(t *testing.T) {
	client := New("http://127.0.0.1:1", "")

	if _, err := client.Regions(context.Background()); exitcode.Of(err) != exitcode.Unreachable {
		t.Errorf("err = %v, exit code = %d", err, exitcode.Of(err))
	}
}

func TestErrorExitCodes(t *testing.T) {
	tests := []struct {
		code   string
		status int
		want   int
	}{
		{"validation_failed", 422, exitcode.Usage},
		{"unknown_region", 422, exitcode.Usage},
		{"http_404", 404, exitcode.Usage},
		{"unauthorized", 401, exitcode.Unreachable},
		{"quota_exhausted", 402, exitcode.Unreachable},
		{"wrong_credential", 403, exitcode.Unreachable},
		{"rate_limited", 429, exitcode.Unreachable},
		{"server_error", 500, exitcode.Unreachable},
	}

	for _, tt := range tests {
		err := &Error{Code: tt.code, Status: tt.status}
		if got := err.ExitCode(); got != tt.want {
			t.Errorf("%s (%d): ExitCode() = %d, want %d", tt.code, tt.status, got, tt.want)
		}
	}
}

func TestHints(t *testing.T) {
	if hints := (&Error{Code: "rate_limited", RetryAfter: 30}).Hints(); len(hints) != 1 || hints[0] != "Try again in 30s." {
		t.Errorf("hints = %v", hints)
	}

	if hints := (&Error{Code: "rate_limited"}).Hints(); len(hints) != 0 {
		t.Errorf("hints = %v, want none without Retry-After", hints)
	}

	if hints := (&Error{Code: "something_else"}).Hints(); hints != nil {
		t.Errorf("hints = %v", hints)
	}
}

func TestKindOf(t *testing.T) {
	tests := map[string]Kind{
		"dm_live_abc": KindChecks,
		"dm_test_abc": KindChecks,
		"dmt_abc":     KindOrganisation,
		"sk_abc":      "",
		"":            "",
	}

	for token, want := range tests {
		if got := KindOf(token); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", token, got, want)
		}
	}

	if KindChecks.Other() != KindOrganisation || KindOrganisation.Other() != KindChecks {
		t.Error("Other() is not symmetric")
	}
}

func TestGrantedFallsBackToThePrimaryToken(t *testing.T) {
	collected := CollectedToken{Token: "dm_live_x", Type: KindChecks}

	if granted := collected.Granted(); len(granted) != 1 || granted[0].Token != "dm_live_x" {
		t.Errorf("Granted() = %v", granted)
	}
}
