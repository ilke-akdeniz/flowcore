package app

import (
	"fmt"
	"sync"
	"time"
)

// maxTraces is how much history the panel keeps. Enough to show the last few
// interactions, small enough that a long session does not grow without bound.
const maxTraces = 12

// Trace is one thing that happened, broken into the work this application did and
// the call it made to FlowCore.
//
// The panel this feeds is the point of the whole client. FlowCore's design is a
// boundary, and a boundary is invisible from one side: reading the API tells you
// what the library does, never what it refuses to do. Seeing four lines of
// application work around one line of library call is the argument, made in a way
// prose cannot.
type Trace struct {
	At    time.Time
	Title string
	Lines []TraceLine
}

// TraceLine is one step, attributed to whichever side of the boundary did it.
type TraceLine struct {
	// Source is "client", "call" or "return". The template renders them
	// differently so the one library call stands out from the work around it.
	Source string
	Text   string
}

// Tracer collects traces for one session.
type Tracer struct {
	mutex  sync.Mutex
	traces []Trace
}

// Start begins a trace. Lines are added to it, and it is recorded when finished.
func (t *Tracer) Start(title string) *Trace {
	return &Trace{At: time.Now(), Title: title}
}

// Client records work this application did that FlowCore neither does nor knows
// about: resolving identity, loading a subject, deciding what to dispatch.
func (tr *Trace) Client(format string, args ...any) *Trace {
	tr.Lines = append(tr.Lines, TraceLine{Source: "client", Text: fmt.Sprintf(format, args...)})

	return tr
}

// Call records a call into the library.
func (tr *Trace) Call(format string, args ...any) *Trace {
	tr.Lines = append(tr.Lines, TraceLine{Source: "call", Text: fmt.Sprintf(format, args...)})

	return tr
}

// Return records what came back.
func (tr *Trace) Return(format string, args ...any) *Trace {
	tr.Lines = append(tr.Lines, TraceLine{Source: "return", Text: fmt.Sprintf(format, args...)})

	return tr
}

// Record finishes a trace, keeping only the most recent few.
func (t *Tracer) Record(trace *Trace) {
	if trace == nil {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.traces = append([]Trace{*trace}, t.traces...)
	if len(t.traces) > maxTraces {
		t.traces = t.traces[:maxTraces]
	}
}

// Traces returns the recent history, newest first.
func (t *Tracer) Traces() []Trace {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	return append([]Trace(nil), t.traces...)
}
