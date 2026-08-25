package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/downmonitor/cli/internal/exitcode"
)

const regionsBody = `{
	"data": [
		{"code": "cdg", "name": "Paris", "country": "FR", "country_name": "France", "continent": "EU", "continent_name": "Europe", "tier": 1, "probes": 2, "networks": ["Scaleway"], "healthy": true},
		{"code": "fra", "name": "Frankfurt", "country": "DE", "country_name": "Germany", "continent": "EU", "continent_name": "Europe", "tier": 2, "probes": 1, "networks": ["Hetzner"], "healthy": true},
		{"code": "nyc", "name": "New York", "country": "US", "country_name": "United States", "continent": "NA", "tier": 1, "probes": 3, "networks": ["Vultr"], "healthy": true}
	],
	"default": ["cdg", "nyc"],
	"default_count": 2,
	"count": 3
}`

func TestRegionsListsEveryRegionByContinent(t *testing.T) {
	server := serve(t, http.StatusOK, []byte(regionsBody))

	code, stdout, stderr := run(t, "regions", "--api-url", server.URL, "--key", "dm_live_test")
	if code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}

	for _, want := range []string{
		"Europe", "North America",
		"cdg", "Paris", "France (FR)", "free", "2 probes", "Scaleway", "auto",
		"fra", "paid", "1 probe ",
		"3 regions in service", "checks run from cdg, nyc",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}

	if strings.Index(stdout, "Europe") > strings.Index(stdout, "North America") {
		t.Error("continents are not in API order")
	}
}

func TestRegionsAcceptsAnOrganisationToken(t *testing.T) {
	server := serve(t, http.StatusOK, []byte(regionsBody))

	if code, _, stderr := run(t, "regions", "--api-url", server.URL, "--key", "dmt_token"); code != exitcode.OK {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}
}

func TestRegionsJSONIsTheAPIResponse(t *testing.T) {
	server := serve(t, http.StatusOK, []byte(regionsBody))

	_, stdout, _ := run(t, "regions", "-o", "json", "--api-url", server.URL, "--key", "dm_live_test")

	if !strings.Contains(stdout, `"code": "fra"`) || !strings.Contains(stdout, `"default_count": 2`) {
		t.Errorf("stdout = %s", stdout)
	}
}

func TestHelpGroupsFlagsInSections(t *testing.T) {
	_, stdout, _ := run(t, "check", "--help")

	for _, want := range []string{"Where to check from", "Request", "Assertions", "Verdict and CI", "Global flags"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help is missing the %q section", want)
		}
	}
}
