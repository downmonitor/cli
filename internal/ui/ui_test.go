package ui

import (
	"testing"
)

func TestWidthIgnoresEscapeCodes(t *testing.T) {
	if got := Width(Paint(Bold+Green, "São Paulo", true)); got != 9 {
		t.Errorf("Width = %d, want 9", got)
	}
}

func TestPaddingAlignsWhatIsSeen(t *testing.T) {
	plain := PadRight("up", 6)
	colored := PadRight(Paint(Green, "up", true), 6)

	if Width(plain) != Width(colored) {
		t.Errorf("%q and %q do not take the same room", plain, colored)
	}

	if got := PadLeft("8ms", 5); got != "  8ms" {
		t.Errorf("PadLeft = %q", got)
	}
}

func TestNoColorMeansNoEscapeCode(t *testing.T) {
	for _, text := range []string{Paint(Red, "down", false), Badge("down", false), Dot("up", false), Tick(false)} {
		if Width(text) != len([]rune(text)) {
			t.Errorf("%q carries an escape code", text)
		}
	}

	if Badge("down", false) != "DOWN" {
		t.Errorf("Badge = %q, want DOWN", Badge("down", false))
	}
}

func TestPlural(t *testing.T) {
	for count, want := range map[int]string{0: "0 checks", 1: "1 check", 3: "3 checks"} {
		if got := Plural(count, "check"); got != want {
			t.Errorf("Plural(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestColorWanted(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	if !ColorWanted(false, nil) {
		t.Error("FORCE_COLOR was ignored")
	}

	if ColorWanted(true, nil) {
		t.Error("--no-color was ignored")
	}

	t.Setenv("NO_COLOR", "1")

	if ColorWanted(false, nil) {
		t.Error("NO_COLOR was ignored")
	}
}
