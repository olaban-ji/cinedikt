package geoip_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cinedikt/internal/geoip"
	"cinedikt/internal/geoip/geoiptest"
)

// Networks MaxMind's own test data places, in a database built here.
var networks = map[string]string{
	"81.2.69.0/24":    "GB",
	"216.160.83.0/24": "US",
	"2001:480::/32":   "US",
}

func TestTheLookupPlacesAnAddressInACountry(t *testing.T) {
	var l geoip.Lookup
	if _, ok := l.Country(netip.MustParseAddr("81.2.69.142")); ok || l.Ready() {
		t.Fatal("a lookup with no database placed an address")
	}
	if err := l.Load(geoiptest.Database(t, networks), "Sat, 03 Oct 2026 00:00:00 GMT"); err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[string]string{
		"81.2.69.142":        "gb",
		"216.160.83.56":      "us",
		"2001:480::1":        "us",
		"::ffff:81.2.69.142": "gb",
	} {
		if got, ok := l.Country(netip.MustParseAddr(addr)); !ok || got != want {
			t.Errorf("%s = %q %v, want %q", addr, got, ok, want)
		}
	}
	if got, ok := l.Country(netip.MustParseAddr("8.8.8.8")); ok {
		t.Errorf("an address the database has no network for = %q", got)
	}
	if got, ok := l.Country(netip.Addr{}); ok {
		t.Errorf("no address = %q", got)
	}
	if l.LastModified() != "Sat, 03 Oct 2026 00:00:00 GMT" {
		t.Errorf("stamp = %q", l.LastModified())
	}
}

func TestTheLookupRefusesWhatIsNotACountryDatabase(t *testing.T) {
	var l geoip.Lookup
	good := geoiptest.Database(t, networks)
	if err := l.Load(good, "first"); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{
		"empty":        nil,
		"not a mmdb":   []byte("hello"),
		"cut short":    good[:len(good)/2],
		"another kind": geoiptest.WrongEdition(t),
	} {
		if err := l.Load(bad, "second"); err == nil {
			t.Errorf("%s was loaded", name)
		}
	}
	if l.LastModified() != "first" {
		t.Errorf("a refused database replaced the one in use: %q", l.LastModified())
	}
	if got, _ := l.Country(netip.MustParseAddr("81.2.69.142")); got != "gb" {
		t.Errorf("after the refusals = %q", got)
	}
}

// memStore keeps the database the way meta.geoip does, in memory.
type memStore struct {
	mu    sync.Mutex
	stamp string
	mmdb  []byte
	keeps int
	fail  error
}

func (m *memStore) GeoIP(context.Context) (string, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stamp, m.mmdb, nil
}

func (m *memStore) GeoIPStamp(context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stamp, nil
}

func (m *memStore) KeepGeoIP(_ context.Context, stamp string, mmdb []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.stamp, m.mmdb = stamp, mmdb
	m.keeps++
	return nil
}

func updater(srv *geoiptest.Server, store geoip.Store, l *geoip.Lookup) *geoip.Updater {
	return &geoip.Updater{
		AccountID:    geoiptest.Account,
		LicenseKey:   geoiptest.License,
		Store:        store,
		Lookup:       l,
		HTTP:         srv.Client(),
		PermalinkURL: srv.PermalinkURL(),
	}
}

// TestTheDatabaseIsDownloadedOnlyWhenItsBuildChanges: every check asks
// with a HEAD, and only a new Last-Modified is worth a GET.
func TestTheDatabaseIsDownloadedOnlyWhenItsBuildChanges(t *testing.T) {
	ctx := context.Background()
	built := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, geoiptest.Database(t, networks)), built)
	store := &memStore{}
	var l geoip.Lookup
	u := updater(srv, store, &l)

	first := geoiptest.Database(t, networks)
	srv.Set(geoiptest.Archive(t, first), built)
	res, err := u.Check(ctx)
	if err != nil || !res.Downloaded {
		t.Fatalf("first check: downloaded %v, err %v", res.Downloaded, err)
	}
	if res.Built != built.Format(http.TimeFormat) || res.Bytes != len(first) || res.Replaced != "" {
		t.Errorf("first check = %+v; want the build, its size, and nothing replaced", res)
	}
	if srv.Heads() != 1 || srv.Gets() != 1 || store.keeps != 1 {
		t.Errorf("first check: %d HEAD, %d GET, %d kept", srv.Heads(), srv.Gets(), store.keeps)
	}
	if got, _ := l.Country(netip.MustParseAddr("81.2.69.142")); got != "gb" {
		t.Errorf("after the download = %q", got)
	}

	for range 3 {
		if res, err := u.Check(ctx); err != nil || res.Downloaded || res.Built != store.stamp {
			t.Fatalf("an unchanged build: %+v, err %v", res, err)
		}
	}
	if srv.Heads() != 4 || srv.Gets() != 1 {
		t.Errorf("unchanged: %d HEAD, %d GET; want a HEAD each and no more GETs", srv.Heads(), srv.Gets())
	}

	// A new build, placing the address elsewhere, is fetched and used.
	srv.Set(geoiptest.Archive(t, geoiptest.Database(t, map[string]string{"81.2.69.0/24": "IE"})), built.Add(96*time.Hour))
	if res, err := u.Check(ctx); err != nil || !res.Downloaded || res.Replaced != built.Format(http.TimeFormat) {
		t.Fatalf("a new build: %+v, err %v; want it downloaded in place of the first", res, err)
	}
	if srv.Gets() != 2 || store.keeps != 2 {
		t.Errorf("new build: %d GET, %d kept", srv.Gets(), store.keeps)
	}
	if got, _ := l.Country(netip.MustParseAddr("81.2.69.142")); got != "ie" {
		t.Errorf("after the new build = %q", got)
	}
	if store.stamp != built.Add(96*time.Hour).Format("Mon, 02 Jan 2006 15:04:05 GMT") || l.LastModified() != store.stamp {
		t.Errorf("kept %q, in use %q", store.stamp, l.LastModified())
	}
}

// TestABadDownloadKeepsTheDatabaseInUse: whatever is wrong with what came
// down, nothing is kept and nothing is swapped.
func TestABadDownloadKeepsTheDatabaseInUse(t *testing.T) {
	ctx := context.Background()
	built := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, geoiptest.Database(t, networks)), built)
	store := &memStore{}
	var l geoip.Lookup
	u := updater(srv, store, &l)
	if _, err := u.Check(ctx); err != nil {
		t.Fatal(err)
	}
	good := store.stamp

	archive := geoiptest.Archive(t, geoiptest.Database(t, networks))
	for name, body := range map[string][]byte{
		"not gzip":      []byte("<html>maintenance</html>"),
		"cut short":     archive[:len(archive)/2],
		"no database":   geoiptest.ArchiveWithoutDatabase(t),
		"empty mmdb":    geoiptest.Archive(t, []byte{}),
		"wrong edition": geoiptest.Archive(t, geoiptest.WrongEdition(t)),
		"garbage mmdb":  geoiptest.Archive(t, []byte("not a database at all")),
	} {
		built = built.Add(time.Hour)
		srv.Set(body, built)
		if res, err := u.Check(ctx); err == nil || res.Downloaded {
			t.Errorf("%s: downloaded %v, err %v", name, res.Downloaded, err)
		}
		if store.stamp != good || l.LastModified() != good {
			t.Errorf("%s: kept %q, in use %q, want the good one %q", name, store.stamp, l.LastModified(), good)
		}
		if got, _ := l.Country(netip.MustParseAddr("81.2.69.142")); got != "gb" {
			t.Errorf("%s: lookups now say %q", name, got)
		}
	}

	// A good download that cannot be kept is not put in use either:
	// every process reads the kept copy, and they would disagree.
	srv.Set(geoiptest.Archive(t, geoiptest.Database(t, map[string]string{"81.2.69.0/24": "IE"})), built.Add(time.Hour))
	store.fail = errors.New("database down")
	if res, err := u.Check(ctx); err == nil || res.Downloaded {
		t.Errorf("an unkept download: downloaded %v, err %v", res.Downloaded, err)
	}
	if got, _ := l.Country(netip.MustParseAddr("81.2.69.142")); got != "gb" {
		t.Errorf("an unkept download was used: %q", got)
	}
}

// TestASecondProcessReadsTheKeptDatabase: it neither downloads nor even
// asks, and it follows a build another process fetched.
func TestASecondProcessReadsTheKeptDatabase(t *testing.T) {
	ctx := context.Background()
	built := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, geoiptest.Database(t, networks)), built)
	store := &memStore{}
	var first geoip.Lookup
	if _, err := updater(srv, store, &first).Check(ctx); err != nil {
		t.Fatal(err)
	}
	heads, gets := srv.Heads(), srv.Gets()

	var second geoip.Lookup
	other := updater(srv, store, &second)
	if err := other.LoadStored(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := second.Country(netip.MustParseAddr("216.160.83.56")); got != "us" {
		t.Errorf("second process = %q", got)
	}
	if srv.Heads() != heads || srv.Gets() != gets {
		t.Errorf("loading the kept copy asked MaxMind: %d HEAD, %d GET", srv.Heads()-heads, srv.Gets()-gets)
	}
	// Its own check finds the build already kept. That is not a download
	// it has news of: the process that kept the build had it.
	if res, err := other.Check(ctx); err != nil || res.Downloaded || res.Built != store.stamp {
		t.Errorf("the second process's check = %+v, %v; want the kept build and no download", res, err)
	}

	// The first process fetches a new build; the second follows it.
	srv.Set(geoiptest.Archive(t, geoiptest.Database(t, map[string]string{"216.160.83.0/24": "CA"})), built.Add(96*time.Hour))
	if _, err := updater(srv, store, &first).Check(ctx); err != nil {
		t.Fatal(err)
	}
	followCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		other.Follow(followCtx, 5*time.Millisecond)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for second.LastModified() != store.stamp && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	stop()
	<-done
	if got, _ := second.Country(netip.MustParseAddr("216.160.83.56")); got != "ca" {
		t.Errorf("after following = %q", got)
	}
}

// TestARefusedKeyIsErrKeyAndNeverQuoted: a wrong key, or a key without
// its account id, is refused as the key, and no error a caller would log
// carries the key: it goes in the Authorization header alone.
func TestARefusedKeyIsErrKeyAndNeverQuoted(t *testing.T) {
	ctx := context.Background()
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, geoiptest.Database(t, networks)), time.Now())
	for _, c := range []struct{ name, account, key string }{
		{"a wrong key", geoiptest.Account, "wrong-licence-key"},
		{"no account id", "", geoiptest.License},
	} {
		u := updater(srv, &memStore{}, &geoip.Lookup{})
		u.AccountID, u.LicenseKey = c.account, c.key
		_, err := u.Check(ctx)
		if !errors.Is(err, geoip.ErrKey) {
			t.Errorf("%s: err = %v, want ErrKey", c.name, err)
		}
		if err != nil && strings.Contains(err.Error(), c.key) {
			t.Errorf("%s: the error carries the key: %v", c.name, err)
		}
	}

	// Nor does a host that does not answer.
	u := updater(srv, &memStore{}, &geoip.Lookup{})
	u.PermalinkURL = "http://127.0.0.1:1" + geoiptest.PermalinkPath + "?suffix=tar.gz"
	if _, err := u.Check(ctx); err == nil || strings.Contains(err.Error(), geoiptest.License) {
		t.Errorf("err = %v", err)
	}
}

// TestMaxMindBeingDownIsADownloadError: no answer, or a server error, is
// MaxMind's trouble and fixes itself, and is typed as that, apart from a
// refused key and from a download that is wrong.
func TestMaxMindBeingDownIsADownloadError(t *testing.T) {
	ctx := context.Background()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "maintenance", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	u := &geoip.Updater{AccountID: geoiptest.Account, LicenseKey: geoiptest.License, Store: &memStore{}, Lookup: &geoip.Lookup{},
		PermalinkURL: down.URL + geoiptest.PermalinkPath + "?suffix=tar.gz"}
	var dl *geoip.DownloadError
	_, err := u.Check(ctx)
	if !errors.As(err, &dl) || dl.Status != http.StatusServiceUnavailable || dl.Method != http.MethodHead {
		t.Errorf("a 503: err = %v, want a DownloadError for the HEAD with its status", err)
	}
	if err == nil || err.Error() != "geoip: HEAD the database: HTTP 503" {
		t.Errorf("a 503 reads %q", err)
	}

	u.PermalinkURL = "http://127.0.0.1:1" + geoiptest.PermalinkPath + "?suffix=tar.gz"
	if _, err := u.Check(ctx); !errors.As(err, &dl) || dl.Status != 0 {
		t.Errorf("no answer: err = %v, want a DownloadError with no status", err)
	}

	srv := geoiptest.NewServer(t, []byte("<html>maintenance</html>"), time.Now())
	u = updater(srv, &memStore{}, &geoip.Lookup{})
	if _, err := u.Check(ctx); err == nil || errors.As(err, &dl) {
		t.Errorf("a download that is not a database: err = %v, want an error that is not MaxMind being down", err)
	}
	u.LicenseKey = "wrong"
	if _, err := u.Check(ctx); !errors.Is(err, geoip.ErrKey) || errors.As(err, &dl) {
		t.Errorf("a refused key: err = %v, want ErrKey alone", err)
	}
}

// TestThePermalinkIsFollowedToItsStorage: MaxMind's permalink answers a
// HEAD and a GET alike with a redirect to the file on its storage. Both
// are followed by the client the updater makes for itself, the stamp
// read is the storage's, and the credentials stay with MaxMind's host.
func TestThePermalinkIsFollowedToItsStorage(t *testing.T) {
	ctx := context.Background()
	built := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	archive := geoiptest.Archive(t, geoiptest.Database(t, networks))
	var heads, gets atomic.Int32
	var leaked atomic.Bool
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		w.Header().Set("Last-Modified", built.Format(http.TimeFormat))
		switch r.Method {
		case http.MethodHead:
			heads.Add(1)
		case http.MethodGet:
			gets.Add(1)
			_, _ = w.Write(archive)
		}
	}))
	defer storage.Close()
	// The same machine by another name, so the redirect leaves the
	// permalink's host the way MaxMind's leaves for its storage.
	elsewhere := strings.Replace(storage.URL, "127.0.0.1", "localhost", 1)
	permalink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != geoiptest.Account || pass != geoiptest.License {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, elsewhere+"/GeoLite2-Country.tar.gz?signature=made-up", http.StatusFound)
	}))
	defer permalink.Close()

	store := &memStore{}
	var l geoip.Lookup
	u := &geoip.Updater{
		AccountID:    geoiptest.Account,
		LicenseKey:   geoiptest.License,
		Store:        store,
		Lookup:       &l,
		PermalinkURL: permalink.URL + geoiptest.PermalinkPath + "?suffix=tar.gz",
	}
	if res, err := u.Check(ctx); err != nil || !res.Downloaded {
		t.Fatalf("first check: downloaded %v, err %v", res.Downloaded, err)
	}
	if res, err := u.Check(ctx); err != nil || res.Downloaded {
		t.Fatalf("an unchanged build: downloaded %v, err %v", res.Downloaded, err)
	}
	if heads.Load() != 2 || gets.Load() != 1 {
		t.Errorf("storage saw %d HEAD, %d GET; want two checks and one download", heads.Load(), gets.Load())
	}
	if store.stamp != built.Format(http.TimeFormat) {
		t.Errorf("kept %q", store.stamp)
	}
	if got, _ := l.Country(netip.MustParseAddr("81.2.69.142")); got != "gb" {
		t.Errorf("after the download = %q", got)
	}
	if leaked.Load() {
		t.Error("the credentials went with the redirect to the storage")
	}
}

// TestAGetBehindTheHeadIsNoDownload: the HEAD names a new build, but the
// GET is served the one already kept, as storage still catching up with
// a release can. Nothing new was fetched, so the check has no download to
// report, and the next check, served the new build, does.
func TestAGetBehindTheHeadIsNoDownload(t *testing.T) {
	ctx := context.Background()
	built := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	newer := built.Add(96 * time.Hour)
	archive := geoiptest.Archive(t, geoiptest.Database(t, networks))
	var mu sync.Mutex
	headStamp, getStamp := built, built
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stamp := headStamp
		if r.Method == http.MethodGet {
			stamp = getStamp
		}
		mu.Unlock()
		w.Header().Set("Last-Modified", stamp.Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(archive)
		}
	}))
	defer srv.Close()
	store := &memStore{}
	u := &geoip.Updater{AccountID: geoiptest.Account, LicenseKey: geoiptest.License, Store: store, Lookup: &geoip.Lookup{},
		PermalinkURL: srv.URL + geoiptest.PermalinkPath + "?suffix=tar.gz"}
	if res, err := u.Check(ctx); err != nil || !res.Downloaded {
		t.Fatalf("first check: %+v, err %v", res, err)
	}

	mu.Lock()
	headStamp = newer
	mu.Unlock()
	res, err := u.Check(ctx)
	if err != nil || res.Downloaded || res.Built != built.Format(http.TimeFormat) {
		t.Errorf("a GET behind the HEAD: %+v, err %v; want the kept build and no download", res, err)
	}

	mu.Lock()
	getStamp = newer
	mu.Unlock()
	res, err = u.Check(ctx)
	if err != nil || !res.Downloaded || res.Built != newer.Format(http.TimeFormat) || res.Replaced != built.Format(http.TimeFormat) {
		t.Errorf("once the GET catches up: %+v, err %v; want the new build downloaded in place of the first", res, err)
	}
}
