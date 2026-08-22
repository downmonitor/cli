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

func TestFirstLine(t *testing.T) {
	if got := firstLine([]byte("  first\nsecond\n")); got != "first" {
		t.Errorf("firstLine = %q", got)
	}

	if got := firstLine(nil); got != "no output" {
		t.Errorf("firstLine(nil) = %q", got)
	}
}
