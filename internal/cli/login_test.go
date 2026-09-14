package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/downmonitor/cli/internal/exitcode"
	"github.com/downmonitor/cli/internal/keychain"
)

// The login tests run the real device flow against a test server, with an
// in-memory keychain.

type fakeStore struct {
	stored map[string]string
	fail   error
	gone   []string
}

func (s *fakeStore) install(t *testing.T) {
	t.Helper()

	if s.stored == nil {
		s.stored = map[string]string{}
	}

	previousGet, previousSet, previousDelete := keychainGet, keychainSet, keychainDelete

	keychainGet = func(profile string) (string, error) {
		if token, ok := s.stored[profile]; ok {
			return token, nil
		}

		return "", errors.New("not found")
	}

	keychainSet = func(profile, token string) error {
		if s.fail != nil {
			return s.fail
		}

		s.stored[profile] = token

		return nil
	}

	keychainDelete = func(profile string) error {
		s.gone = append(s.gone, profile)
		delete(s.stored, profile)

		return nil
	}

	t.Cleanup(func() {
		keychainGet, keychainSet, keychainDelete = previousGet, previousSet, previousDelete
	})
}

// loginServer implements the device flow, answering each poll with the next
// entry of polls: an HTTP status, an [error, message] pair or a JSON body.
func loginServer(t *testing.T, polls []any) *httptest.Server {
	t.Helper()

	poll := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/auth/device":
			var asked map[string]string
			_ = json.NewDecoder(r.Body).Decode(&asked)

			if !strings.HasPrefix(asked["client_name"], "downmonitor/") {
				t.Errorf("client_name = %q", asked["client_name"])
			}

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"device_code": "device-secret",
				"user_code": "ABCD-2345",
				"verification_uri": "https://app.downmonitor.io/cli",
				"verification_uri_complete": "https://app.downmonitor.io/cli?code=ABCD-2345",
				"expires_in": 600,
				"interval": 1
			}`))
		case "/auth/device/token":
			var asked map[string]string
			_ = json.NewDecoder(r.Body).Decode(&asked)

			if asked["device_code"] != "device-secret" {
				t.Errorf("device_code = %q", asked["device_code"])
			}

			answer := polls[min(poll, len(polls)-1)]
			poll++

			switch value := answer.(type) {
			case int:
				w.WriteHeader(value)
			case [2]string:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"` + value[0] + `","message":"` + value[1] + `"}`))
			case string:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(value))
			}
		default:
			t.Errorf("unexpected call to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	t.Cleanup(server.Close)

	return server
}

// approvedBody is the response to a poll once the login is approved.
const approvedBody = `{
	"token": "dm_live_collected-key",
	"type": "checks",
	"name": "downmonitor CLI",
	"tokens": [
		{"type": "checks", "token": "dm_live_collected-key", "abilities": ["read"], "expires_at": null},
		{"type": "organisation", "token": "dmt_collected-secret", "abilities": ["monitors:write"], "expires_at": null}
	],
	"workspace": {"name": "Acme"},
	"user": {"name": "Robin", "email": "robin@acme.com"},
	"expires_at": null
}`

func runLogin(t *testing.T, args ...string) (int, string, string, *fakeStore) {
	t.Helper()

	store := &fakeStore{}
	store.install(t)

	previousStep := slowDownStep
	slowDownStep = 10 * time.Millisecond
	t.Cleanup(func() { slowDownStep = previousStep })

	t.Setenv("DOWNMONITOR_API_KEY", "")
	t.Setenv("DOWNMONITOR_TOKEN", "")
	t.Setenv("NO_COLOR", "1")

	var stdout, stderr bytes.Buffer
	code := Execute(args, &stdout, &stderr)

	return code, stdout.String(), stderr.String(), store
}

func TestLoginShowsACodeAndStoresWhatTheBrowserApproved(t *testing.T) {
	server := loginServer(t, []any{
		[2]string{"authorization_pending", "Waiting for the browser approval."},
		approvedBody,
	})

	code, stdout, stderr, store := runLogin(t,
		"login", "--no-browser", "--api-url", server.URL)

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	// The code is a prompt, so it goes to stderr.
	if !strings.Contains(stderr, "ABCD-2345") {
		t.Errorf("the code was not shown:\n%s", stderr)
	}

	if !strings.Contains(stderr, "code=ABCD-2345") {
		t.Errorf("the address to open was not shown:\n%s", stderr)
	}

	// Both credentials are stored, one entry each.
	if store.stored["default:checks"] != "dm_live_collected-key" {
		t.Errorf("checks key stored = %q", store.stored["default:checks"])
	}

	if store.stored["default:organisation"] != "dmt_collected-secret" {
		t.Errorf("organisation token stored = %q", store.stored["default:organisation"])
	}

	if !strings.Contains(stdout, "Acme") || !strings.Contains(stdout, "robin@acme.com") {
		t.Errorf("stdout does not say who was signed in:\n%s", stdout)
	}

	for _, want := range []string{"Checks API key", "check and watch", "organisation token", "workspace"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q\n%s", want, stdout)
		}
	}

	// The tokens themselves are never printed.
	if strings.Contains(stdout+stderr, "dmt_collected-secret") || strings.Contains(stdout+stderr, "dm_live_collected-key") {
		t.Error("a token was printed")
	}
}

func TestLoginAsksForBothByDefault(t *testing.T) {
	var asked []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/auth/device" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			asked = append(asked, body["type"])

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"device_code":"d","user_code":"ABCD-2345","verification_uri":"u","verification_uri_complete":"u","expires_in":600,"interval":1,"grants":["checks","organisation"]}`))

			return
		}

		_, _ = w.Write([]byte(approvedBody))
	}))
	defer server.Close()

	store := &fakeStore{}
	store.install(t)
	t.Setenv("NO_COLOR", "1")

	var stdout, stderr bytes.Buffer
	Execute([]string{"login", "--no-browser", "--api-url", server.URL}, &stdout, &stderr)

	if len(asked) != 1 || asked[0] != "both" {
		t.Errorf("asked for %v, want both", asked)
	}
}

func TestLoginKeepsPollingThroughASlowDown(t *testing.T) {
	server := loginServer(t, []any{
		[2]string{"slow_down", "Polling faster than the interval you were given."},
		approvedBody,
	})

	code, _, stderr, store := runLogin(t, "login", "--no-browser", "--api-url", server.URL)

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	if store.stored["default:checks"] == "" {
		t.Error("nothing was stored")
	}
}

func TestLoginRefusedInTheBrowserGrantsNothing(t *testing.T) {
	server := loginServer(t, []any{
		[2]string{"access_denied", "The login request was refused in the browser."},
	})

	code, _, stderr, store := runLogin(t, "login", "--no-browser", "--api-url", server.URL)

	if code != exitcode.Unreachable {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Unreachable)
	}

	if len(store.stored) != 0 {
		t.Errorf("something was stored: %v", store.stored)
	}

	if !strings.Contains(stderr, "denied") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestLoginOnAnExpiredCodeSaysToStartAgain(t *testing.T) {
	server := loginServer(t, []any{
		[2]string{"expired_token", "This login request has expired."},
	})

	code, _, stderr, _ := runLogin(t, "login", "--no-browser", "--api-url", server.URL)

	if code != exitcode.Unreachable {
		t.Fatalf("exit code = %d", code)
	}

	if !strings.Contains(stderr, "downmonitor login") {
		t.Errorf("stderr does not say what to do:\n%s", stderr)
	}
}

func TestLoginStoresUnderTheProfileItWasGiven(t *testing.T) {
	server := loginServer(t, []any{approvedBody})

	code, _, _, store := runLogin(t, "login", "--no-browser", "--profile", "staging", "--api-url", server.URL)

	if code != exitcode.OK {
		t.Fatal("login failed")
	}

	if store.stored["staging:checks"] == "" {
		t.Errorf("stored under %v, not the staging profile", store.stored)
	}
}

func TestAKeychainThatCannotBeWrittenNeverPrintsTheToken(t *testing.T) {
	server := loginServer(t, []any{approvedBody})

	store := &fakeStore{fail: errors.New("no store here")}
	store.install(t)

	t.Setenv("NO_COLOR", "1")

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"login", "--no-browser", "--api-url", server.URL}, &stdout, &stderr)

	if code != exitcode.Unreachable {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Unreachable)
	}

	if strings.Contains(stdout.String()+stderr.String(), "dmt_collected-secret") {
		t.Error("the token was printed when the keychain refused it")
	}

	if !strings.Contains(stderr.String(), "DOWNMONITOR_TOKEN") {
		t.Errorf("no way forward was offered:\n%s", stderr.String())
	}
}

func TestTheStoredLoginIsUsedByOtherCommands(t *testing.T) {
	store := &fakeStore{stored: map[string]string{"default:organisation": "dmt_from-keychain"}}
	store.install(t)

	t.Setenv("DOWNMONITOR_API_KEY", "")
	t.Setenv("DOWNMONITOR_TOKEN", "")

	var seen string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":{"prefix":"dmt_abc","type":"organisation","abilities":["read"]},"workspace":{"name":"Acme"},"user":{"name":"Robin","email":"robin@acme.com"},"checks":null}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer

	if code := Execute([]string{"whoami", "--api-url", server.URL}, &stdout, &stderr); code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr.String())
	}

	if seen != "Bearer dmt_from-keychain" {
		t.Errorf("Authorization = %q: the stored login was not used", seen)
	}

	if !strings.Contains(stdout.String(), "organisation") {
		t.Errorf("whoami does not name the credential:\n%s", stdout.String())
	}
}

func TestLogoutForgetsTheProfile(t *testing.T) {
	store := &fakeStore{stored: map[string]string{"staging": "dmt_x"}}
	store.install(t)

	var stdout, stderr bytes.Buffer

	if code := Execute([]string{"logout", "--profile", "staging"}, &stdout, &stderr); code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr.String())
	}

	// Both kinds and the legacy entry.
	if len(store.gone) != 3 {
		t.Errorf("forgot %v, want every account of the profile", store.gone)
	}
}

func TestAnEnvironmentKeyBeatsTheStoredLogin(t *testing.T) {
	store := &fakeStore{stored: map[string]string{"default:checks": "dm_live_from-keychain"}}
	store.install(t)

	t.Setenv("DOWNMONITOR_API_KEY", "dm_live_from-environment")

	var seen string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"default":["cdg"],"count":0}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	Execute([]string{"regions", "--api-url", server.URL}, &stdout, &stderr)

	if seen != "Bearer dm_live_from-environment" {
		t.Errorf("Authorization = %q: the environment must win over the keychain", seen)
	}
}

func TestAFlagBeatsEverything(t *testing.T) {
	store := &fakeStore{stored: map[string]string{"default:checks": "dm_live_from-keychain"}}
	store.install(t)

	t.Setenv("DOWNMONITOR_API_KEY", "dm_live_from-environment")

	var seen string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"default":["cdg"],"count":0}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	Execute([]string{"regions", "--key", "dm_live_from-flag", "--api-url", server.URL}, &stdout, &stderr)

	if seen != "Bearer dm_live_from-flag" {
		t.Errorf("Authorization = %q: the flag must win", seen)
	}
}

func TestTheWrongKindOfCredentialIsRefusedBeforeAnyCall(t *testing.T) {
	// The mistake this catches: logging in, getting an organisation token, and
	// having `check` fail with a 403 from the other end three seconds later.
	store := &fakeStore{stored: map[string]string{"default:organisation": "dmt_only-this"}}
	store.install(t)

	t.Setenv("DOWNMONITOR_API_KEY", "")
	t.Setenv("DOWNMONITOR_TOKEN", "")
	t.Setenv("NO_COLOR", "1")

	called := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"check", "https://acme.com", "--api-url", server.URL}, &stdout, &stderr)

	if code != exitcode.Usage {
		t.Fatalf("exit code = %d, want %d: it is our mistake to send the wrong credential", code, exitcode.Usage)
	}

	if called {
		t.Error("a call was made with a credential the API would have refused")
	}

	for _, want := range []string{"organisation token", "Checks API key", "downmonitor login"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr is missing %q\n%s", want, stderr.String())
		}
	}
}

func TestACredentialPassedByFlagIsUsedWhateverItsKind(t *testing.T) {
	// An explicit flag is an instruction, not a guess: if it is the wrong kind the
	// API says so, which is the right place for that answer.
	store := &fakeStore{}
	store.install(t)

	t.Setenv("NO_COLOR", "1")

	var seen string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"default":["cdg"],"count":0}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	Execute([]string{"regions", "--key", "dmt_explicit", "--api-url", server.URL}, &stdout, &stderr)

	if seen != "Bearer dmt_explicit" {
		t.Errorf("Authorization = %q", seen)
	}
}

func TestLoginCopesWithABackendThatGrantsOneCredentialPerLogin(t *testing.T) {
	// A binary newer than the backend it talks to: `both` is refused, so the CLI
	// asks for the Checks API key rather than refusing to log in at all.
	var asked []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/auth/device" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			asked = append(asked, body["type"])

			if body["type"] == "both" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"validation_failed","message":"The selected type is invalid."}`))

				return
			}

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"device_code":"d","user_code":"ABCD-2345","verification_uri":"u","verification_uri_complete":"u","expires_in":600,"interval":1}`))

			return
		}

		_, _ = w.Write([]byte(`{"token":"dm_live_only-one","type":"checks","name":"CLI","workspace":{"name":"Acme"},"user":{"name":"Robin","email":"robin@acme.com"}}`))
	}))
	defer server.Close()

	store := &fakeStore{}
	store.install(t)
	t.Setenv("DOWNMONITOR_API_KEY", "")
	t.Setenv("DOWNMONITOR_TOKEN", "")
	t.Setenv("NO_COLOR", "1")

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"login", "--no-browser", "--api-url", server.URL}, &stdout, &stderr)

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr.String())
	}

	if len(asked) != 2 || asked[0] != "both" || asked[1] != "checks" {
		t.Errorf("asked for %v, want both then checks", asked)
	}

	// An answer with no `tokens` still stores the one it carried.
	if store.stored["default:checks"] != "dm_live_only-one" {
		t.Errorf("stored %v", store.stored)
	}

	if !strings.Contains(stderr.String(), "one credential per login") {
		t.Errorf("the fallback was silent:\n%s", stderr.String())
	}
}

func TestWhoamiJSON(t *testing.T) {
	store := &fakeStore{stored: map[string]string{"default:checks": "dm_live_from-keychain"}}
	store.install(t)

	t.Setenv("DOWNMONITOR_API_KEY", "")
	t.Setenv("DOWNMONITOR_TOKEN", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":{"prefix":"dm_live_abc","type":"checks"},"workspace":{"name":"Acme"},"user":{"name":"Robin","email":"robin@acme.com"},"checks":{"period":"2026-09","used":120,"limit":500,"remaining":380}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer

	if code := Execute([]string{"whoami", "-o", "json", "--api-url", server.URL}, &stdout, &stderr); code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr.String())
	}

	var answers []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &answers); err != nil || len(answers) != 1 {
		t.Fatalf("stdout is not a list of one answer: %v\n%s", err, stdout.String())
	}
}

func TestLogoutWithoutACredentialStore(t *testing.T) {
	store := &fakeStore{}
	store.install(t)

	keychainDelete = func(string) error { return keychain.ErrUnavailable }

	var stdout, stderr bytes.Buffer

	if code := Execute([]string{"logout"}, &stdout, &stderr); code != exitcode.Usage {
		t.Fatalf("exit code = %d, want %d: %s", code, exitcode.Usage, stderr.String())
	}
}
