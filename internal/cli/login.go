package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/exitcode"
	"github.com/downmonitor/cli/internal/keychain"
	"github.com/downmonitor/cli/internal/ui"
)

// newLoginCommand signs in with the OAuth 2.0 device authorization grant
// (RFC 8628) and stores the issued credentials in the OS keychain.
//
// No password or second factor goes through the terminal: the user confirms a
// short code in a browser session that is already signed in, and picks the
// workspace there.
func newLoginCommand(a *app) *cobra.Command {
	var (
		tokenType string
		noBrowser bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in from the browser and store the credentials in the OS keychain",
		Long: `Sign in from the browser and store the credentials in the OS keychain, never in
a file.

A short code is shown here and confirmed in your browser, so no password or
two-factor code is typed in the terminal.

One login grants both credentials: a Checks API key for "check" and "watch", and
an organisation token for the workspace API. Keep several logins apart with
--profile, and select one later with --profile or $DOWNMONITOR_PROFILE.`,
		Example: `  downmonitor login
  downmonitor login --type checks
  downmonitor login --profile staging`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tokenType != "both" && tokenType != "checks" && tokenType != "organisation" {
				return exitcode.Usagef("--type must be both, checks or organisation")
			}

			client := api.New(a.cfg.BaseURL, "")

			authorization, err := a.start(cmd, client, tokenType)
			if err != nil {
				return err
			}

			a.announceCode(authorization, noBrowser)

			collected, err := a.pollUntilApproved(cmd, client, authorization)
			if err != nil {
				return err
			}

			return a.storeCredentials(collected)
		},
	}

	cmd.Flags().StringVar(&tokenType, "type", "both", "Credentials to request: both, checks (dm_live_…) or organisation (dmt_…)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the URL instead of opening a browser")

	return cmd
}

// start begins the device authorization. Servers that predate `both` reject
// it as invalid; in that case the Checks API key alone is requested.
func (a *app) start(cmd *cobra.Command, client *api.Client, tokenType string) (*api.DeviceAuthorization, error) {
	authorization, err := client.StartDeviceLogin(cmd.Context(), tokenType, clientName())

	if err == nil || tokenType != "both" {
		return authorization, err
	}

	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "validation_failed" {
		return nil, err
	}

	a.warn(fmt.Sprintf("this server grants one credential per login; requesting %s", api.KindChecks.Describe()),
		fmt.Sprintf("Run `downmonitor login --type organisation` as well for %s.", api.KindOrganisation.Commands()))

	return client.StartDeviceLogin(cmd.Context(), string(api.KindChecks), clientName())
}

// announceCode prints the user code and opens the browser. The code goes to
// stderr since it is a prompt, not output.
func (a *app) announceCode(authorization *api.DeviceAuthorization, noBrowser bool) {
	color := a.errColor
	code := "  " + authorization.UserCode + "  "
	rule := strings.Repeat("─", ui.Width(code))

	fmt.Fprintf(a.stderr, "\n  %s\n", ui.Paint(ui.Grey, "Your one-time code", color))
	fmt.Fprintf(a.stderr, "  %s\n", ui.Paint(ui.Grey, "╭"+rule+"╮", color))
	fmt.Fprintf(a.stderr, "  %s%s%s\n", ui.Paint(ui.Grey, "│", color), ui.Paint(ui.Bold+ui.Green, code, color), ui.Paint(ui.Grey, "│", color))
	fmt.Fprintf(a.stderr, "  %s\n\n", ui.Paint(ui.Grey, "╰"+rule+"╯", color))

	if noBrowser {
		fmt.Fprintf(a.stderr, "  Open %s\n  and confirm that the code matches.\n\n", ui.Paint(ui.Cyan, authorization.VerificationURIComplete, color))

		return
	}

	fmt.Fprintf(a.stderr, "  Opening %s in your browser.\n", ui.Paint(ui.Cyan, authorization.VerificationURI, color))
	fmt.Fprintf(a.stderr, "  %s\n\n", ui.Paint(ui.Grey, "No browser? Open "+authorization.VerificationURIComplete, color))

	openBrowser(authorization.VerificationURIComplete)
}

// pollUntilApproved polls at the interval requested by the server until the
// login is approved, denied or expired.
func (a *app) pollUntilApproved(cmd *cobra.Command, client *api.Client, authorization *api.DeviceAuthorization) (*api.CollectedToken, error) {
	interval := time.Duration(max(authorization.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(authorization.ExpiresIn, 60)) * time.Second)

	a.spinner.Start("Waiting for you to confirm the code in the browser")
	defer a.spinner.Stop()

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-cmd.Context().Done():
			return nil, exitcode.Unreachablef("login canceled")
		case <-timer.C:
		}

		collected, err := client.PollDeviceLogin(cmd.Context(), authorization.DeviceCode)
		if err == nil {
			return collected, nil
		}

		var apiErr *api.Error
		if !errors.As(err, &apiErr) {
			return nil, err
		}

		switch apiErr.Code {
		case "authorization_pending":
		case "slow_down":
			interval += time.Second
		case "access_denied":
			return nil, exitcode.Unreachablef("the login was denied in the browser")
		case "expired_token":
			return nil, exitcode.Unreachablef("the code expired. Run `downmonitor login` again")
		default:
			return nil, err
		}

		if time.Now().After(deadline) {
			return nil, exitcode.Unreachablef("the code expired. Run `downmonitor login` again")
		}

		timer.Reset(interval)
	}
}

// storeCredentials saves each granted credential in the keychain, one entry
// per kind. If the keychain cannot be used, nothing is written anywhere else
// and the tokens are not printed either: the user is pointed at environment
// variables instead.
func (a *app) storeCredentials(collected *api.CollectedToken) error {
	granted := collected.Granted()

	for _, credential := range granted {
		if err := keychainSet(keychain.Account(a.cfg.Profile, string(kindOf(credential))), credential.Token); err != nil {
			hints := make([]string, 0, 1+len(granted))
			hints = append(hints, "Nothing was written to disk. Create a key in the dashboard and export it instead:")

			for _, other := range granted {
				hints = append(hints, "export "+kindOf(other).EnvVar()+"=…")
			}

			return &hintedError{
				err:   exitcode.Unreachablef("could not store the credentials in %s: %w", keychain.Where(), err),
				hints: hints,
			}
		}
	}

	fmt.Fprintf(a.stdout, "%s Signed in as %s (%s) · workspace %s · profile %s\n",
		ui.Tick(a.color), ui.Paint(ui.Bold, collected.User.Name, a.color), collected.User.Email,
		ui.Paint(ui.Bold, collected.Workspace.Name, a.color), a.cfg.Profile)

	for _, credential := range granted {
		fmt.Fprintf(a.stdout, "  %s %s\n", ui.Paint(ui.Grey, "•", a.color), describeGrant(credential))
	}

	fmt.Fprintf(a.stdout, "\n  %s\n", ui.Paint(ui.Grey, "Stored in "+keychain.Where()+".", a.color))
	fmt.Fprintf(a.stdout, "  Next: %s\n", ui.Paint(ui.Bold, "downmonitor check https://your-site.com", a.color))

	return nil
}

// kindOf returns the kind of a granted credential, falling back on its prefix
// when the server did not say.
func kindOf(credential api.GrantedToken) api.Kind {
	if credential.Type != "" {
		return credential.Type
	}

	return api.KindOf(credential.Token)
}

// describeGrant describes a credential and what it is used for.
func describeGrant(credential api.GrantedToken) string {
	kind := kindOf(credential)

	line := fmt.Sprintf("%s for %s", kind.Describe(), kind.Commands())

	if len(credential.Abilities) > 0 {
		line += fmt.Sprintf(" (%s)", strings.Join(credential.Abilities, ", "))
	}

	return line
}

func newLogoutCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored credentials of a profile",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Remove both kinds and the legacy single entry.
			for _, kind := range []string{string(api.KindChecks), string(api.KindOrganisation), ""} {
				err := keychainDelete(keychain.Account(a.cfg.Profile, kind))
				if errors.Is(err, keychain.ErrUnavailable) {
					return exitcode.Usagef("no supported credential store on this system, so there is nothing to remove")
				}

				if err != nil {
					return exitcode.Unreachablef("could not remove the credentials from %s: %w", keychain.Where(), err)
				}
			}

			fmt.Fprintf(a.stdout, "%s Removed the %s login from %s.\n", ui.Tick(a.color), a.cfg.Profile, keychain.Where())

			return nil
		},
	}
}

func newWhoamiCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the credentials in use, the workspace and the remaining checks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Report every credential available, since a login grants two.
			found := 0

			var answers []*api.Me

			for _, kind := range []api.Kind{api.KindChecks, api.KindOrganisation} {
				client, err := a.clientFor(kind)
				if err != nil {
					continue
				}

				found++

				a.spinner.Start("Fetching account details")

				me, err := client.Me(cmd.Context())
				if err != nil {
					a.warn(kind.Describe() + ": " + err.Error())

					continue
				}

				answers = append(answers, me)
			}

			a.spinner.Stop()

			if found == 0 {
				return exitcode.Unreachablef(
					"no credential found. Run `downmonitor login`, or set %s", api.KindChecks.EnvVar(),
				)
			}

			if len(answers) == 0 {
				return exitcode.Unreachablef("none of the credentials of this profile was accepted")
			}

			if a.quiet {
				return nil
			}

			if a.output.IsJSON() {
				return json.NewEncoder(a.stdout).Encode(answers)
			}

			color := a.color
			label := func(text string) string { return ui.Paint(ui.Grey, ui.PadRight(text, 10), color) }

			fmt.Fprintf(a.stdout, "%s (%s)\n", ui.Paint(ui.Bold, answers[0].User.Name, color), answers[0].User.Email)
			fmt.Fprintf(a.stdout, "%s%s · profile %s\n", label("workspace"), ui.Paint(ui.Bold, answers[0].Workspace.Name, color), a.cfg.Profile)

			for _, me := range answers {
				fmt.Fprintf(a.stdout, "\n%s\n", ui.Paint(ui.Bold+ui.Cyan, me.Token.Type, color))
				fmt.Fprintf(a.stdout, "  %s%s…\n", label("key"), me.Token.Prefix)

				if len(me.Token.Abilities) > 0 {
					fmt.Fprintf(a.stdout, "  %s%s\n", label("may"), strings.Join(me.Token.Abilities, ", "))
				}

				if me.Checks != nil {
					fmt.Fprintf(a.stdout, "  %s%d of %d used in %s · %s left %s\n", label("checks"),
						me.Checks.Used, me.Checks.Limit, me.Checks.Period,
						ui.Paint(ui.Bold, fmt.Sprint(me.Checks.Remaining), color),
						usageBar(me.Checks.Used, me.Checks.Limit, color))
				}
			}

			return nil
		},
	}
}

func clientName() string {
	return fmt.Sprintf("downmonitor/%s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
}

// openBrowser opens url in the default browser. Failures are ignored: the URL
// is always printed as well.
func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	_ = cmd.Start()
}

// usageBar draws check usage as a gauge: green, yellow past 75%, red when
// exhausted. It is only drawn with color, since the numbers are printed
// anyway.
func usageBar(used, limit int, color bool) string {
	if !color || limit <= 0 {
		return ""
	}

	const width = 20

	filled := min(width, used*width/limit)
	style := ui.Green

	switch {
	case used >= limit:
		style = ui.Red
	case used*4 >= limit*3:
		style = ui.Yellow
	}

	return ui.Paint(style, strings.Repeat("━", filled), true) + ui.Paint(ui.Grey, strings.Repeat("━", width-filled), true)
}
