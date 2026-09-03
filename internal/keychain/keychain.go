// Package keychain stores credentials in the operating system's credential
// store: the Keychain on macOS and the Secret Service (through secret-tool) on
// Linux.
//
// It shells out to the tools the system ships rather than linking against
// native libraries, which keeps the binary static and free of cgo. Tokens are
// always passed on stdin, never as command-line arguments, so they do not show
// up in the process list.
//
// When no store is available, nothing is written to disk as a fallback:
// callers are expected to point the user at environment variables instead.
package keychain

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// service is the name entries are stored under. The account holds the profile
// and the credential kind, so several logins can coexist.
const service = "downmonitor-cli"

// macOSNotFound is the exit status of `security` when no item matches.
const macOSNotFound = 44

var (
	// ErrUnavailable means no supported credential store could be used.
	ErrUnavailable = errors.New("no OS credential store available")

	// ErrNotFound means the store is available but holds no entry for the
	// account.
	ErrNotFound = errors.New("no stored credential")
)

// Account returns the account name for a profile and credential kind. An
// empty kind yields the legacy single-entry name, still read for backwards
// compatibility.
func Account(profile, kind string) string {
	if profile == "" {
		profile = "default"
	}

	if kind == "" {
		return profile
	}

	return profile + ":" + kind
}

// Set stores or replaces the token of an account.
func Set(account, token string) error {
	switch runtime.GOOS {
	case "darwin":
		// `security -i` reads commands from stdin, which keeps the token out of
		// argv. -U updates an existing item instead of failing.
		command := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n",
			quote(service), quote(account), quote(token))

		return run(exec.Command("security", "-i"), command)
	case "linux":
		if !hasSecretTool() {
			return ErrUnavailable
		}

		cmd := exec.Command("secret-tool", "store", "--label="+service, "service", service, "account", account)

		return run(cmd, token)
	}

	return ErrUnavailable
}

// Get returns the token of an account.
func Get(account string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
		if err != nil {
			if exitStatus(err) == macOSNotFound {
				return "", ErrNotFound
			}

			return "", ErrUnavailable
		}

		return strings.TrimRight(string(out), "\n"), nil
	case "linux":
		if !hasSecretTool() {
			return "", ErrUnavailable
		}

		// secret-tool exits 1 both when nothing matches and when the lookup
		// fails, so an empty result is the only reliable signal.
		out, err := exec.Command("secret-tool", "lookup", "service", service, "account", account).Output()
		if err != nil || len(out) == 0 {
			return "", ErrNotFound
		}

		return strings.TrimRight(string(out), "\n"), nil
	}

	return "", ErrUnavailable
}

// Delete removes the token of an account. Deleting a missing entry is not an
// error.
func Delete(account string) error {
	switch runtime.GOOS {
	case "darwin":
		err := run(exec.Command("security", "delete-generic-password", "-s", service, "-a", account), "")
		if exitStatus(err) == macOSNotFound {
			return nil
		}

		return err
	case "linux":
		if !hasSecretTool() {
			return ErrUnavailable
		}

		return run(exec.Command("secret-tool", "clear", "service", service, "account", account), "")
	}

	return ErrUnavailable
}

// Where names the credential store, for messages.
func Where() string {
	switch runtime.GOOS {
	case "darwin":
		return "the macOS keychain"
	case "linux":
		return "the Secret Service (secret-tool)"
	}

	return "an OS credential store"
}

func hasSecretTool() bool {
	_, err := exec.LookPath("secret-tool")

	return err == nil
}

// run executes cmd with stdin as its standard input. On failure, the returned
// error wraps the *exec.ExitError and includes the first line of output.
func run(cmd *exec.Cmd, stdin string) error {
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", cmd.Args[0], err, firstLine(out))
	}

	return nil
}

func exitStatus(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return -1
}

func firstLine(out []byte) string {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "no output"
	}

	line, _, _ := strings.Cut(text, "\n")

	return line
}

// quote wraps value in double quotes for the `security -i` command parser,
// escaping backslashes and double quotes.
func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
