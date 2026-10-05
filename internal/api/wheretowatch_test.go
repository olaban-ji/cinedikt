package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"cinedikt/internal/catalog"
	"cinedikt/internal/streaming"
)

// fakeGeo places the addresses it is given and nothing else.
type fakeGeo map[string]string

func (g fakeGeo) Country(addr netip.Addr) (string, bool) {
	cc, ok := g[addr.String()]
	return cc, ok
}

func (fakeGeo) Ready() bool { return true }

// unloadedGeo is GeoLite2 set up with no database loaded yet: it places
// nobody.
type unloadedGeo struct{}

func (unloadedGeo) Country(netip.Addr) (string, bool) { return "", false }
func (unloadedGeo) Ready() bool                       { return false }

var geo = fakeGeo{
	"81.2.69.142":   "GB",
	"216.160.83.56": "US",
	"2001:480::1":   "US",
	"89.160.20.112": "SE",
	// Addresses no reader has; the lookup must never be asked about them.
	"127.0.0.1": "US",
	"10.0.0.7":  "US",
	"::1":       "US",
}

func request(headers map[string]string, remote string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/where-to-watch/tt0133093", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// TestNoGeoHeaderIsTrusted: Railway's edge sets no country header, so a
// reader who sends one is placed by their address all the same, and
// cannot pick a country to have the metered API asked about.
func TestNoGeoHeaderIsTrusted(t *testing.T) {
	for _, h := range []string{"CF-IPCountry", "CloudFront-Viewer-Country", "X-Vercel-IP-Country"} {
		r := request(map[string]string{h: "FR", "X-Real-IP": "81.2.69.142"}, "")
		if got := countryOf(r, geo); got != "gb" {
			t.Errorf("%s was trusted: %q", h, got)
		}
	}
}

// TestTheCountryComesFromTheClientsAddress: X-Real-IP, then the last
// X-Forwarded-For entry (the one the proxy appended), then the connection
// itself. The first entry is the client's to write, so it never counts.
func TestTheCountryComesFromTheClientsAddress(t *testing.T) {
	for _, c := range []struct {
		name    string
		headers map[string]string
		remote  string
		want    string
	}{
		{"X-Real-IP", map[string]string{"X-Real-IP": "81.2.69.142", "X-Forwarded-For": "216.160.83.56"}, "89.160.20.112:1", "gb"},
		{"the last X-Forwarded-For", map[string]string{"X-Forwarded-For": "81.2.69.142, 216.160.83.56"}, "89.160.20.112:1", "us"},
		{"a spoofed first X-Forwarded-For is ignored", map[string]string{"X-Forwarded-For": "216.160.83.56,81.2.69.142"}, "89.160.20.112:1", "gb"},
		{"an unreadable X-Real-IP", map[string]string{"X-Real-IP": "unknown", "X-Forwarded-For": "81.2.69.142"}, "89.160.20.112:1", "gb"},
		{"the connection", nil, "89.160.20.112:52100", "se"},
		{"IPv6 in brackets", nil, "[2001:480::1]:443", "us"},
		{"IPv6 in a header", map[string]string{"X-Real-IP": "2001:480::1"}, "", "us"},
		{"IPv4 mapped into IPv6", map[string]string{"X-Real-IP": "::ffff:81.2.69.142"}, "", "gb"},
	} {
		if got := countryOf(request(c.headers, c.remote), geo); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestAnAddressThatCannotBePlacedHasNoCountry: loopback and private
// addresses are nobody's country, whatever a database says, and neither
// is an address the database does not know.
func TestAnAddressThatCannotBePlacedHasNoCountry(t *testing.T) {
	for _, c := range []struct {
		name    string
		headers map[string]string
		remote  string
	}{
		{"loopback", nil, "127.0.0.1:5173"},
		{"IPv6 loopback", nil, "[::1]:5173"},
		{"private", map[string]string{"X-Real-IP": "10.0.0.7"}, "127.0.0.1:1"},
		{"unknown to the database", map[string]string{"X-Real-IP": "8.8.8.8"}, ""},
		{"nothing readable", map[string]string{"X-Real-IP": "nope", "X-Forwarded-For": "also nope"}, "garbage"},
	} {
		if got := countryOf(request(c.headers, c.remote), geo); got != "" {
			t.Errorf("%s: %q, want none", c.name, got)
		}
	}
	// Without a database nobody is placed, whatever header they send.
	if got := countryOf(request(map[string]string{"X-Real-IP": "81.2.69.142"}, ""), nil); got != "" {
		t.Errorf("no database placed an address in %q", got)
	}
	if got := countryOf(request(map[string]string{"CF-IPCountry": "GB"}, ""), nil); got != "" {
		t.Errorf("no database, a header placed a reader in %q", got)
	}
}

// fakeWatch answers as told and records the country it was asked about.
type fakeWatch struct {
	mu    sync.Mutex
	reply catalog.WatchReply
	err   error
	asked []string
}

func (f *fakeWatch) Answer(_ context.Context, tconst, country string) (catalog.WatchReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, tconst+"/"+country)
	return f.reply, f.err
}

func askWatch(t *testing.T, s *CatalogServer, id string, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if r == nil {
		r = httptest.NewRequest(http.MethodGet, "/where-to-watch/"+id, nil)
	}
	r.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.whereToWatch(rec, r)
	return rec
}

func watchServer(w WhereToWatch, g CountryLookup) *CatalogServer {
	s := NewCatalogServer(nil, discardLogger())
	if w != nil {
		s.WithWhereToWatch(w, g)
	}
	return s
}

func TestAnAnswerIsKeptByTheReadersBrowserForAnHour(t *testing.T) {
	answer := &streaming.Answer{
		Stream: []streaming.Offer{{ID: "netflix", Name: "Netflix", Link: "https://netflix/title/1",
			Logo: streaming.Logo{Dark: "d.svg", Light: "l.svg"}}},
		Free: []streaming.Offer{},
		Rent: []streaming.Offer{{ID: "prime", Name: "Prime Video", Link: "p", Price: "3.99 USD"}},
		Buy:  []streaming.Offer{},
	}
	watch := &fakeWatch{reply: catalog.WatchReply{Country: "us", CountryName: "United States", Covered: true, Answer: answer}}
	r := request(map[string]string{"X-Real-IP": "216.160.83.56"}, "")
	rec := askWatch(t, watchServer(watch, geo), "tt0133093", r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=3600" {
		t.Errorf("Cache-Control = %q", got)
	}
	if len(watch.asked) != 1 || watch.asked[0] != "tt0133093/us" {
		t.Errorf("asked = %v", watch.asked)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"country", "countryName", "covered", "stream", "free", "rent", "buy"} {
		if _, ok := body[k]; !ok {
			t.Errorf("the body has no %q: %s", k, rec.Body)
		}
	}
	if string(body["country"]) != `"us"` || string(body["covered"]) != "true" || string(body["free"]) != "[]" {
		t.Errorf("body = %s", rec.Body)
	}
	if !strings.Contains(string(body["rent"]), `"price":"3.99 USD"`) || !strings.Contains(string(body["stream"]), `"logo":{"dark":"d.svg","light":"l.svg"}`) {
		t.Errorf("lists = %s", rec.Body)
	}
}

// TestAReaderWhoCannotBePlacedIsTold: the real service, with no store
// and no API behind it, answers a reader with no country as a country
// without coverage, which proves it reaches neither.
func TestAReaderWhoCannotBePlacedIsTold(t *testing.T) {
	s := watchServer(&catalog.WhereToWatch{}, geo)
	rec := askWatch(t, s, "tt0133093", request(nil, "127.0.0.1:5173"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"country":"xx","covered":false}` {
		t.Errorf("body = %s", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=3600" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestWhereToWatchFailuresAreNeverCachedOrExplained(t *testing.T) {
	for _, c := range []struct {
		name  string
		watch WhereToWatch
		id    string
		code  int
	}{
		{"a bad id", &fakeWatch{}, "nm0000206", http.StatusBadRequest},
		{"no key, so no service", nil, "tt0133093", http.StatusServiceUnavailable},
		{"a movie the catalog does not hold", &fakeWatch{err: fmt.Errorf("catalog: tt9: %w", catalog.ErrNotFound)}, "tt9999999", http.StatusNotFound},
		{"the API failing", &fakeWatch{err: fmt.Errorf("%w: %w", catalog.ErrUpstream, errors.New("streaming: HTTP 500: secret upstream detail key=abc"))}, "tt0133093", http.StatusBadGateway},
		{"the key refused", &fakeWatch{err: fmt.Errorf("%w: %w", catalog.ErrUpstream, streaming.ErrKey)}, "tt0133093", http.StatusBadGateway},
		{"the store failing", &fakeWatch{err: errors.New("catalog: where to watch tt0133093/us: dial tcp: connection refused, secret")}, "tt0133093", http.StatusInternalServerError},
	} {
		rec := askWatch(t, watchServer(c.watch, geo), c.id, nil)
		if rec.Code != c.code {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, c.code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", c.name, got)
		}
		body := rec.Body.String()
		if strings.Contains(body, "secret") || strings.Contains(body, "key=") || strings.Contains(body, "refused") {
			t.Errorf("%s: the body explains too much: %s", c.name, body)
		}
		var parsed map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil || parsed["covered"] != nil {
			t.Errorf("%s: body = %s; an error never claims anything about coverage", c.name, body)
		}
	}
}

// TestAReaderIsNotToldThereIsNoCoverageBeforeGeoLite2Loads: with a
// license key set and no database in yet, a reader the lookup would have
// placed is answered 503, kept by nobody, rather than a country without
// coverage kept for an hour.
func TestAReaderIsNotToldThereIsNoCoverageBeforeGeoLite2Loads(t *testing.T) {
	watch := &fakeWatch{reply: catalog.WatchReply{Country: "gb", CountryName: "United Kingdom", Covered: true, Answer: &streaming.Answer{}}}
	s := watchServer(watch, unloadedGeo{})
	rec := askWatch(t, s, "tt0133093", request(map[string]string{"X-Real-IP": "81.2.69.142"}, ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if strings.Contains(rec.Body.String(), "covered") {
		t.Errorf("body = %s; an error never claims anything about coverage", rec.Body)
	}
	if len(watch.asked) != 0 {
		t.Errorf("the service was asked: %v", watch.asked)
	}

	// Nor does a header naming their country place them.
	rec = askWatch(t, s, "tt0133093", request(map[string]string{"CF-IPCountry": "GB"}, ""))
	if rec.Code != http.StatusServiceUnavailable || len(watch.asked) != 0 {
		t.Errorf("a country header: status = %d, asked = %v", rec.Code, watch.asked)
	}

	// Without a license key there is no lookup to wait for.
	rec = askWatch(t, watchServer(&catalog.WhereToWatch{}, nil), "tt0133093", request(map[string]string{"X-Real-IP": "81.2.69.142"}, ""))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"country":"xx","covered":false}` {
		t.Errorf("no lookup: status = %d, body = %s", rec.Code, rec.Body)
	}
}

// TestTheRouteIsMounted: the catalog's routes include it, and with no
// service behind it it says so rather than claiming no coverage.
func TestTheRouteIsMounted(t *testing.T) {
	srv := New(NewCatalogServer(nil, discardLogger()), discardLogger())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/where-to-watch/tt0133093", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
}
