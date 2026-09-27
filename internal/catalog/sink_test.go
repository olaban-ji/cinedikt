package catalog

import (
	"sync"

	"cinedikt/internal/notify"
)

// recordingSink keeps every event it hears, in order. The jobs note
// from several goroutines at once, so it is behind a mutex.
type recordingSink struct {
	mu     sync.Mutex
	events []notify.Event
}

func (r *recordingSink) Note(e notify.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingSink) all() []notify.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]notify.Event(nil), r.events...)
}

// of is every event of one kind.
func (r *recordingSink) of(kind notify.Kind) []notify.Event {
	var out []notify.Event
	for _, e := range r.all() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
