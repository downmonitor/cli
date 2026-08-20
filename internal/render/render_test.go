package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/downmonitor/cli/internal/api"
)

func load(t *testing.T) *api.Check {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "cli", "testdata", "check_up.json"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	var check api.Check
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatalf("the fixture is not a Check: %v", err)
	}

	return &check
}

func render(t *testing.T, format Format, check *api.Check) string {
	t.Helper()

	var out bytes.Buffer

	if err := Check(&out, format, check, Options{}); err != nil {
		t.Fatalf("rendering %s: %v", format, err)
	}

	return out.String()
}

func TestTableShowsEveryPhaseAndWhatItCost(t *testing.T) {
	out := render(t, Table, load(t))

	for _, want := range []string{
		"https://api.acme.com/v1/health",
		"GET", "200", "up",
		"region", "status", "dns", "tcp", "tls", "ttfb", "total", "verdict",
		"cdg Paris", "8ms", "11ms", "24ms", "121ms", "138ms",
		"sin Singapore", "slow",
		"p50 201ms", "p95 871ms", "0 redirects",
		"tls Let's Encrypt R11", "expires in 61 days", "TLSv1.3",
		"3 checks billed", "ck_7qk3xr2m4a1b2c", "cached until",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the table is missing %q\n%s", want, out)
		}
	}
}

func TestTableCarriesNoEscapeCodesWithoutColor(t *testing.T) {
	if strings.Contains(render(t, Table, load(t)), "\033[") {
		t.Error("color leaked into an uncolored render")
	}
}

func TestARegionThatWasNotMeasuredIsNeverShownAsDown(t *testing.T) {
	check := load(t)
	check.Regions[0].Measured = false
	check.Regions[0].Verdict = nil
	check.Regions[0].Timings = nil
	check.Regions[0].Error = &api.ReadingErr{Code: "not_measured", Message: "No reading came back from there."}
	check.Summary.Measured = 2
	check.Summary.NotMeasured = 1

	out := render(t, Table, check)

	if !strings.Contains(out, "not measured") {
		t.Errorf("the region is not marked as unmeasured:\n%s", out)
	}

	if strings.Contains(out, "cdg Paris  down") {
		t.Error("an unmeasured region was shown as down")
	}

	if !strings.Contains(out, "1 of 3 regions not measured") {
		t.Errorf("the count is missing:\n%s", out)
	}
}

func TestJSONIsTheAnswerAndNothingElse(t *testing.T) {
	out := render(t, JSON, load(t))

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v", err)
	}

	if decoded["id"] != "ck_7qk3xr2m4a1b2c" || decoded["verdict"] != "up" {
		t.Errorf("the answer was changed: %v", decoded)
	}

	if !strings.HasPrefix(out, "{") {
		t.Error("something was printed before the JSON")
	}
}

func TestNDJSONIsOneLine(t *testing.T) {
	out := render(t, NDJSON, load(t))

	if lines := strings.Count(strings.TrimSpace(out), "\n"); lines != 0 {
		t.Errorf("ndjson has %d line breaks, want one object on one line", lines+1)
	}
}

func TestQuietRendersNothingInEveryFormat(t *testing.T) {
	for _, format := range Formats {
		var out bytes.Buffer

		if err := Check(&out, format, load(t), Options{Quiet: true}); err != nil {
			t.Fatal(err)
		}

		if out.Len() != 0 {
			t.Errorf("%s printed %d bytes with -q", format, out.Len())
		}
	}
}

func TestParseFormatRefusesWhatItDoesNotKnow(t *testing.T) {
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("yaml was accepted")
	}

	for _, format := range Formats {
		if parsed, err := ParseFormat(strings.ToUpper(string(format))); err != nil || parsed != format {
			t.Errorf("%s was not accepted in upper case: %v", format, err)
		}
	}
}

func TestColoredTableKeepsItsColumnsAndDrawsTheWaterfall(t *testing.T) {
	var out bytes.Buffer

	if err := Check(&out, Table, load(t), Options{Color: true}); err != nil {
		t.Fatal(err)
	}

	// Every row must start its waterfall in the same visible column.
	starts := map[int]bool{}

	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.Contains(line, "Paris") && !strings.Contains(line, "New York") && !strings.Contains(line, "Singapore") {
			continue
		}

		plain := []rune(stripEscapes(line))
		bar := strings.IndexRune(string(plain), '━')

		if bar < 0 {
			t.Errorf("no waterfall on %q", line)

			continue
		}

		starts[len([]rune(string(plain)[:bar]))] = true
	}

	if len(starts) != 1 {
		t.Errorf("the rows do not line up:\n%s", out.String())
	}
}

func stripEscapes(text string) string {
	var out strings.Builder

	for i := 0; i < len(text); i++ {
		if text[i] == 0x1b {
			for i < len(text) && !isLetter(text[i]) {
				i++
			}

			continue
		}

		out.WriteByte(text[i])
	}

	return out.String()
}

func TestHeaderHidesDisagreeingStatusCodes(t *testing.T) {
	check := load(t)

	if got := header(check); !strings.Contains(got, "200") {
		t.Errorf("header = %q, want the common status", got)
	}

	other := 503
	check.Regions[1].StatusCode = &other

	if got := header(check); strings.Contains(got, "200") || strings.Contains(got, "503") {
		t.Errorf("header = %q, want no status when regions disagree", got)
	}
}

func isLetter(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}
