package cli

import (
	"reflect"
	"testing"

	"github.com/downmonitor/cli/internal/api"
)

func TestParseJSONAssertion(t *testing.T) {
	tests := []struct {
		rule string
		want api.JSONAssertion
	}{
		{`$.status == "ok"`, api.JSONAssertion{Path: "$.status", Op: "==", Value: "ok"}},
		{`$.count >= 3`, api.JSONAssertion{Path: "$.count", Op: ">=", Value: 3.0}},
		{`$.count > 3`, api.JSONAssertion{Path: "$.count", Op: ">", Value: 3.0}},
		{`$.ready != false`, api.JSONAssertion{Path: "$.ready", Op: "!=", Value: false}},
		{`$.name contains 'a==b'`, api.JSONAssertion{Path: "$.name", Op: "contains", Value: "a==b"}},
		{`$.items exists`, api.JSONAssertion{Path: "$.items", Op: "exists"}},
		{`$.error not exists`, api.JSONAssertion{Path: "$.error", Op: "not exists"}},
		{`$.id`, api.JSONAssertion{Path: "$.id", Op: "exists"}},
		{`$.value == null`, api.JSONAssertion{Path: "$.value", Op: "==", Value: nil}},
		{`$.version == v2`, api.JSONAssertion{Path: "$.version", Op: "==", Value: "v2"}},
	}

	for _, tt := range tests {
		got, err := parseJSONAssertion(tt.rule)
		if err != nil {
			t.Errorf("%s: %v", tt.rule, err)

			continue
		}

		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %+v, want %+v", tt.rule, got, tt.want)
		}
	}
}

func TestParseJSONAssertionRejectsInvalidRules(t *testing.T) {
	for _, rule := range []string{"", "status == ok", "$.a ==", "$.a exists now", "$.a b"} {
		if got, err := parseJSONAssertion(rule); err == nil {
			t.Errorf("%q was accepted as %+v", rule, got)
		}
	}
}

func TestAllowance(t *testing.T) {
	tests := map[string]float64{
		"50%":   300,
		"1.5x":  300,
		"300ms": 500,
		"0%":    200,
	}

	for spec, want := range tests {
		got, err := allowance(200, spec)
		if err != nil || got != want {
			t.Errorf("allowance(200, %q) = %v, %v; want %v", spec, got, err, want)
		}
	}

	for _, spec := range []string{"", "fast", "50", "-10%", "x", "1.5"} {
		if _, err := allowance(200, spec); err == nil {
			t.Errorf("allowance(200, %q) was accepted", spec)
		}
	}
}

func TestParseHeaders(t *testing.T) {
	headers, err := parseHeaders([]string{"X-One: 1", "X-Two:two: parts", "Empty:"})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{"X-One": "1", "X-Two": "two: parts", "Empty": ""}
	if !reflect.DeepEqual(headers, want) {
		t.Errorf("headers = %v", headers)
	}

	for _, invalid := range []string{"no colon", ": value"} {
		if _, err := parseHeaders([]string{invalid}); err == nil {
			t.Errorf("%q was accepted", invalid)
		}
	}
}

func TestContinentCode(t *testing.T) {
	for input, want := range map[string]string{
		"EU": "EU", "europe": "EU", "North America": "NA", "south-america": "SA", "oc": "OC",
	} {
		if got, ok := continentCode(input); !ok || got != want {
			t.Errorf("continentCode(%q) = %q, %v", input, got, ok)
		}
	}

	if _, ok := continentCode("atlantis"); ok {
		t.Error("atlantis is not a continent")
	}
}

func TestDescribeWhere(t *testing.T) {
	tests := []struct {
		request api.CheckRequest
		want    string
	}{
		{api.CheckRequest{}, "from regions picked automatically"},
		{api.CheckRequest{Count: 3}, "from 3 regions picked automatically"},
		{api.CheckRequest{Regions: []string{"cdg", "nyc"}}, "from cdg, nyc"},
		{api.CheckRequest{Regions: "tier1"}, "from tier1 regions"},
		{api.CheckRequest{Countries: []string{"FR"}, Count: 1}, "from 1 region in FR"},
	}

	for _, tt := range tests {
		if got := describeWhere(tt.request); got != tt.want {
			t.Errorf("describeWhere(%+v) = %q, want %q", tt.request, got, tt.want)
		}
	}
}
