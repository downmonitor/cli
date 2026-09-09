package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/exitcode"
)

func TestWatchStopsAfterTheRequestedReadings(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, stdout, stderr := run(t, "watch", "https://api.acme.com", "--readings", "1",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	for _, want := range []string{"Watching https://api.acme.com", "cdg 138ms", "p95 871ms", "1 reading · 3 checks billed"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}
}

func TestWatchUntilUpStopsOnTheFirstUp(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	code, _, stderr := run(t, "watch", "https://api.acme.com", "--until", "up", "--timeout-until", "1m",
		"--api-url", server.URL, "--key", "dm_live_test")

	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	if len(server.requests) != 1 {
		t.Errorf("%d readings, want 1", len(server.requests))
	}
}

func TestWatchNDJSONPrintsOneObjectPerReading(t *testing.T) {
	server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

	_, stdout, _ := run(t, "watch", "https://api.acme.com", "--readings", "1", "-o", "ndjson",
		"--api-url", server.URL, "--key", "dm_live_test")

	var check api.Check
	if err := json.Unmarshal([]byte(stdout), &check); err != nil || check.ID != "ck_7qk3xr2m4a1b2c" {
		t.Errorf("stdout is not one check: %v\n%s", err, stdout)
	}
}

func TestWatchRejectsInvalidFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-i", "1s"},
		{"--until", "sideways"},
		{"--timeout-until", "1m"},
		{"--readings", "-1"},
		{"--save", "out.json"},
	} {
		server := serve(t, http.StatusCreated, fixture(t, "check_up.json"))

		code, _, stderr := run(t, append([]string{"watch", "https://api.acme.com", "--api-url", server.URL, "--key", "dm_live_test"}, args...)...)

		if code != exitcode.Usage {
			t.Errorf("%v: exit code = %d, want %d\n%s", args, code, exitcode.Usage, stderr)
		}

		if len(server.requests) != 0 {
			t.Errorf("%v: a request was sent", args)
		}
	}
}

func reading(verdict string, regions map[string]string) *api.Check {
	check := &api.Check{Verdict: verdict, BilledChecks: len(regions)}

	for code, regionVerdict := range regions {
		v := regionVerdict
		check.Regions = append(check.Regions, api.RegionReading{Region: code, Measured: true, Verdict: &v})
	}

	return check
}

func TestLogReadingTracksTransitionsAndOutages(t *testing.T) {
	var stdout strings.Builder

	a := &app{stdout: &stdout, stderr: &strings.Builder{}, output: "table"}
	state := &watchState{previous: map[string]string{}}
	f := &watchFlags{}

	a.logReading(reading("up", map[string]string{"cdg": "up"}), state, f)
	a.logReading(reading("down", map[string]string{"cdg": "down"}), state, f)
	a.logReading(reading("up", map[string]string{"cdg": "up"}), state, f)

	if state.readings != 3 || state.billed != 3 || state.outages != 1 {
		t.Errorf("state = %+v", state)
	}

	for _, want := range []string{"cdg: up → down", "cdg: down → up", "recovered after"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the log is missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestConditionMet(t *testing.T) {
	up := reading("up", nil)

	if !conditionMet(up, &watchState{}, "up") || conditionMet(up, &watchState{}, "down") {
		t.Error("up/down conditions are wrong")
	}

	if conditionMet(up, &watchState{streak: stableReadings - 1}, "stable") {
		t.Error("stable was met too early")
	}

	if !conditionMet(up, &watchState{streak: stableReadings}, "stable") {
		t.Error("stable was not met")
	}
}

func TestWatchLineShowsTheStatusOfADownRegion(t *testing.T) {
	down, code := "down", 503
	check := &api.Check{
		Verdict: "down",
		Regions: []api.RegionReading{
			{Region: "cdg", Measured: true, Verdict: &down, StatusCode: &code},
			{Region: "nyc", Measured: false},
		},
	}

	line := watchLine(check, nil, time.Date(2026, 9, 1, 12, 30, 0, 0, time.UTC), false)

	for _, want := range []string{"12:30:00", "DOWN", "cdg 503", "nyc —"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q is missing %q", line, want)
		}
	}
}
