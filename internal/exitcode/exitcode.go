// Package exitcode defines the exit codes of the CLI and the error type that
// carries them.
//
// Exit codes are part of the public interface: scripts and CI pipelines branch
// on them. The important distinction is between Verdict (the target is down)
// and Unreachable (the check could not run at all), which a pipeline usually
// wants to handle differently. Only cmd/downmonitor calls os.Exit.
package exitcode

import (
	"errors"
	"fmt"
)

const (
	// OK means the target is up and every assertion passed.
	OK = 0

	// Verdict means the check ran and the target is down, or an assertion
	// failed.
	Verdict = 1

	// Usage means the command line was invalid: an unknown flag or region, an
	// unreadable file. Nothing was sent.
	Usage = 2

	// Unreachable means nothing was checked: the API could not be reached, the
	// credential was rejected or the quota is spent. --fail-open turns it into
	// OK.
	Unreachable = 3

	// Timeout means `watch --until` did not see its condition before
	// --timeout-until elapsed.
	Timeout = 4
)

// Error is an error annotated with the exit code it should produce.
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ExitCode implements Coded.
func (e *Error) ExitCode() int { return e.Code }

// Coded is implemented by errors that know their exit code. api.Error
// implements it as well, which keeps this package free of any dependency.
type Coded interface {
	error
	ExitCode() int
}

// Verdictf returns an error that exits with Verdict.
func Verdictf(format string, args ...any) *Error {
	return &Error{Code: Verdict, Err: fmt.Errorf(format, args...)}
}

// Usagef returns an error that exits with Usage.
func Usagef(format string, args ...any) *Error {
	return &Error{Code: Usage, Err: fmt.Errorf(format, args...)}
}

// Unreachablef returns an error that exits with Unreachable.
func Unreachablef(format string, args ...any) *Error {
	return &Error{Code: Unreachable, Err: fmt.Errorf(format, args...)}
}

// Timeoutf returns an error that exits with Timeout.
func Timeoutf(format string, args ...any) *Error {
	return &Error{Code: Timeout, Err: fmt.Errorf(format, args...)}
}

// Of returns the exit code for err. Errors that carry no code are treated as
// usage errors: they come from the CLI itself, before any verdict exists.
func Of(err error) int {
	if err == nil {
		return OK
	}

	var coded Coded
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}

	return Usage
}
