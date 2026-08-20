package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/ui"
)

// writeTable renders the human-readable output: the verdict, one row per
// region with the request phases, then percentiles, certificate, failed
// assertions and cost.
//
// With color, each row also gets a waterfall of the request phases drawn on a
// common scale, which makes the slow region and the slow phase stand out.
func writeTable(w io.Writer, check *api.Check, options Options) error {
	color := options.Color
	badge := ui.Badge(check.Verdict, color)

	fmt.Fprintf(w, "\n %s  %s\n", badge, paint(ui.Bold, check.Target, color))

	if line := header(check); line != "" {
		fmt.Fprintf(w, " %s  %s\n", strings.Repeat(" ", ui.Width(badge)), paint(ui.Grey, line, color))
	}

	fmt.Fprintln(w)

	rows := [][]string{{"", "region", "status", "dns", "tcp", "tls", "ttfb", "total", "verdict"}}
	longest := longestTotal(check)

	for _, region := range check.Regions {
		if !region.Measured {
			rows = append(rows, []string{
				ui.Dot("", color),
				placeWithCountry(region, color),
				paint(ui.Grey, "not measured — "+reason(region), color),
			})

			continue
		}

		verdict := deref(region.Verdict)
		timings := region.Timings

		row := []string{
			ui.Dot(verdict, color),
			placeWithCountry(region, color),
			statusCell(region, color),
			ms(timings.DNSMs),
			ms(timings.TCPMs),
			ms(timings.TLSMs),
			ms(timings.TTFBMs),
			paint(ui.Bold, ms(timings.TotalMs), color),
			paint(ui.VerdictStyle(verdict), verdict, color),
		}

		if color {
			row = append(row, waterfall(timings, longest))
		}

		rows = append(rows, row)
	}

	writeRows(w, rows, color)
	fmt.Fprintln(w)

	if line := percentiles(check); line != "" {
		fmt.Fprintln(w, "  "+line)
	}

	if line := certificate(check.TLS); line != "" {
		mark := ui.Tick(color)
		if check.TLS.DaysLeft != nil && *check.TLS.DaysLeft < 14 {
			mark = paint(ui.Yellow, "!", color)
		}

		fmt.Fprintln(w, "  "+mark+" "+line)
	}

	for _, assertion := range check.FailedAssertions {
		fmt.Fprintln(w, "  "+ui.Cross(color)+" "+paint(ui.Red, describeAssertion(assertion), color))
	}

	if color {
		fmt.Fprintln(w, "  "+legend())
	}

	fmt.Fprintln(w, "  "+paint(ui.Grey, cost(check), color))
	_, err := fmt.Fprintln(w)

	return err
}

// writeRows aligns cells by visible width: status and timings to the right,
// everything else to the left. Cells beyond the header (the waterfall) and the
// last cell of a short row (an unmeasured region) are not aligned and do not
// widen any column.
func writeRows(w io.Writer, rows [][]string, color bool) {
	columns := len(rows[0])
	spans := func(row []string, i int) bool {
		return i >= columns || (len(row) < columns && i == len(row)-1)
	}

	widths := make([]int, columns)

	for _, row := range rows {
		for i, cell := range row {
			if !spans(row, i) {
				widths[i] = max(widths[i], ui.Width(cell))
			}
		}
	}

	for index, row := range rows {
		cells := make([]string, len(row))

		for i, cell := range row {
			switch {
			case spans(row, i):
				cells[i] = cell
			case i >= 2 && i <= 7:
				cells[i] = ui.PadLeft(cell, widths[i])
			default:
				cells[i] = ui.PadRight(cell, widths[i])
			}
		}

		line := strings.TrimRight(strings.Join(cells, "  "), " ")
		if index == 0 {
			line = paint(ui.Grey, line, color)
		}

		fmt.Fprintln(w, "  "+line)
	}
}

func placeWithCountry(region api.RegionReading, color bool) string {
	text := paint(ui.Bold, region.Region, color) + " " + region.Name

	if region.Country != "" {
		text += paint(ui.Grey, ", "+region.Country, color)
	}

	return text
}

func statusCell(region api.RegionReading, color bool) string {
	text := status(region)

	if region.StatusCode == nil || *region.StatusCode == 0 {
		return paint(ui.Red, text, color)
	}

	switch code := *region.StatusCode; {
	case code < 300:
		return paint(ui.Green, text, color)
	case code < 400:
		return paint(ui.Cyan, text, color)
	default:
		return paint(ui.Red, text, color)
	}
}

// barWidth is the width of the waterfall of the slowest region.
const barWidth = 24

var phases = []struct {
	name  string
	style string
}{
	{"dns", ui.Cyan},
	{"connect", ui.Blue},
	{"tls", ui.Magenta},
	{"wait", ui.Yellow},
	{"transfer", ui.Green},
}

func longestTotal(check *api.Check) int {
	longest := 0

	for _, region := range check.Regions {
		if region.Measured && region.Timings != nil && region.Timings.TotalMs != nil {
			longest = max(longest, *region.Timings.TotalMs)
		}
	}

	return longest
}

// waterfall draws the request phases on a scale where the slowest region
// spans barWidth columns. TTFB is measured from the start of the request, so
// the wait phase is what remains once DNS, connect and TLS are subtracted.
func waterfall(timings *api.Timings, longest int) string {
	if timings == nil || timings.TotalMs == nil || longest <= 0 {
		return ""
	}

	value := func(p *int) int {
		if p == nil || *p < 0 {
			return 0
		}

		return *p
	}

	dns, tcp, tls := value(timings.DNSMs), value(timings.TCPMs), value(timings.TLSMs)
	ttfb, total := value(timings.TTFBMs), value(timings.TotalMs)
	wait := max(0, ttfb-dns-tcp-tls)
	transfer := max(0, total-max(ttfb, dns+tcp+tls))

	var bar strings.Builder

	drawn, elapsed := 0, 0

	for i, duration := range []int{dns, tcp, tls, wait, transfer} {
		elapsed += duration
		upTo := elapsed * barWidth / longest

		// Any phase that took time gets at least one column.
		if duration > 0 && upTo == drawn {
			upTo = drawn + 1
		}

		upTo = min(upTo, barWidth)

		if upTo > drawn {
			bar.WriteString(phases[i].style + strings.Repeat("━", upTo-drawn) + ui.Reset)
			drawn = upTo
		}
	}

	return bar.String()
}

func legend() string {
	parts := make([]string, len(phases))

	for i, phase := range phases {
		parts[i] = phase.style + "━" + ui.Reset + ui.Grey + " " + phase.name + ui.Reset
	}

	return strings.Join(parts, "  ")
}

// header is the line under the target: the method, the status code if every
// region agrees on it, and the number of regions.
func header(check *api.Check) string {
	var parts []string

	if method, ok := check.Request["method"].(string); ok && method != "" {
		parts = append(parts, method)
	}

	if code := commonStatus(check); code != "" {
		parts = append(parts, code)
	}

	if count := len(check.Regions); count > 0 {
		parts = append(parts, ui.Plural(count, "region"))
	}

	return strings.Join(parts, " · ")
}

// commonStatus returns the status code shared by every measured region, or ""
// when they differ, so a single number never hides a disagreement.
func commonStatus(check *api.Check) string {
	common := -1

	for _, region := range check.Regions {
		if !region.Measured || region.StatusCode == nil {
			continue
		}

		switch {
		case common == -1:
			common = *region.StatusCode
		case common != *region.StatusCode:
			return ""
		}
	}

	if common == -1 {
		return ""
	}

	return strconv.Itoa(common)
}

func certificate(tls *api.TLS) string {
	if tls == nil {
		return ""
	}

	var parts []string

	if tls.Issuer != "" {
		parts = append(parts, tls.Issuer)
	}

	if tls.DaysLeft != nil {
		parts = append(parts, "expires in "+ui.Plural(*tls.DaysLeft, "day"))
	} else if tls.NotAfter != "" {
		parts = append(parts, "expires "+tls.NotAfter)
	}

	if tls.Version != "" {
		parts = append(parts, tls.Version)
	}

	return "tls " + strings.Join(parts, " · ")
}

// cost is always the last line: what the check was billed, and the id that
// fetches it again with `check --id`.
func cost(check *api.Check) string {
	parts := []string{ui.Plural(check.BilledChecks, "check") + " billed", check.ID}

	if until := clock(check.CachedUntil); until != "" {
		parts = append(parts, "cached until "+until)
	}

	return strings.Join(parts, " · ")
}

func clock(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ""
	}

	return parsed.UTC().Format("15:04") + " UTC"
}
