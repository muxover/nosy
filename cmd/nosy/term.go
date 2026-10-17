package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// isTerminal is true when f is a character device, which a terminal is.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// status draws one line of progress with a spinner on w, replacing itself in
// place. Nothing is drawn unless the caller starts it, and it leaves the line
// empty when it stops.
type status struct {
	w    io.Writer
	mu   sync.Mutex
	text string
	spin int
	done chan struct{}
	wg   sync.WaitGroup
}

var spinFrames = []string{"|", "/", "-", `\`}

func startStatus(w io.Writer) *status {
	s := &status{w: w, done: make(chan struct{})}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(120 * time.Millisecond)
		defer t.Stop()
		for {
			s.draw()
			select {
			case <-s.done:
				return
			case <-t.C:
			}
		}
	}()
	return s
}

// set changes the step shown and draws it at once.
func (s *status) set(text string) {
	s.mu.Lock()
	s.text = text
	s.mu.Unlock()
	s.draw()
}

func (s *status) draw() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spin++
	fmt.Fprintf(s.w, "\r%s %-60s", spinFrames[s.spin%len(spinFrames)], s.text)
}

// stop ends the spinner and clears its line.
func (s *status) stop() {
	close(s.done)
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "\r%s\r", strings.Repeat(" ", 64))
}
