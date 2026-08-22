package cli

import "github.com/downmonitor/cli/internal/ui"

// deref returns the string value, or an em dash for nil.
func deref(value *string) string {
	if value == nil {
		return "—"
	}

	return *value
}

func plural(count int, word string) string {
	return ui.Plural(count, word)
}

// paintVerdict colors text with the color of verdict.
func paintVerdict(text, verdict string, color bool) string {
	return ui.Paint(ui.VerdictStyle(verdict), text, color)
}
