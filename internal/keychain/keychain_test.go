package keychain

import "testing"

func TestAccount(t *testing.T) {
	tests := []struct {
		profile, kind, want string
	}{
		{"default", "checks", "default:checks"},
		{"staging", "organisation", "staging:organisation"},
		{"", "checks", "default:checks"},
		{"staging", "", "staging"},
		{"", "", "default"},
	}

	for _, tt := range tests {
		if got := Account(tt.profile, tt.kind); got != tt.want {
			t.Errorf("Account(%q, %q) = %q, want %q", tt.profile, tt.kind, got, tt.want)
		}
	}
}

func TestQuote(t *testing.T) {
	tests := map[string]string{
		"dm_live_abc": `"dm_live_abc"`,
		`a"b`:         `"a\"b"`,
		`a\b`:         `"a\\b"`,
		"with space":  `"with space"`,
	}

	for in, want := range tests {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine([]byte("  first\nsecond\n")); got != "first" {
		t.Errorf("firstLine = %q", got)
	}

	if got := firstLine(nil); got != "no output" {
		t.Errorf("firstLine(nil) = %q", got)
	}
}
