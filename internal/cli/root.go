// Package cli implements the downmonitor commands.
//
// Commands parse flags, build a request, send it with internal/api and print
// the response with internal/render. Every command can be tested end to end
// through Execute against an httptest server.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/config"
	"github.com/downmonitor/cli/internal/exitcode"
	"github.com/downmonitor/cli/internal/keychain"
	"github.com/downmonitor/cli/internal/render"
	"github.com/downmonitor/cli/internal/ui"
)

// Version is the CLI version, set by main at start-up.
var Version = "dev"

// keychainGet reads the credential store. Tests replace it.
var keychainGet = keychain.Get

// app holds the state shared by every command, resolved once in
// PersistentPreRunE.
type app struct {
	cfg config.Config

	// stdout only ever receives data, so `-o json | jq` is never broken by an
	// informational message. Progress, warnings and errors go to stderr.
	stdout io.Writer
	stderr io.Writer

	output  render.Format
	quiet   bool
	verbose bool

	// Color is decided per stream: stdout may be piped while stderr is a
	// terminal, as in `downmonitor check … -o json | jq`.
	color    bool
	errColor bool

	spinner *ui.Spinner
}

// clientFor returns a client authenticated with the given kind of credential.
// A missing or mismatched credential is reported here, with a clearer message
// than the API's 401 or 403 would give.
func (a *app) clientFor(kind api.Kind) (*api.Client, error) {
	credential, ok := a.cfg.For(kind)
	if !ok {
		if other, held := a.cfg.For(kind.Other()); held {
			return nil, exitcode.Usagef(
				"this command needs %s, but %s holds %s.\n  Run `downmonitor login` again: it grants both",
				kind.Describe(), other.Source, kind.Other().Describe(),
			)
		}

		return nil, exitcode.Unreachablef(
			"no %s found. Run `downmonitor login`, or set %s", kind.Describe(), kind.EnvVar(),
		)
	}

	if got := api.KindOf(credential.Token); got != "" && got != kind {
		return nil, exitcode.Usagef(
			"%s holds %s, but this command needs %s.\n  Run `downmonitor login` (it grants both), or pass --key",
			credential.Source, got.Describe(), kind.Describe(),
		)
	}

	a.debugf("using %s from %s", kind.Describe(), credential.Source)

	return api.New(a.cfg.BaseURL, credential.Token), nil
}

func (a *app) renderOptions() render.Options {
	return render.Options{Color: a.color, Quiet: a.quiet}
}

// Execute runs the CLI with args and returns the process exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	a := &app{stdout: stdout, stderr: stderr}

	root := newRootCommand(a)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	// Cancel the request in flight on Ctrl-C or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitcode.OK
	}

	a.report(err)

	return exitcode.Of(err)
}

// report prints an error to stderr, with the API's field errors and any hint
// on how to fix it.
func (a *app) report(err error) {
	a.spinner.Stop()

	var hints []string

	var hinted *hintedError
	if errors.As(err, &hinted) {
		hints = hinted.hints
	}

	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		a.problem(err.Error(), hints...)

		return
	}

	a.problem(apiErr.Error(), append(apiErr.Hints(), hints...)...)

	for field, messages := range apiErr.Fields {
		for _, message := range messages {
			fmt.Fprintf(a.stderr, "  %s %s\n", ui.Paint(ui.Bold, field+":", a.errColor), message)
		}
	}
}

// hintedError attaches command-specific hints to an error.
type hintedError struct {
	err   error
	hints []string
}

func (h *hintedError) Error() string { return h.err.Error() }
func (h *hintedError) Unwrap() error { return h.err }

// problem prints an error message followed by hints. Without color, the
// message is prefixed with "downmonitor:" so it is easy to find in CI logs.
func (a *app) problem(message string, hints ...string) {
	lines := strings.Split(message, "\n")

	if a.errColor {
		fmt.Fprintf(a.stderr, "%s %s\n", ui.Cross(true), ui.Paint(ui.Bold, lines[0], true))
	} else {
		fmt.Fprintf(a.stderr, "downmonitor: %s\n", lines[0])
	}

	for _, line := range lines[1:] {
		fmt.Fprintf(a.stderr, "  %s\n", strings.TrimSpace(line))
	}

	a.hints(hints)
}

// warn prints a warning to stderr.
func (a *app) warn(message string, hints ...string) {
	a.spinner.Stop()

	fmt.Fprintf(a.stderr, "%s %s\n", ui.Paint(ui.Yellow, "warning:", a.errColor), message)
	a.hints(hints)
}

func (a *app) hints(hints []string) {
	for _, hint := range hints {
		fmt.Fprintf(a.stderr, "  %s %s\n", ui.Paint(ui.Grey, "→", a.errColor), hint)
	}
}

// debugf prints a line to stderr with --verbose. Credentials must never be
// passed to it.
func (a *app) debugf(format string, args ...any) {
	if a.verbose {
		fmt.Fprintln(a.stderr, ui.Paint(ui.Grey, fmt.Sprintf(format, args...), a.errColor))
	}
}

func newRootCommand(a *app) *cobra.Command {
	var (
		keyFlag     string
		profileFlag string
		apiURLFlag  string
		outputFlag  string
		noColor     bool
	)

	root := &cobra.Command{
		Use:   "downmonitor",
		Short: "Check your endpoints from Downmonitor's probe network",
		Long: `Check a URL from Downmonitor's probe network, once, on a loop, or as a step in
a CI pipeline, with an exit code scripts can rely on.

Without --regions, a check runs from two regions picked among those with a
probe in service, on two continents when possible. Name regions with
--regions cdg,nyc (or a city: --regions paris), or narrow the pick with
--country FR or --continent EU.

Exit codes
  0  up, and every assertion passed
  1  down, or an assertion failed
  2  usage error: unknown flag or region, unreadable file
  3  nothing was checked: API unreachable, credential rejected, no probe available`,
		Example: `  downmonitor check https://example.com
  downmonitor check https://example.com --country FR --count 3`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			format, err := render.ParseFormat(outputFlag)
			if err != nil {
				return exitcode.Usagef("%w", err)
			}

			a.output = format
			a.color = streamColor(noColor, a.stdout)
			a.errColor = streamColor(noColor, a.stderr)
			a.spinner = ui.NewSpinner(a.stderr, a.interactive(), a.errColor)
			a.cfg = config.Load(keyFlag, profileFlag, keychainGet)

			if apiURLFlag != "" {
				a.cfg.BaseURL = apiURLFlag
			}

			a.debugf("api %s · profile %s", a.cfg.BaseURL, a.cfg.Profile)

			return nil
		},
	}

	flags := root.PersistentFlags()
	flags.StringVar(&keyFlag, "key", "", "API key or token to use instead of the environment or the stored login")
	flags.StringVar(&keyFlag, "token", "", "Alias of --key")
	flags.StringVar(&profileFlag, "profile", "", "Stored login to use (default $DOWNMONITOR_PROFILE or \"default\")")
	flags.StringVar(&apiURLFlag, "api-url", "", "API base URL (default $DOWNMONITOR_API_URL)")
	flags.StringVarP(&outputFlag, "output", "o", string(render.Table), "Output format: "+render.JoinFormats())
	flags.BoolVarP(&a.quiet, "quiet", "q", false, "Print nothing; rely on the exit code")
	flags.BoolVarP(&a.verbose, "verbose", "v", false, "Print debugging information to stderr (never credentials)")
	flags.BoolVar(&noColor, "no-color", false, "Disable colors (also $NO_COLOR)")

	_ = flags.MarkHidden("token")

	// Commands are listed in the order they are added, not alphabetically.
	cobra.EnableCommandSorting = false

	root.AddGroup(
		&cobra.Group{ID: "checks", Title: "Checks"},
	)

	for _, command := range []*cobra.Command{newCheckCommand(a)} {
		command.GroupID = "checks"
		root.AddCommand(command)
	}

	// Accept the spellings people commonly try first.
	root.SetGlobalNormalizationFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		switch name {
		case "region":
			name = "regions"
		case "countries":
			name = "country"
		case "continents":
			name = "continent"
		}

		return pflag.NormalizedName(name)
	})

	installHelp(root)

	return root
}

// streamColor reports whether w should be colored. Only an *os.File can be
// a terminal; any other writer (a test buffer, for instance) is never
// colored.
func streamColor(noColor bool, w io.Writer) bool {
	file, ok := w.(*os.File)

	return ok && ui.ColorWanted(noColor, file)
}

// interactive reports whether a person is likely watching stderr, which is
// when the spinner is shown: not with -q, not in CI, not when redirected.
func (a *app) interactive() bool {
	file, ok := a.stderr.(*os.File)

	return ok && !a.quiet && os.Getenv("CI") == "" && ui.Terminal(file)
}

// forgive implements --fail-open: when the API reports that nothing was
// checked, the error becomes a warning. Verdicts and usage errors are never
// forgiven.
func (a *app) forgive(err error, failOpen bool) error {
	if err == nil || !failOpen {
		return err
	}

	var apiErr *api.Error
	if !errors.As(err, &apiErr) || !apiErr.NothingRan() {
		return err
	}

	a.warn(apiErr.Error(), "--fail-open: nothing was checked, so this does not fail the command.")

	return nil
}
