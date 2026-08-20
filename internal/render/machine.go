package render

import (
	"encoding/json"
	"io"

	"github.com/downmonitor/cli/internal/api"
)

// writeJSON writes the API response, indented. It is deliberately not a
// CLI-specific shape, so scripts work the same against the API and the CLI.
func writeJSON(w io.Writer, check *api.Check) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	return encoder.Encode(check)
}

// writeNDJSON writes the API response on a single line.
func writeNDJSON(w io.Writer, check *api.Check) error {
	return json.NewEncoder(w).Encode(check)
}
