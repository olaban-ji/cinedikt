package notify

import (
	"context"
	"testing"
	"time"
)

// plain is a sink with no memory and nothing to flush, which is what
// every test double in the catalog is.
type plain struct{ got []Event }

func (p *plain) Note(e Event) { p.got = append(p.got, e) }

type nowhere struct{}

func (nowhere) LoadNotifyState(context.Context) ([]byte, error) { return nil, nil }
func (nowhere) SaveNotifyState(context.Context, []byte) error   { return nil }

func TestAttachDetachAndCloseLeaveASinkWithoutThemAlone(t *testing.T) {
	// A nil sink is the ordinary case: nothing configured. No call
	// may panic on it or wait for anything.
	Attach(nil, nowhere{})
	start := time.Now()
	Close(nil, time.Second)

	Detach(nil)
	var p plain
	Attach(&p, nowhere{})
	Detach(&p)
	Close(&p, time.Second)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("Close waited on a sink that has nothing to flush")
	}
	if len(p.got) != 0 {
		t.Fatalf("a plain sink heard %d events from Attach, Detach or Close", len(p.got))
	}
}
