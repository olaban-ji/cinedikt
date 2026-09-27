package catalog

import (
	"time"

	"cinedikt/internal/notify"
)

// report hands an event to the sink, if there is one, stamped with the
// time it happened. A nil sink is the ordinary case: nothing is
// configured, and the jobs carry on.
func report(sink notify.Sink, e notify.Event) {
	if sink == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	sink.Note(e)
}

// pass is one run of a background job, as far as the notifier is
// concerned.
//
// A wake that finds an empty queue is the common case — the posters
// rest for twenty minutes and then look again — and that must not
// become news. start is called only once there is a batch to do, and
// finish and pause say nothing unless start did.
type pass struct {
	sink  notify.Sink
	job   string
	on    bool
	began time.Time
}

func (p *pass) start(total int64) {
	if p.on || p.sink == nil {
		return
	}
	p.on = true
	p.began = time.Now()
	report(p.sink, notify.Event{Job: p.job, Kind: notify.Started, Total: total})
}

// finish is the end of the work: done is what the pass exists to
// produce, none what came back with nothing to give, errs what failed.
func (p *pass) finish(done, none, errs int64) {
	if !p.on || p.sink == nil {
		return
	}
	p.on = false
	report(p.sink, notify.Event{Job: p.job, Kind: notify.Finished,
		Done: done, None: none, Errors: errs, Took: time.Since(p.began)})
}

// pause is a pass stopped on purpose until next.
func (p *pass) pause(cause notify.Cause, done, errs int64, next time.Time) {
	if !p.on || p.sink == nil {
		return
	}
	p.on = false
	report(p.sink, notify.Event{Job: p.job, Kind: notify.Paused, Cause: cause,
		Done: done, Errors: errs, NextTry: next, Took: time.Since(p.began)})
}
