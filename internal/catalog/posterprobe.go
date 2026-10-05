package catalog

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// posterGone reports whether a poster address can be shown, and
// whether the host said so definitively.
//
// `missing` is "do not draw this": a 404, or an empty address. `gone`
// is narrower — the host answered, and the picture it once served is
// not there any more. Only that is worth writing down and asking
// another service about; a host that does not answer is neither, so
// Amazon being briefly unreachable never empties the cold screen and
// never puts a live poster into the repair queue.
//
// A 404 is not yet that answer. The image edges replay a miss for
// about five minutes and then serve the picture again, so the first
// one only keeps the film off this draw. A second, once that window
// has passed, is the picture actually being gone.
//
// Tests replace it so a fixture never asks the network.
var posterGone = rememberPosterGone

// posterMissTTL is how long an edge keeps a miss. max-age on those
// 404s is 300s; the extra slack is so a probe that lands as the entry
// is stored does not confirm it a moment too soon.
const posterMissTTL = 5*time.Minute + 15*time.Second

// posterNow is the clock the miss window is measured with. Tests move it.
var posterNow = time.Now

// posterMemo is one address's answer.
//
// at is set only for a miss that has not been confirmed. A final
// answer leaves it zero: the picture is there, or the miss has stuck.
type posterMemo struct {
	dead bool
	at   time.Time
}

// posterVerdict remembers an answer. The cold screen is asked on every
// arrival, and the same addresses come up again and again.
var posterVerdict sync.Map

var posterHTTP = &http.Client{Timeout: 2 * time.Second}

// rememberPosterGone asks the host once per address and remembers a
// definite answer. The cold screen is asked on every arrival and the
// same addresses come up again and again.
//
// An empty address is missing but not gone: there is nothing to have
// stopped answering. A miss inside the edge's error window is the same
// shape — not drawn, not written down — until a later ask still misses.
func rememberPosterGone(ctx context.Context, raw string) (missing, gone bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true, false
	}
	if v, ok := posterVerdict.Load(raw); ok {
		m := v.(posterMemo)
		if m.at.IsZero() {
			return m.dead, m.dead
		}
		if posterNow().Sub(m.at) < posterMissTTL {
			return true, false
		}
	}
	dead, known := askPoster(ctx, raw)
	if !known {
		// The confirmation itself failed to arrive. Keep the earlier
		// miss, if there was one, so a silent host is not asked on
		// every card of the same visit.
		if v, ok := posterVerdict.Load(raw); ok {
			m := v.(posterMemo)
			if !m.at.IsZero() {
				posterVerdict.Store(raw, posterMemo{dead: true, at: posterNow()})
				return true, false
			}
		}
		return false, false
	}
	if !dead {
		posterVerdict.Store(raw, posterMemo{})
		return false, false
	}
	if v, ok := posterVerdict.Load(raw); ok {
		m := v.(posterMemo)
		if !m.at.IsZero() && posterNow().Sub(m.at) >= posterMissTTL {
			posterVerdict.Store(raw, posterMemo{dead: true})
			return true, true
		}
	}
	posterVerdict.Store(raw, posterMemo{dead: true, at: posterNow()})
	return true, false
}

// askPoster asks the image host. known is false when the answer was not
// a real yes or no — a timeout, a 403, a 500 — so the caller can try
// again later instead of remembering a guess.
func askPoster(ctx context.Context, raw string) (missing, known bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return true, true
	}
	req.Header.Set("User-Agent", "cinedikt")
	resp, err := posterHTTP.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		return true, true
	case http.StatusOK, http.StatusNoContent, http.StatusPartialContent:
		return false, true
	default:
		return false, false
	}
}
