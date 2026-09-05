package render

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

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

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// writeJUnit writes one test case per region and per failed assertion, so CI
// systems show a failing region like a failing test.
//
// A region that was not measured is reported as skipped rather than failed:
// the check did not run there, which says nothing about the target.
func writeJUnit(w io.Writer, check *api.Check) error {
	suite := junitSuite{Name: "downmonitor " + check.Target}

	for _, region := range check.Regions {
		testCase := junitCase{Name: place(region), ClassName: "region", Time: seconds(region)}

		switch {
		case !region.Measured:
			testCase.Skipped = &junitSkipped{Message: reason(region)}
			suite.Skipped++
		case deref(region.Verdict) == "down":
			testCase.Failure = &junitFailure{Message: reason(region), Type: "down"}
			suite.Failures++
		}

		suite.Cases = append(suite.Cases, testCase)
		suite.Tests++
	}

	for _, assertion := range check.FailedAssertions {
		name := "assertion " + assertion.Type
		if assertion.Region != "" {
			name += " (" + assertion.Region + ")"
		}

		suite.Cases = append(suite.Cases, junitCase{
			Name:      name,
			ClassName: "assertion",
			Failure:   &junitFailure{Message: describeAssertion(assertion), Type: assertion.Type},
		})
		suite.Tests++
		suite.Failures++
	}

	document := junitSuites{
		Tests:    suite.Tests,
		Failures: suite.Failures,
		Skipped:  suite.Skipped,
		Suites:   []junitSuite{suite},
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}

	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")

	if err := encoder.Encode(document); err != nil {
		return err
	}

	_, err := io.WriteString(w, "\n")

	return err
}

// writeGitHub writes the Markdown table to the job log, one workflow command
// annotation per failure, and the table again to the job summary when running
// on GitHub Actions.
func writeGitHub(w io.Writer, check *api.Check) error {
	if err := writeMarkdown(w, check); err != nil {
		return err
	}

	for _, region := range check.Regions {
		if region.Measured && deref(region.Verdict) == "down" {
			annotate(w, "error", region.Region+" is down", reason(region))
		}
	}

	for _, assertion := range check.FailedAssertions {
		annotate(w, "error", "Assertion failed", describeAssertion(assertion))
	}

	if check.Summary.NotMeasured > 0 {
		annotate(w, "warning", "Not measured", fmt.Sprintf("%d of %d regions produced no reading",
			check.Summary.NotMeasured, check.Summary.Regions))
	}

	// Writing the summary is best effort: failing to do so must not fail the
	// step.
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		if file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_ = writeMarkdown(file, check)
			_ = file.Close()
		}
	}

	return nil
}

// annotate writes a GitHub Actions workflow command such as
// "::error title=...::message", escaping values as the runner expects.
func annotate(w io.Writer, level, title, message string) {
	fmt.Fprintf(w, "::%s title=%s::%s\n", level, escapeProperty(title), escapeData(message))
}

var (
	dataEscaper     = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	propertyEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

func escapeData(value string) string     { return dataEscaper.Replace(value) }
func escapeProperty(value string) string { return propertyEscaper.Replace(value) }

// writeMarkdown writes the result as a Markdown table, for pull request
// comments and job summaries.
func writeMarkdown(w io.Writer, check *api.Check) error {
	var out strings.Builder

	fmt.Fprintf(&out, "**%s** — %s\n\n", cell(check.Target), check.Verdict)
	out.WriteString("| region | status | ttfb | total | verdict |\n| --- | --- | --- | --- | --- |\n")

	for _, region := range check.Regions {
		if !region.Measured {
			fmt.Fprintf(&out, "| %s | — | — | — | not measured |\n", cell(place(region)))

			continue
		}

		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n",
			cell(place(region)),
			cell(status(region)),
			ms(region.Timings.TTFBMs),
			ms(region.Timings.TotalMs),
			cell(deref(region.Verdict)),
		)
	}

	if line := percentiles(check); line != "" {
		fmt.Fprintf(&out, "\n%s\n", line)
	}

	if len(check.FailedAssertions) > 0 {
		out.WriteString("\n")

		for _, assertion := range check.FailedAssertions {
			fmt.Fprintf(&out, "- ✗ %s\n", describeAssertion(assertion))
		}
	}

	fmt.Fprintf(&out, "\n<sub>%s</sub>\n", cost(check))

	_, err := io.WriteString(w, out.String())

	return err
}

// cell escapes a value for use inside a Markdown table.
func cell(value string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(value)
}

func seconds(region api.RegionReading) float64 {
	if region.Timings == nil || region.Timings.TotalMs == nil {
		return 0
	}

	return float64(*region.Timings.TotalMs) / 1000
}
