package target

import "testing"

func TestOf(t *testing.T) {
	cases := map[string]Kind{
		"https://api.acme.com/health": HTTP,
		"http://api.acme.com":         HTTP,
		"HTTPS://API.ACME.COM":        HTTP,
		"db.acme.com:5432":            Port,
		"ping://203.0.113.10":         Ping,
		"dns://acme.com":              DNS,
		"tls://acme.com":              TLS,

		// A bare host is ambiguous and must not be guessed.
		"acme.com":       Unknown,
		"":               Unknown,
		"ftp://acme.com": Unknown,
		"db.acme.com:":   Unknown,
	}

	for raw, want := range cases {
		if got := Of(raw); got != want {
			t.Errorf("Of(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestOnlyHTTPIsServedForNow(t *testing.T) {
	if !HTTP.Served() {
		t.Error("HTTP must be served")
	}

	for _, kind := range []Kind{Port, Ping, DNS, TLS, Unknown} {
		if kind.Served() {
			t.Errorf("%q is not served by the API yet", kind)
		}

		if kind.Describe() == "" {
			t.Errorf("%q has no description for a refusal", kind)
		}
	}
}

func TestNormalizeTrimsWithoutGuessing(t *testing.T) {
	value, kind := Normalize("  https://acme.com/health  ")

	if value != "https://acme.com/health" || kind != HTTP {
		t.Errorf("Normalize = %q, %q", value, kind)
	}
}
