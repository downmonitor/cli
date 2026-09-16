package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, OK},
		{"verdict", Verdictf("down"), Verdict},
		{"usage", Usagef("bad flag"), Usage},
		{"unreachable", Unreachablef("no route"), Unreachable},
		{"timeout", Timeoutf("too slow"), Timeout},
		{"wrapped", fmt.Errorf("context: %w", Unreachablef("no route")), Unreachable},
		{"plain error", errors.New("boom"), Usage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Of(tt.err); got != tt.want {
				t.Errorf("Of() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestErrorUnwraps(t *testing.T) {
	cause := errors.New("cause")
	err := &Error{Code: Usage, Err: cause}

	if !errors.Is(err, cause) {
		t.Error("errors.Is did not find the wrapped error")
	}

	if err.Error() != "cause" {
		t.Errorf("Error() = %q", err.Error())
	}
}
