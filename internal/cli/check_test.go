package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/downmonitor/cli/internal/exitcode"
)

// Commands are tested end to end through Execute against an httptest server.
// The assertions cover the public contract: exit code, output, and the request
// sent to the API.

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}

	return raw
}

// recorder is a test server that answers with a fixed body and records the
// requests it receives.
type recorder struct {
	*httptest.Server
	requests []*http.Request
	bodies   []map[string]any
}

func serve(t *testing.T, status int, body []byte) *recorder {
	t.Helper()

	rec := &recorder{}

	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded map[string]any

		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&decoded)
		}

		rec.requests = append(rec.requests, r)
		rec.bodies = append(rec.bodies, decoded)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))

	t.Cleanup(rec.Close)

	return rec
}

// run executes the CLI in isolation from the machine's own configuration: the
// keychain is stubbed and the relevant environment variables are cleared.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	previous := keychainGet
	keychainGet = func(string) (string, error) { return "", nil }
	t.Cleanup(func() { keychainGet = previous })

	t.Setenv("DOWNMONITOR_API_KEY", "")
	t.Setenv("DOWNMONITOR_TOKEN", "")
	t.Setenv("DOWNMONITOR_REGIONS", "")
	t.Setenv("DOWNMONITOR_API_URL", "")
	t.Setenv("DOWNMONITOR_PROFILE", "")
	t.Setenv("DOWNMONITOR_BEARER", "")
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "1")

	var stdout, stderr bytes.Buffer

	code := Execute(args, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func TestCheckUpExitsZeroAndPrintsEveryRegion(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, stdout, _ := run(t,
		"check", "https://api.acme.com/v1/health",
		"--api-url", server.URL, "--key", "dm_live_test",
	)

	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want %d", code, exitcode.OK)
	}

	for _, want := range []string{
		"cdg Paris", "nyc New York", "sin Singapore",
		"121ms", "871ms", "slow",
		"p50 201ms", "p95 871ms",
		"Let's Encrypt R11", "expires in 61 days",
		"3 checks billed", "ck_7qk3xr2m4a1b2c",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q\n%s", want, stdout)
		}
	}
}

func TestCheckSendsWhatTheFlagsAskedFor(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, _, stderr := run(t,
		"check", "https://api.acme.com/v1/orders",
		"-X", "post",
		"--json", `{"sku":"A-1"}`,
		"-H", "X-Api-Version: 2026-09-01",
		"--bearer", "target-token",
		"-r", "cdg,nyc",
		"--exclude", "nyc",
		"--confirm", "2",
		"--expect-status", "201",
		"--expect-body", "pending",
		"--expect-json", `$.order.state == "pending"`,
		"--max-ttfb", "400",
		"--timeout", "20s",
		"--no-follow", "--insecure", "-4",
		"--resolve", "api.acme.com:443:203.0.113.10",
		"--api-url", server.URL, "--key", "dm_live_test",
	)

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	body := server.bodies[0]

	if body["method"] != "POST" {
		t.Errorf("method = %v, want POST (upper-cased for us)", body["method"])
	}

	if body["confirm"].(float64) != 2 {
		t.Errorf("confirm = %v", body["confirm"])
	}

	if body["timeout_seconds"].(float64) != 20 {
		t.Errorf("timeout_seconds = %v", body["timeout_seconds"])
	}

	if body["follow_redirects"] != false || body["verify_tls"] != false {
		t.Errorf("--no-follow and --insecure did not travel: %v", body)
	}

	if body["ip_version"].(float64) != 4 {
		t.Errorf("ip_version = %v", body["ip_version"])
	}

	if body["resolve"] != "api.acme.com:443:203.0.113.10" {
		t.Errorf("resolve = %v", body["resolve"])
	}

	headers := body["headers"].(map[string]any)
	if headers["X-Api-Version"] != "2026-09-01" {
		t.Errorf("header did not travel: %v", headers)
	}

	// --json sets Content-Type and Accept unless they were given explicitly.
	if headers["Content-Type"] != "application/json" {
		t.Errorf("--json did not set Content-Type: %v", headers)
	}

	if auth := body["auth"].(map[string]any); auth["token"] != "target-token" || auth["type"] != "bearer" {
		t.Errorf("auth = %v", auth)
	}

	assertions := body["assertions"].(map[string]any)
	if assertions["status"] != "201" || assertions["max_ttfb_ms"].(float64) != 400 {
		t.Errorf("assertions = %v", assertions)
	}

	rule := assertions["json"].([]any)[0].(map[string]any)
	if rule["path"] != "$.order.state" || rule["op"] != "==" || rule["value"] != "pending" {
		t.Errorf("--expect-json was not parsed: %v", rule)
	}

	// A list of regions is sent as a list; a group name as a string.
	if _, isList := body["regions"].([]any); !isList {
		t.Errorf("regions = %v, want a list", body["regions"])
	}
}

func TestWithNoRegionTheAPIPicksThem(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, _, stderr := run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")
	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	// Nothing is sent about regions: the API picks among those in service.
	for _, field := range []string{"regions", "countries", "continents", "count"} {
		if _, sent := server.bodies[0][field]; sent {
			t.Errorf("%s was sent: %v", field, server.bodies[0])
		}
	}
}

func TestCountryContinentAndCountNarrowThePick(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, _, stderr := run(t, "check", "https://api.acme.com",
		"--country", "fr,de", "--continents", "north-america", "-n", "3",
		"--api-url", server.URL, "--key", "dm_live_test")
	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	body := server.bodies[0]

	if got := body["countries"].([]any); len(got) != 2 || got[0] != "FR" || got[1] != "DE" {
		t.Errorf("countries = %v, want [FR DE]", got)
	}

	if got := body["continents"].([]any); len(got) != 1 || got[0] != "NA" {
		t.Errorf("continents = %v, want [NA]", got)
	}

	if body["count"].(float64) != 3 {
		t.Errorf("count = %v", body["count"])
	}
}

func TestRegionIsASpellingOfRegionsAndTakesACity(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	run(t, "check", "https://api.acme.com", "--region", "paris", "--api-url", server.URL, "--key", "dm_live_test")

	if got := server.bodies[0]["regions"].([]any); len(got) != 1 || got[0] != "paris" {
		t.Errorf("regions = %v, want [paris]", got)
	}
}

func TestNamingRegionsAndAskingForAPickIsAUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"-r", "cdg", "--country", "FR"},
		{"-r", "cdg,nyc", "--count", "3"},
		{"--country", "France"},
		{"--continent", "atlantis"},
		{"--count", "99"},
	} {
		server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

		code, _, stderr := run(t, append([]string{"check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test"}, args...)...)

		if code != exitcode.Usage {
			t.Errorf("%v: exit code = %d, want %d\n%s", args, code, exitcode.Usage, stderr)
		}

		if len(server.bodies) != 0 {
			t.Errorf("%v: a request was sent", args)
		}
	}
}

func TestTheRepositoryFileChoosesWhereWhenNoFlagDoes(t *testing.T) {
	answer := fixture(t, "check_up.json")
	dir := t.TempDir()
	yml := "countries: [fr]\ncount: 3\nconfirm: 2\ntimeout: 12s\nheaders:\n  X-Preview: yes\n"

	if err := os.WriteFile(filepath.Join(dir, ".downmonitor.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	server := serve(t, http.StatusCreated, answer)
	run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")

	body := server.bodies[0]

	if got := body["countries"].([]any); len(got) != 1 || got[0] != "FR" {
		t.Errorf("countries = %v, want [FR]", got)
	}

	if body["count"].(float64) != 3 || body["confirm"].(float64) != 2 || body["timeout_seconds"].(float64) != 12 {
		t.Errorf("the file's defaults did not travel: %v", body)
	}

	if body["headers"].(map[string]any)["X-Preview"] != "yes" {
		t.Errorf("headers = %v", body["headers"])
	}

	// A flag replaces the file's selection entirely.
	server = serve(t, http.StatusCreated, answer)
	run(t, "check", "https://api.acme.com", "-r", "nyc", "--api-url", server.URL, "--key", "dm_live_test")

	if _, sent := server.bodies[0]["countries"]; sent {
		t.Errorf("the file's countries were sent with --regions: %v", server.bodies[0])
	}
}

func TestCheckRegionGroupTravelsAsAString(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	run(t, "check", "https://api.acme.com", "-r", "tier1", "--api-url", server.URL, "--key", "dm_live_test")

	if server.bodies[0]["regions"] != "tier1" {
		t.Errorf("regions = %v, want the string tier1", server.bodies[0]["regions"])
	}
}

func TestCheckCarriesTheContractVersionAndTheCredential(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")

	request := server.requests[0]

	if got := request.Header.Get("Authorization"); got != "Bearer dm_live_test" {
		t.Errorf("Authorization = %q", got)
	}

	if got := request.Header.Get("X-Api-Version"); got == "" {
		t.Error("no X-Api-Version was sent")
	}

	if got := request.Header.Get("User-Agent"); !strings.HasPrefix(got, "downmonitor/") {
		t.Errorf("User-Agent = %q", got)
	}
}

func TestCheckDownExitsOne(t *testing.T) {
	body := withVerdict(t, "down", false)
	server := serve(t, http.StatusCreated, body)

	code, stdout, _ := run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Verdict {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Verdict)
	}

	if !strings.Contains(stdout, "DOWN") {
		t.Errorf("the verdict is not in the output:\n%s", stdout)
	}
}

func TestSlowIsReportedNotFailed(t *testing.T) {
	server := serve(t, http.StatusCreated, withVerdict(t, "slow", true))

	code, _, _ := run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: slow is a pass unless a latency assertion says otherwise", code)
	}
}

func TestFailOnNeverKeepsTheBuildGreen(t *testing.T) {
	server := serve(t, http.StatusCreated, withVerdict(t, "down", false))

	code, _, _ := run(t, "check", "https://api.acme.com",
		"--fail-on", "never", "--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want 0 with --fail-on never", code)
	}
}

func TestNothingMeasuredIsExitThreeAndFailOpenForgivesIt(t *testing.T) {
	server := serve(t, http.StatusServiceUnavailable,
		[]byte(`{"error":"no_probe_available","message":"No region could run the check."}`))

	code, _, stderr := run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Unreachable {
		t.Fatalf("exit code = %d, want %d: nothing was checked", code, exitcode.Unreachable)
	}

	if !strings.Contains(stderr, "No region could run the check.") {
		t.Errorf("the server's own message is missing:\n%s", stderr)
	}

	code, _, stderr = run(t, "check", "https://api.acme.com",
		"--fail-open", "--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want 0 with --fail-open", code)
	}

	if !strings.Contains(stderr, "warning") {
		t.Errorf("--fail-open said nothing on stderr:\n%s", stderr)
	}
}

func TestAQuotaRefusalIsNeverAVerdict(t *testing.T) {
	server := serve(t, http.StatusPaymentRequired,
		[]byte(`{"error":"quota_exhausted","message":"This key has used its 500 checks for 2026-09.","limit":500}`))

	code, stdout, stderr := run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Unreachable {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Unreachable)
	}

	if stdout != "" {
		t.Errorf("stdout must stay empty when nothing was measured, got %q", stdout)
	}

	if !strings.Contains(stderr, "whoami") {
		t.Errorf("no hint about the quota:\n%s", stderr)
	}
}

func TestAnUnknownRegionIsAUsageError(t *testing.T) {
	server := serve(t, http.StatusUnprocessableEntity,
		[]byte(`{"error":"unknown_region","message":"No region \"atlantis\". GET /v1/regions lists them."}`))

	code, _, stderr := run(t, "check", "https://api.acme.com", "-r", "atlantis",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Usage {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Usage)
	}

	if !strings.Contains(stderr, "downmonitor regions") {
		t.Errorf("no hint pointing at `regions`:\n%s", stderr)
	}
}

func TestATargetTheAPIDoesNotServeIsRefusedBeforeAnyCall(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	for _, target := range []string{"db.acme.com:5432", "ping://203.0.113.10", "dns://acme.com", "tls://acme.com"} {
		code, _, stderr := run(t, "check", target, "--api-url", server.URL, "--key", "dm_live_test")

		if code != exitcode.Usage {
			t.Errorf("%s: exit code = %d, want %d", target, code, exitcode.Usage)
		}

		if !strings.Contains(stderr, "not supported yet") {
			t.Errorf("%s: unhelpful message %q", target, stderr)
		}
	}

	if len(server.requests) != 0 {
		t.Errorf("%d call(s) were made for a target we cannot run", len(server.requests))
	}
}

func TestNoCredentialIsExitThreeWithSomethingToDoAboutIt(t *testing.T) {
	code, _, stderr := run(t, "check", "https://api.acme.com", "--api-url", "http://127.0.0.1:1")

	if code != exitcode.Unreachable {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Unreachable)
	}

	if !strings.Contains(stderr, "downmonitor login") {
		t.Errorf("stderr does not say what to do:\n%s", stderr)
	}
}

func TestAnUnreachableAPIIsNeverAVerdict(t *testing.T) {
	code, stdout, _ := run(t, "check", "https://api.acme.com",
		"--api-url", "http://127.0.0.1:1", "--key", "dm_live_test")

	if code != exitcode.Unreachable {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Unreachable)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestQuietPrintsNothingAndStillCarriesTheVerdict(t *testing.T) {
	server := serve(t, http.StatusCreated, withVerdict(t, "down", false))

	code, stdout, _ := run(t, "check", "https://api.acme.com", "-q",
		"--api-url", server.URL, "--key", "dm_live_test")

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing with -q", stdout)
	}

	if code != exitcode.Verdict {
		t.Errorf("exit code = %d, want %d", code, exitcode.Verdict)
	}
}

func TestJSONOutputIsTheAPIBodyUnchanged(t *testing.T) {
	raw := fixture(t, "check_up.json")
	server := serve(t, http.StatusCreated, raw)

	_, stdout, _ := run(t, "check", "https://api.acme.com", "-o", "json",
		"--api-url", server.URL, "--key", "dm_live_test")

	var printed, served map[string]any

	if err := json.Unmarshal([]byte(stdout), &printed); err != nil {
		t.Fatalf("the output is not JSON: %v\n%s", err, stdout)
	}

	if err := json.Unmarshal(raw, &served); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"id", "verdict", "passed", "billed_checks", "target"} {
		if fmtAny(printed[key]) != fmtAny(served[key]) {
			t.Errorf("%s: printed %v, served %v", key, printed[key], served[key])
		}
	}

	if len(printed["regions"].([]any)) != 3 {
		t.Errorf("regions were not carried whole")
	}
}

func TestSaveWritesTheReadingAndIDFetchesItBack(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))
	path := filepath.Join(t.TempDir(), "check.json")

	run(t, "check", "https://api.acme.com", "--save", path,
		"--api-url", server.URL, "--key", "dm_live_test")

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("--save wrote nothing: %v", err)
	}

	if !strings.Contains(string(saved), "ck_7qk3xr2m4a1b2c") {
		t.Errorf("the saved reading is not the reading:\n%s", saved)
	}

	// --id fetches the check back with a GET, without a target.
	run(t, "check", "--id", "ck_7qk3xr2m4a1b2c", "--api-url", server.URL, "--key", "dm_live_test")

	last := server.requests[len(server.requests)-1]

	if last.Method != http.MethodGet || !strings.HasSuffix(last.URL.Path, "/checks/ck_7qk3xr2m4a1b2c") {
		t.Errorf("--id called %s %s", last.Method, last.URL.Path)
	}
}

func TestBaselineFailsOnTheRegressionRatherThanTheNumber(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "last-good.json")

	if err := os.WriteFile(baseline, withP95(t, 200), 0o644); err != nil {
		t.Fatal(err)
	}

	// 871ms against a 200ms baseline is more than 50% slower.
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, _, stderr := run(t, "check", "https://api.acme.com",
		"--baseline", baseline, "--max-regression", "50%",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Verdict {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Verdict)
	}

	if !strings.Contains(stderr, "p95 went from") {
		t.Errorf("the regression is not explained:\n%s", stderr)
	}

	// A larger allowance passes.
	code, _, _ = run(t, "check", "https://api.acme.com",
		"--baseline", baseline, "--max-regression", "10x",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestMaxP95FailsTheCheckWithoutTouchingTheVerdict(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, _, stderr := run(t, "check", "https://api.acme.com", "--max-p95", "800",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Verdict {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Verdict)
	}

	if !strings.Contains(stderr, "p95 871ms is above") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAnUnknownOutputFormatIsAUsageError(t *testing.T) {
	code, _, stderr := run(t, "check", "https://api.acme.com", "-o", "yaml", "--key", "dm_live_test")

	if code != exitcode.Usage {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Usage)
	}

	if !strings.Contains(stderr, "unknown output format") {
		t.Errorf("stderr = %q", stderr)
	}
}

// withVerdict rewrites the fixture's verdict, for the cases that only differ by it.
func withVerdict(t *testing.T, verdict string, passed bool) []byte {
	t.Helper()

	var check map[string]any

	if err := json.Unmarshal(fixture(t, "check_up.json"), &check); err != nil {
		t.Fatal(err)
	}

	check["verdict"] = verdict
	check["passed"] = passed

	encoded, err := json.Marshal(check)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

func withP95(t *testing.T, p95 int) []byte {
	t.Helper()

	var check map[string]any

	if err := json.Unmarshal(fixture(t, "check_up.json"), &check); err != nil {
		t.Fatal(err)
	}

	check["summary"].(map[string]any)["p95_ms"] = p95

	encoded, err := json.Marshal(check)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

func fmtAny(value any) string {
	encoded, _ := json.Marshal(value)

	return string(encoded)
}

func TestInvalidFlagsAreRejectedBeforeAnyCall(t *testing.T) {
	for _, args := range [][]string{
		{"--fail-on", "sometimes"},
		{"--fail-on", "tls-expiring"},
		{"--max-regression", "50%"},
		{"--baseline", "base.json", "--max-regression", "a lot"},
		{"--retries", "-1"},
		{"--timeout", "500ms"},
		{"-4", "-6"},
		{"-d", "a", "--json", "{}"},
		{"-u", "no-password"},
		{"-H", "not a header"},
	} {
		server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

		code, _, stderr := run(t, append([]string{"check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test"}, args...)...)

		if code != exitcode.Usage {
			t.Errorf("%v: exit code = %d, want %d\n%s", args, code, exitcode.Usage, stderr)
		}

		if len(server.requests) != 0 {
			t.Errorf("%v: a request was sent", args)
		}
	}
}

func TestFailOpenForgivesAnUnreachableAPI(t *testing.T) {
	code, stdout, stderr := run(t, "check", "https://api.acme.com", "--fail-open",
		"--api-url", "http://127.0.0.1:1", "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want 0 with --fail-open", code)
	}

	if stdout != "" || !strings.Contains(stderr, "--fail-open") {
		t.Errorf("stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestFailOpenNeverForgivesAVerdict(t *testing.T) {
	server := serve(t, http.StatusCreated, withVerdict(t, "down", false))

	code, _, _ := run(t, "check", "https://api.acme.com", "--fail-open",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Verdict {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Verdict)
	}
}

func TestFailOnAllRegionsDown(t *testing.T) {
	server := serve(t, http.StatusCreated, withVerdict(t, "down", false))

	// The fixture has no region down, so all-regions-down does not fail.
	code, _, _ := run(t, "check", "https://api.acme.com", "--fail-on", "all-regions-down",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestFailOnTLSExpiring(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	// The certificate of the fixture expires in 61 days.
	code, _, _ := run(t, "check", "https://api.acme.com", "--fail-on", "tls-expiring", "--tls-min-days", "90",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Verdict {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Verdict)
	}
}

func TestAMissingBaselineIsTheFirstRun(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))
	baseline := filepath.Join(t.TempDir(), "missing.json")

	code, _, stderr := run(t, "check", "https://api.acme.com", "--baseline", baseline, "--max-regression", "10%",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}
}

func TestAnInvalidBaselineIsAUsageError(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))
	baseline := filepath.Join(t.TempDir(), "baseline.json")

	if err := os.WriteFile(baseline, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, _ := run(t, "check", "https://api.acme.com", "--baseline", baseline, "--max-regression", "10%",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.Usage {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Usage)
	}
}

func TestFollowFalseDisablesRedirects(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	run(t, "check", "https://api.acme.com", "--follow=false", "--api-url", server.URL, "--key", "dm_live_test")

	if server.bodies[0]["follow_redirects"] != false {
		t.Errorf("follow_redirects = %v", server.bodies[0]["follow_redirects"])
	}
}

func TestJSONBodyDefaultsToPost(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	run(t, "check", "https://api.acme.com", "--json", `{"a":1}`, "-H", "content-type: text/plain",
		"--api-url", server.URL, "--key", "dm_live_test")

	body := server.bodies[0]
	headers := body["headers"].(map[string]any)

	if body["method"] != "POST" || body["body"] != `{"a":1}` {
		t.Errorf("method = %v, body = %v", body["method"], body["body"])
	}

	// A header given explicitly wins, whatever its case.
	if _, set := headers["Content-Type"]; set || headers["content-type"] != "text/plain" {
		t.Errorf("headers = %v", headers)
	}
}

func TestDataIsReadFromAFile(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))
	path := filepath.Join(t.TempDir(), "body.txt")

	if err := os.WriteFile(path, []byte("from a file"), 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, "check", "https://api.acme.com", "-d", "@"+path, "--api-url", server.URL, "--key", "dm_live_test")

	if server.bodies[0]["body"] != "from a file" {
		t.Errorf("body = %v", server.bodies[0]["body"])
	}
}

func TestTheWrongCredentialIsRefusedBeforeAnyCall(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, _, stderr := run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dmt_organisation")

	if code != exitcode.Usage {
		t.Fatalf("exit code = %d, want %d", code, exitcode.Usage)
	}

	if !strings.Contains(stderr, "Checks API key") || len(server.requests) != 0 {
		t.Errorf("stderr = %q, %d requests", stderr, len(server.requests))
	}
}

func TestNoProbeAvailableSuggestsWhatToChange(t *testing.T) {
	server := serve(t, http.StatusServiceUnavailable,
		[]byte(`{"error":"no_probe_available","message":"No probe in France right now.","available":["nyc","sin"]}`))

	_, _, stderr := run(t, "check", "https://api.acme.com", "--country", "FR",
		"--api-url", server.URL, "--key", "dm_live_test")

	for _, want := range []string{"In service now: nyc, sin.", "drop --country", "--fail-open"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr is missing %q:\n%s", want, stderr)
		}
	}
}

func TestAnInvalidRepositoryFileIsReported(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, ".downmonitor.yml"), []byte("regions: [oops\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	code, _, stderr := run(t, "check", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	if !strings.Contains(stderr, "ignoring .downmonitor.yml") {
		t.Errorf("no warning about the file:\n%s", stderr)
	}
}

func TestVerboseNeverPrintsTheKey(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	_, stdout, stderr := run(t, "check", "https://api.acme.com", "-v", "--api-url", server.URL, "--key", "dm_live_secret")

	if strings.Contains(stdout+stderr, "dm_live_secret") {
		t.Error("the key was printed")
	}

	if !strings.Contains(stderr, "the --key flag") {
		t.Errorf("verbose output does not say which credential is used:\n%s", stderr)
	}
}
