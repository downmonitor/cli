// Package render formats a check result for output.
//
// Renderers are pure functions of the API response: they never compute a
// verdict, a percentile or an assertion outcome, so the CLI can never disagree
// with the API.
package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/ui"
)

// Format is an output format selected with --output.
type Format string

// Supported output formats.
const (
	Table    Format = "table"
	JSON     Format = "json"
	NDJSON   Format = "ndjson"
	JUnit    Format = "junit"
	GitHub   Format = "github"
	Markdown Format = "markdown"
)

// Formats lists every supported format, in the order they are documented.
var Formats = []Format{Table, JSON, NDJSON, JUnit, GitHub, Markdown}

// ParseFormat parses a --output value, case-insensitively.
func ParseFormat(value string) (Format, error) {
	wanted := Format(strings.ToLower(strings.TrimSpace(value)))

	for _, format := range Formats {
		if wanted == format {
			return format, nil
		}
	}

	return "", fmt.Errorf("unknown output format %q: use one of %s", value, JoinFormats())
}

// JoinFormats returns the supported formats as a comma-separated list.
func JoinFormats() string {
	names := make([]string, len(Formats))
	for i, format := range Formats {
		names[i] = string(format)
	}

	return strings.Join(names, ", ")
}

// IsJSON reports whether the format is one of the JSON formats.
func (f Format) IsJSON() bool {
	return f == JSON || f == NDJSON
}

// Options are display settings decided once at start-up.
type Options struct {
	Color bool
	Quiet bool
}

// Check writes a check result in the given format.
func Check(w io.Writer, format Format, check *api.Check, options Options) error {
	if options.Quiet {
		return nil
	}

	switch format {
	case JSON:
		return writeJSON(w, check)
	case NDJSON:
		return writeNDJSON(w, check)
	case JUnit:
		return writeJUnit(w, check)
	case GitHub:
		return writeGitHub(w, check)
	case Markdown:
		return writeMarkdown(w, check)
	default:
		return writeTable(w, check, options)
	}
}

func paint(style, text string, color bool) string {
	return ui.Paint(style, text, color)
}

func deref(value *string) string {
	if value == nil {
		return "—"
	}

	return *value
}

func ms(value *int) string {
	if value == nil {
		return "—"
	}

	return strconv.Itoa(*value) + "ms"
}

func place(region api.RegionReading) string {
	return region.Region + " " + region.Name
}

func status(region api.RegionReading) string {
	if region.StatusCode != nil && *region.StatusCode > 0 {
		return strconv.Itoa(*region.StatusCode)
	}

	if region.Error != nil && region.Error.Code != "" {
		return region.Error.Code
	}

	return "—"
}

func reason(region api.RegionReading) string {
	if region.Error != nil && region.Error.Message != "" {
		return region.Error.Message
	}

	if !region.Measured {
		return "no reading came back"
	}

	return deref(region.Verdict)
}

func percentiles(check *api.Check) string {
	var parts []string

	if check.Summary.P50Ms != nil {
		parts = append(parts, "p50 "+ms(check.Summary.P50Ms))
	}

	if check.Summary.P95Ms != nil {
		parts = append(parts, "p95 "+ms(check.Summary.P95Ms))
	}

	if check.Summary.Redirects != nil {
		parts = append(parts, ui.Plural(*check.Summary.Redirects, "redirect"))
	}

	if check.Summary.NotMeasured > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d regions not measured",
			check.Summary.NotMeasured, check.Summary.Regions))
	}

	return strings.Join(parts, " · ")
}

func describeAssertion(assertion api.Assertion) string {
	text := assertion.Type

	if assertion.Region != "" {
		text = assertion.Region + ": " + text
	}

	if assertion.Expected != "" {
		text += " expected " + assertion.Expected
	}

	if assertion.Actual != "" {
		text += ", got " + assertion.Actual
	}

	if assertion.Message != "" {
		text += " — " + assertion.Message
	}

	return text
}
