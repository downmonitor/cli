package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/downmonitor/cli/internal/ui"
)

// The help output groups commands by topic and splits a command's flags into
// sections: `check` has around thirty flags, which are hard to scan as a
// single alphabetical list.

// flagSection is the pflag annotation holding the section of a flag.
const flagSection = "downmonitor_section"

const (
	sectionWhere      = "Where to check from"
	sectionRequest    = "Request"
	sectionAssertions = "Assertions"
	sectionVerdict    = "Verdict and CI"
	sectionWatch      = "Watching"
	sectionOther      = "Flags"
)

// sections lists the flag sections in display order. Flags without a section
// go to the last one.
var sections = []string{sectionWhere, sectionRequest, sectionAssertions, sectionVerdict, sectionWatch, sectionOther}

// inSection assigns flags to a help section.
func inSection(flags *pflag.FlagSet, section string, names ...string) {
	for _, name := range names {
		_ = flags.SetAnnotation(name, flagSection, []string{section})
	}
}

// helpColor decides whether the help is colored. It cannot rely on the app
// settings, because --help returns before PersistentPreRunE runs.
func helpColor(cmd *cobra.Command) bool {
	noColor := false

	if flag := cmd.Flags().Lookup("no-color"); flag != nil {
		noColor = flag.Value.String() == "true"
	}

	out, isFile := cmd.OutOrStdout().(*os.File)

	return isFile && ui.ColorWanted(noColor, out)
}

func installHelp(root *cobra.Command) {
	cobra.AddTemplateFunc("heading", func(cmd *cobra.Command, title string) string {
		return ui.Paint(ui.Bold+ui.Cyan, title, helpColor(cmd))
	})

	cobra.AddTemplateFunc("accent", func(cmd *cobra.Command, text string) string {
		return ui.Paint(ui.Bold, text, helpColor(cmd))
	})

	cobra.AddTemplateFunc("flagSections", flagSections)

	root.SetUsageTemplate(usageTemplate)
}

// flagSections renders the local flags of cmd grouped by section.
func flagSections(cmd *cobra.Command) string {
	grouped := map[string]*pflag.FlagSet{}

	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}

		section := sectionOther
		if values, ok := flag.Annotations[flagSection]; ok && len(values) > 0 {
			section = values[0]
		}

		if grouped[section] == nil {
			grouped[section] = pflag.NewFlagSet(section, pflag.ContinueOnError)
			grouped[section].SortFlags = false
		}

		grouped[section].AddFlag(flag)
	})

	color := helpColor(cmd)

	var out strings.Builder

	for _, section := range sections {
		set := grouped[section]
		if set == nil {
			continue
		}

		title := section
		if len(grouped) == 1 {
			title = "Flags"
		}

		out.WriteString("\n\n" + ui.Paint(ui.Bold+ui.Cyan, title, color) + "\n")
		out.WriteString(strings.TrimRight(set.FlagUsages(), " \n"))
	}

	return out.String()
}

// usageTemplate is cobra's default template with styled headings, grouped
// commands and sectioned flags.
const usageTemplate = `{{heading . "Usage"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <command> [flags]{{end}}{{if gt (len .Aliases) 0}}

{{heading . "Aliases"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading . "Examples"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{$root := .}}{{range $group := .Groups}}

{{heading $root $group.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{accent $root (rpad .Name .NamePadding) }}  {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading . "More"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{accent $root (rpad .Name .NamePadding) }}  {{.Short}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}{{flagSections .}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading . "Global flags"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Run "{{.CommandPath}} <command> --help" for more information about a command.{{end}}
Documentation: https://downmonitor.io/docs/cli
`
