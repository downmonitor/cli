// Package ui provides terminal styling: ANSI colors, padding that ignores
// escape codes, and a progress spinner.
//
// Functions take the color decision as an argument instead of detecting it
// themselves. It is made once per stream at start-up, which keeps rendering
// deterministic and testable.
package ui

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ANSI styles. Styles can be combined by concatenation, e.g. Bold+Red.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Grey    = "\033[90m"

	// Reversed styles, used for verdict badges.
	OnGreen  = "\033[1;30;42m"
	OnYellow = "\033[1;30;43m"
	OnRed    = "\033[1;97;41m"
	OnGrey   = "\033[1;30;47m"
)

// Paint wraps text in style when color is true, and returns it unchanged
// otherwise.
func Paint(style, text string, color bool) string {
	if !color || text == "" {
		return text
	}

	return style + text + Reset
}

// VerdictStyle returns the color used for a verdict everywhere in the output.
func VerdictStyle(verdict string) string {
	switch verdict {
	case "up":
		return Green
	case "slow":
		return Yellow
	case "down":
		return Red
	}

	return Grey
}

// Badge renders a verdict as a colored block (" UP " on green), or as the
// upper-cased word without color.
func Badge(verdict string, color bool) string {
	label := strings.ToUpper(verdict)
	if label == "" {
		label = "UNKNOWN"
	}

	if !color {
		return label
	}

	style := OnGrey

	switch verdict {
	case "up":
		style = OnGreen
	case "slow":
		style = OnYellow
	case "down":
		style = OnRed
	}

	return style + " " + label + " " + Reset
}

// Dot returns a bullet colored by verdict: ● when there is a verdict, ○ when
// there is none.
func Dot(verdict string, color bool) string {
	if verdict == "" {
		return Paint(Grey, "○", color)
	}

	return Paint(VerdictStyle(verdict), "●", color)
}

// Tick returns a green check mark.
func Tick(color bool) string { return Paint(Green, "✓", color) }

// Cross returns a red cross.
func Cross(color bool) string { return Paint(Red, "✗", color) }

// Plural formats a count with a noun, adding an "s" unless count is 1.
func Plural(count int, word string) string {
	if count == 1 {
		return "1 " + word
	}

	return strconv.Itoa(count) + " " + word + "s"
}

var escapes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// Width returns the number of columns text occupies, ignoring ANSI escape
// codes. Every rune counts as one column, which holds for the content printed
// by this CLI (codes, city names, numbers).
func Width(text string) int {
	return utf8.RuneCountInString(escapes.ReplaceAllString(text, ""))
}

// PadRight pads text with spaces on the right up to width visible columns.
// Unlike text/tabwriter, it handles colored cells correctly.
func PadRight(text string, width int) string {
	if gap := width - Width(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}

	return text
}

// PadLeft pads text with spaces on the left up to width visible columns.
func PadLeft(text string, width int) string {
	if gap := width - Width(text); gap > 0 {
		return strings.Repeat(" ", gap) + text
	}

	return text
}

// Terminal reports whether stream is an interactive terminal.
func Terminal(stream *os.File) bool {
	if stream == nil || os.Getenv("TERM") == "dumb" {
		return false
	}

	info, err := stream.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// ColorWanted reports whether stream should be colored. It honors
// --no-color, NO_COLOR (https://no-color.org) and FORCE_COLOR, and otherwise
// colors only terminals.
func ColorWanted(noColorFlag bool, stream *os.File) bool {
	if noColorFlag || os.Getenv("NO_COLOR") != "" {
		return false
	}

	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}

	return Terminal(stream)
}
