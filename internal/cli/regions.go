package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/ui"
)

// newRegionsCommand lists the regions that currently have a probe in service.
func newRegionsCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "regions",
		Short: "List the regions a check can run from",
		Long: `List every region with a probe in service, grouped by continent, with its
network. Regions without a probe in service are not listed.

Regions marked "auto" are the ones a check without --regions currently uses.
The selection follows probe availability, so it can change over time.`,
		Example: `  downmonitor regions
  downmonitor regions -o json | jq -r '.data[].code'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.clientForAny()
			if err != nil {
				return err
			}

			a.spinner.Start("Fetching regions")

			regions, err := client.Regions(cmd.Context())

			a.spinner.Stop()

			if err != nil {
				return err
			}

			if a.quiet {
				return nil
			}

			if a.output.IsJSON() {
				encoder := json.NewEncoder(a.stdout)
				encoder.SetIndent("", "  ")

				return encoder.Encode(regions)
			}

			a.printRegions(regions)

			return nil
		},
	}
}

var continentNames = map[string]string{
	"EU": "Europe", "NA": "North America", "SA": "South America",
	"AS": "Asia", "OC": "Oceania", "AF": "Africa",
}

func (a *app) printRegions(regions *api.Regions) {
	color := a.color
	auto := map[string]bool{}

	for _, code := range regions.Default {
		auto[code] = true
	}

	if len(regions.Data) == 0 {
		fmt.Fprintln(a.stdout, "No region has a probe in service right now.")

		return
	}

	// Size the columns over every row so all continents line up.
	codeWidth, nameWidth, countryWidth := 0, 0, 0

	for _, region := range regions.Data {
		codeWidth = max(codeWidth, ui.Width(region.Code))
		nameWidth = max(nameWidth, ui.Width(region.Name))
		countryWidth = max(countryWidth, ui.Width(countryOf(region)))
	}

	// Keep the API order; a continent is listed where its first region is.
	var order []string
	byContinent := map[string][]api.Region{}

	for _, region := range regions.Data {
		if _, seen := byContinent[region.Continent]; !seen {
			order = append(order, region.Continent)
		}

		byContinent[region.Continent] = append(byContinent[region.Continent], region)
	}

	for _, continent := range order {
		title := byContinent[continent][0].ContinentName
		if title == "" {
			title = continentNames[continent]
		}

		if title == "" {
			title = continent
		}

		fmt.Fprintf(a.stdout, "\n  %s %s\n", ui.Paint(ui.Bold+ui.Cyan, title, color), ui.Paint(ui.Grey, continent, color))

		for _, region := range byContinent[continent] {
			plan := ui.Paint(ui.Grey, "paid", color)
			if region.Tier == 1 {
				plan = ui.Paint(ui.Green, "free", color)
			}

			marker := ""
			if auto[region.Code] {
				marker = "  " + ui.Paint(ui.Bold+ui.Yellow, "auto", color)
			}

			fmt.Fprintf(a.stdout, "  %s %s  %s  %s  %s  %s  %s%s\n",
				ui.Paint(ui.Green, "●", color),
				ui.Paint(ui.Bold, ui.PadRight(region.Code, codeWidth), color),
				ui.PadRight(region.Name, nameWidth),
				ui.Paint(ui.Grey, ui.PadRight(countryOf(region), countryWidth), color),
				plan,
				ui.PadLeft(plural(region.Probes, "probe"), 8),
				ui.Paint(ui.Grey, strings.Join(region.Networks, ", "), color),
				marker,
			)
		}
	}

	summary := plural(regions.Count, "region") + " in service"

	if len(regions.Default) > 0 {
		summary += " · without --regions, checks run from " + strings.Join(regions.Default, ", ")
	}

	fmt.Fprintf(a.stdout, "\n  %s\n", ui.Paint(ui.Grey, summary, color))
	fmt.Fprintf(a.stdout, "  %s\n\n", ui.Paint(ui.Grey, "Choose with --regions cdg,nyc · --country FR · --continent EU · --count 3", color))
}

func countryOf(region api.Region) string {
	if region.CountryName != "" {
		return region.CountryName + " (" + region.Country + ")"
	}

	return region.Country
}
