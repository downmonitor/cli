package ui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner shows an animated status line with the elapsed time while waiting.
//
// A disabled spinner is a no-op, which is how it stays out of pipes, CI logs
// and quiet mode. Callers must Stop it before writing to the same stream; Stop
// erases the line and is safe to call more than once, or on a nil Spinner.
type Spinner struct {
	w       io.Writer
	enabled bool
	color   bool

	mu      sync.Mutex
	text    string
	started time.Time
	stop    chan struct{}
	done    chan struct{}
}

// NewSpinner returns a spinner writing to w. It does nothing unless enabled.
func NewSpinner(w io.Writer, enabled, color bool) *Spinner {
	return &Spinner{w: w, enabled: enabled, color: color}
}

// Start shows the spinner with text, or replaces the text if it is already
// running. The elapsed time restarts in both cases.
func (s *Spinner) Start(text string) {
	if s == nil || !s.enabled {
		return
	}

	s.mu.Lock()
	s.text = text
	s.started = time.Now()
	running := s.stop != nil
	s.mu.Unlock()

	if running {
		return
	}

	s.stop = make(chan struct{})
	s.done = make(chan struct{})

	// Hide the cursor while redrawing; Stop shows it again.
	fmt.Fprint(s.w, "\033[?25l")

	go s.loop(s.stop, s.done)
}

// Update changes the text without restarting the clock.
func (s *Spinner) Update(text string) {
	if s == nil || !s.enabled {
		return
	}

	s.mu.Lock()
	s.text = text
	s.mu.Unlock()
}

// Stop erases the spinner line and restores the cursor.
func (s *Spinner) Stop() {
	if s == nil || !s.enabled || s.stop == nil {
		return
	}

	close(s.stop)
	<-s.done

	s.stop = nil
	s.done = nil

	fmt.Fprint(s.w, "\r\033[2K\033[?25h")
}

func (s *Spinner) loop(stop, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for frame := 0; ; frame++ {
		s.mu.Lock()
		text := s.text
		elapsed := time.Since(s.started)
		s.mu.Unlock()

		line := Paint(Cyan, frames[frame%len(frames)], s.color) + " " + text

		if elapsed >= time.Second {
			line += Paint(Grey, fmt.Sprintf(" · %.0fs", elapsed.Seconds()), s.color)
		}

		fmt.Fprint(s.w, "\r\033[2K"+line)

		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}
