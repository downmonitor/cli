package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/exitcode"
	"github.com/downmonitor/cli/internal/render"
	"github.com/downmonitor/cli/internal/ui"
)

// minInterval is the shortest interval between two readings. Every reading is
// a billed check.
const minInterval = 5 * time.Second

// stableReadings is how many identical verdicts in a row `--until stable`
// waits for.
const stableReadings = 3

type watchFlags struct {
	check    *checkFlags
	interval time.Duration
	until    string
	timeout  time.Duration
	readings int
	onChange string
	bell     bool
}

func newWatchCommand(a *app) *cobra.Command {
	f := &watchFlags{check: &checkFlags{}}

	cmd := &cobra.Command{
		Use:   "watch <target>",
		Short: "Check a URL on a loop and log every change",
		Long: `Run the same check on a loop until interrupted, or until a condition is met.
Each reading prints one line, with a marker when a region changes state.

Every reading is billed like a check. The running total is shown while
waiting and in the final summary.

` + whereFromHelp,
		Example: `  downmonitor watch https://api.example.com/health -i 15s
  downmonitor watch https://api.example.com -r cdg,nyc,sin --bell

  # Wait for a deployment to come up, then carry on
  downmonitor watch "$DEPLOY_URL" --until up --timeout-until 5m && ./smoke-tests.sh`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.validate(); err != nil {
				return err
			}

			client, err := a.clientFor(api.KindChecks)
			if err != nil {
				return err
			}

			f.check.file = a.cfg.File

			request, err := f.check.request(args[0])
			if err != nil {
				return err
			}

			return a.watch(cmd, client, request, f)
		},
	}

	bindCheckFlags(cmd, f.check)

	flags := cmd.Flags()
	flags.DurationVarP(&f.interval, "interval", "i", 30*time.Second, "Time between readings (minimum 5s)")
	flags.StringVar(&f.until, "until", "", "Exit 0 as soon as the verdict is up, down or stable")
	flags.DurationVar(&f.timeout, "timeout-until", 0, "Give up on --until after this long, with exit code 4")
	flags.IntVar(&f.readings, "readings", 0, "Stop after this many readings")
	flags.StringVar(&f.onChange, "on-change", "", "Shell command to run whenever a region changes state")
	flags.BoolVar(&f.bell, "bell", false, "Ring the terminal bell whenever a region changes state")

	inSection(flags, sectionWatch, "interval", "until", "timeout-until", "readings", "on-change", "bell")

	return cmd
}

func (f *watchFlags) validate() error {
	if err := f.check.validate(); err != nil {
		return err
	}

	if f.check.id != "" || f.check.save != "" || f.check.baseline != "" || f.check.retries > 0 {
		return exitcode.Usagef("--id, --save, --baseline and --retries only apply to check")
	}

	if f.interval < minInterval {
		return exitcode.Usagef("--interval must be at least %s: every reading is a billed check", minInterval)
	}

	switch f.until {
	case "", "up", "down", "stable":
	default:
		return exitcode.Usagef("--until must be up, down or stable")
	}

	if f.timeout < 0 {
		return exitcode.Usagef("--timeout-until cannot be negative")
	}

	if f.timeout > 0 && f.until == "" {
		return exitcode.Usagef("--timeout-until needs --until")
	}

	if f.readings < 0 {
		return exitcode.Usagef("--readings cannot be negative")
	}

	return nil
}

// watchState is what the loop remembers between readings.
type watchState struct {
	readings    int
	billed      int
	outages     int
	downSince   time.Time
	downFor     time.Duration
	previous    map[string]string
	lastVerdict string
	streak      int
}

func (a *app) watch(cmd *cobra.Command, client *api.Client, request api.CheckRequest, f *watchFlags) error {
	state := &watchState{previous: map[string]string{}}

	if a.output == render.Table && !a.quiet {
		fmt.Fprintf(a.stdout, "%s %s %s\n",
			ui.Paint(ui.Bold, "Watching", a.color),
			ui.Paint(ui.Bold, request.Target, a.color),
			ui.Paint(ui.Grey, describeWhere(request)+", every "+f.interval.String()+" · ctrl-c to stop", a.color),
		)
	}

	var deadline time.Time
	if f.timeout > 0 {
		deadline = time.Now().Add(f.timeout)
	}

	for {
		a.spinner.Start(checkingText(request))

		check, err := client.Check(cmd.Context(), request)

		a.spinner.Stop()

		switch {
		case err == nil:
			a.logReading(check, state, f)
		case cmd.Context().Err() != nil:
			// Interrupted while a reading was in flight: stop like a Ctrl-C
			// between two readings would.
			a.watchSummary(state)

			return nil //nolint:nilerr // an interruption ends the watch normally
		default:
			// A failed reading is not a verdict: report it and keep going, the
			// next one may succeed. Usage errors are final.
			var apiErr *api.Error
			if !errors.As(err, &apiErr) || !apiErr.NothingRan() {
				return err
			}

			a.warn(time.Now().Format(time.TimeOnly) + " not measured: " + apiErr.Error())
		}

		if check != nil && f.until != "" && conditionMet(check, state, f.until) {
			a.watchSummary(state)

			return nil
		}

		if f.readings > 0 && state.readings >= f.readings {
			a.watchSummary(state)

			return nil
		}

		wait := f.interval

		if !deadline.IsZero() {
			left := time.Until(deadline)
			if left <= 0 {
				a.watchSummary(state)

				return exitcode.Timeoutf("--until %s was not met within %s", f.until, f.timeout)
			}

			wait = min(wait, left)
		}

		if !a.wait(cmd, wait, state) {
			a.watchSummary(state)

			return nil
		}
	}
}

// wait sleeps for d while showing a countdown. It returns false if the
// command was interrupted.
func (a *app) wait(cmd *cobra.Command, d time.Duration, state *watchState) bool {
	next := time.Now().Add(d)

	timer := time.NewTimer(d)
	defer timer.Stop()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	countdown := func() string {
		return ui.Paint(ui.Grey, fmt.Sprintf("next reading in %s · %s · %s billed · ctrl-c to stop",
			time.Until(next).Round(time.Second), plural(state.readings, "reading"), plural(state.billed, "check")),
			a.errColor)
	}

	a.spinner.Start(countdown())
	defer a.spinner.Stop()

	for {
		select {
		case <-cmd.Context().Done():
			return false
		case <-timer.C:
			return true
		case <-ticker.C:
			a.spinner.Update(countdown())
		}
	}
}

// logReading records a reading and prints it, along with every region that
// changed state since the previous one.
func (a *app) logReading(check *api.Check, state *watchState, f *watchFlags) {
	state.readings++
	state.billed += check.BilledChecks

	var transitions []string

	for _, region := range check.Regions {
		if !region.Measured {
			continue
		}

		verdict := deref(region.Verdict)
		if was, seen := state.previous[region.Region]; seen && was != verdict {
			transitions = append(transitions, fmt.Sprintf("%s: %s → %s", region.Region, was, verdict))
		}

		state.previous[region.Region] = verdict
	}

	sort.Strings(transitions)

	// Outages are counted on the overall verdict, not per region.
	if check.Verdict == "down" && state.lastVerdict != "down" {
		state.outages++
		state.downSince = time.Now()
	}

	if check.Verdict != "down" && state.lastVerdict == "down" && !state.downSince.IsZero() {
		recovered := time.Since(state.downSince).Round(time.Second)
		state.downFor += recovered
		state.downSince = time.Time{}
		transitions = append(transitions, "recovered after "+recovered.String())
	}

	if check.Verdict == state.lastVerdict {
		state.streak++
	} else {
		state.streak = 1
	}

	state.lastVerdict = check.Verdict

	switch {
	case a.quiet:
	case a.output.IsJSON():
		_ = json.NewEncoder(a.stdout).Encode(check)
	default:
		fmt.Fprintln(a.stdout, watchLine(check, transitions, time.Now(), a.color))
	}

	if len(transitions) > 0 {
		if f.bell {
			fmt.Fprint(a.stderr, "\a")
		}

		a.runOnChange(check, f, transitions)
	}
}

func watchLine(check *api.Check, transitions []string, now time.Time, color bool) string {
	verdict := check.Verdict
	if verdict == "down" {
		verdict = strings.ToUpper(verdict)
	}

	parts := []string{
		ui.Paint(ui.Grey, now.Format(time.TimeOnly), color),
		ui.Dot(check.Verdict, color) + " " + ui.Paint(ui.Bold+ui.VerdictStyle(check.Verdict), ui.PadRight(verdict, 4), color),
	}

	for _, region := range check.Regions {
		name := ui.Paint(ui.Grey, region.Region, color)

		switch {
		case !region.Measured:
			parts = append(parts, name+" —")
		case deref(region.Verdict) == "down" && region.StatusCode != nil && *region.StatusCode > 0:
			parts = append(parts, name+" "+ui.Paint(ui.Red, fmt.Sprint(*region.StatusCode), color))
		case region.Timings != nil && region.Timings.TotalMs != nil:
			parts = append(parts, name+" "+paintVerdict(fmt.Sprintf("%dms", *region.Timings.TotalMs), deref(region.Verdict), color))
		default:
			parts = append(parts, name+" —")
		}
	}

	if check.Summary.P95Ms != nil {
		parts = append(parts, ui.Paint(ui.Grey, fmt.Sprintf("p95 %dms", *check.Summary.P95Ms), color))
	}

	line := strings.Join(parts, "  ")

	if len(transitions) > 0 {
		line += "  " + ui.Paint(ui.Bold+ui.Yellow, "▸ "+strings.Join(transitions, " · "), color)
	}

	return line
}

// runOnChange runs the --on-change command with details of the reading in its
// environment. A failure is reported but does not stop the watch.
func (a *app) runOnChange(check *api.Check, f *watchFlags, transitions []string) {
	if f.onChange == "" {
		return
	}

	encoded, err := json.Marshal(check)
	if err != nil {
		return
	}

	var down []string

	for _, region := range check.Regions {
		if region.Measured && deref(region.Verdict) == "down" {
			down = append(down, region.Region)
		}
	}

	cmd := shellCommand(f.onChange)
	cmd.Env = append(os.Environ(),
		"DOWNMONITOR_VERDICT="+check.Verdict,
		"DOWNMONITOR_TRANSITIONS="+strings.Join(transitions, ","),
		"DOWNMONITOR_REGIONS_DOWN="+strings.Join(down, ","),
		"DOWNMONITOR_CHECK="+string(encoded),
	)
	cmd.Stdout = a.stderr
	cmd.Stderr = a.stderr

	if err := cmd.Run(); err != nil {
		a.warn("--on-change failed: " + err.Error())
	}
}

func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", command)
	}

	return exec.Command("sh", "-c", command)
}

// conditionMet evaluates --until. "stable" means the last few readings all
// had the same verdict.
func conditionMet(check *api.Check, state *watchState, until string) bool {
	switch until {
	case "up", "down":
		return check.Verdict == until
	case "stable":
		return state.streak >= stableReadings
	}

	return false
}

func (a *app) watchSummary(state *watchState) {
	if a.quiet || a.output != render.Table {
		return
	}

	parts := []string{
		plural(state.readings, "reading"),
		plural(state.billed, "check") + " billed",
	}

	if state.outages > 0 {
		parts = append(parts, plural(state.outages, "outage"))
	}

	if state.downFor > 0 {
		parts = append(parts, state.downFor.String()+" down")
	}

	fmt.Fprintln(a.stdout, ui.Paint(ui.Grey, strings.Join(parts, " · "), a.color))
}
