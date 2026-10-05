package catalog

import (
	"regexp"
	"strconv"
	"testing"
	"time"

	"cinedikt/internal/notify"
)

func TestProgressSaysHowFarAndHowLong(t *testing.T) {
	p := &progress{
		start: time.Now().Add(-(16*time.Minute + 35*time.Second)),
		total: 1_000_000,
	}
	got := fieldMap(p.fields(409632))

	if !regexp.MustCompile(`^\d+m\d+s$`).MatchString(got["elapsed"]) {
		t.Errorf("elapsed = %q, want a duration like 16m35s", got["elapsed"])
	}
	if got["progress"] != "41%" {
		t.Errorf("progress = %q, want 41%%", got["progress"])
	}
	if _, ok := got["rows"]; ok {
		t.Errorf("rows = %q, want the percentage instead", got["rows"])
	}
}

// TestProgressEventNeverReadsAsFinished is the board's side of a tick:
// a share that stops at 99%, so a pass that is nearly done is not
// shown as done, and no ETA until there is enough behind it to trust.
func TestProgressEventNeverReadsAsFinished(t *testing.T) {
	base := notify.Event{Job: notify.JobPosters}

	early := &progress{start: time.Now().Add(-10 * time.Minute), total: 1000}
	e := early.event(base, 40)
	if e.Kind != notify.Progress || e.Job != notify.JobPosters || e.Share != 0.04 || e.Total != 1000 {
		t.Fatalf("event = %+v, want a posters Progress with its share and total", e)
	}
	if !e.ETA.IsZero() {
		t.Errorf("ETA = %v at 4%%, want none before 5%%", e.ETA)
	}

	fresh := &progress{start: time.Now().Add(-30 * time.Second), total: 1000}
	if e := fresh.event(base, 500); !e.ETA.IsZero() {
		t.Errorf("ETA = %v after 30s, want none before two minutes", e.ETA)
	}

	going := &progress{start: time.Now().Add(-10 * time.Minute), total: 1000}
	e = going.event(base, 500)
	if e.ETA.IsZero() {
		t.Fatal("no ETA at 50% after ten minutes")
	}
	if left := time.Until(e.ETA); left < 9*time.Minute || left > 11*time.Minute {
		t.Errorf("ETA is %v away, want about ten minutes", left)
	}

	full := &progress{start: time.Now().Add(-10 * time.Minute), total: 1000}
	if e := full.event(base, 1000); e.Share != 0.99 {
		t.Errorf("share = %v at the last row, want 0.99 until the pass says it finished", e.Share)
	}

	download := &progress{start: time.Now(), bytes: true}
	if e := download.event(base, 5<<20); e.Bytes != 5<<20 || e.Share != 0 {
		t.Errorf("event = %+v, want bytes and no share when the length is unknown", e)
	}
}

func TestStepTellsTheSinkAndDoneDoesNot(t *testing.T) {
	var sink recordingSink
	p := newProgress(quietLogger(), "filling in posters", 100)
	p.watch(&sink, notify.Event{Job: notify.JobPosters})
	p.last = time.Now().Add(-ProgressEvery)
	p.step(10)
	p.done(100)
	got := sink.all()
	if len(got) != 1 || got[0].Kind != notify.Progress || got[0].Share != 0.1 || got[0].Total != 100 {
		t.Fatalf("events = %+v, want one Progress from the step and nothing from done", got)
	}
	if got[0].At.IsZero() {
		t.Error("a progress event went out without a time")
	}
}

func TestProgressWithoutATotalKeepsTheRowCount(t *testing.T) {
	p := &progress{start: time.Now().Add(-2 * time.Second)}
	got := fieldMap(p.fields(10))
	if got["rows"] != "10" {
		t.Errorf("rows = %q, want 10", got["rows"])
	}
	if _, ok := got["progress"]; ok {
		t.Errorf("progress = %q, want none without a total", got["progress"])
	}
	if !regexp.MustCompile(`^\d+s$`).MatchString(got["elapsed"]) {
		t.Errorf("elapsed = %q, want a duration like 2s", got["elapsed"])
	}
}

func fieldMap(fields []any) map[string]string {
	out := make(map[string]string, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key, _ := fields[i].(string)
		switch v := fields[i+1].(type) {
		case string:
			out[key] = v
		case int64:
			out[key] = strconv.FormatInt(v, 10)
		}
	}
	return out
}
