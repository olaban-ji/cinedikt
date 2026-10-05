package geoip

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// permalinkURL is MaxMind's address for the newest GeoLite2 Country. It
// takes the account id and the license key as Basic auth and redirects to
// the file on MaxMind's storage.
const permalinkURL = "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz"

// Bounds on a download. GeoLite2 Country is a few megabytes packed and
// under ten unpacked; these are far above that and far below what would
// hurt, so a wrong or runaway response is refused rather than read.
const (
	maxDownload = 64 << 20
	maxDatabase = 64 << 20
)

// ErrKey is MaxMind refusing the license key, or the account id with it.
// Only a person can fix it.
var ErrKey = errors.New("geoip: MaxMind refused the license key")

// DownloadError is MaxMind not answering, or answering with an error that
// is not about the key. Status is zero when no answer came back at all: a
// refused connection, a timeout. It is typed so the notifier can tell
// MaxMind being down, which fixes itself, from a fault in the download.
type DownloadError struct {
	Method string
	Status int
	Err    error
}

func (e *DownloadError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("geoip: %s the database: HTTP %d", e.Method, e.Status)
	}
	return fmt.Sprintf("geoip: %s the database: %v", e.Method, e.Err)
}

func (e *DownloadError) Unwrap() error { return e.Err }

// Result is what one check found.
type Result struct {
	// Built is MaxMind's stamp for the build in use once the check is
	// done.
	Built string
	// Downloaded is a new build this check fetched, kept and put in use.
	// Loading the build another process kept is not one: only the check
	// that kept a build has news about it. Nor is a download that turned
	// out to be the build already kept.
	Downloaded bool
	// Bytes is the downloaded database's size, unpacked.
	Bytes int
	// Replaced is the stamp of the kept build the download replaced, or
	// "" when there was none.
	Replaced string
}

// Store is where the database is kept between processes and restarts.
type Store interface {
	// GeoIP is the kept database and its stamp, or "" and nil for none.
	GeoIP(ctx context.Context) (lastModified string, mmdb []byte, err error)
	// GeoIPStamp is the kept database's stamp alone, or "" for none. It
	// is what a process reads to see whether another has fetched a newer
	// one, so it does not read the database itself.
	GeoIPStamp(ctx context.Context) (string, error)
	// KeepGeoIP replaces the kept database.
	KeepGeoIP(ctx context.Context, lastModified string, mmdb []byte) error
}

// Updater keeps a Lookup's database current from MaxMind.
type Updater struct {
	// AccountID and LicenseKey are MaxMind's credentials, which it takes
	// together.
	AccountID  string
	LicenseKey string
	Store      Store
	Lookup     *Lookup
	// HTTP is the client downloads go through. Nil takes one with a
	// timeout long enough for the file on a slow day.
	HTTP   *http.Client
	Logger *slog.Logger
	// PermalinkURL replaces MaxMind's address, for the tests. Empty takes
	// the real one.
	PermalinkURL string
}

// Check asks MaxMind when the newest database was built and downloads it
// only when that is not the build already kept. A HEAD costs nothing
// against MaxMind's download limits, and a GET does, so the check can run
// as often as it likes.
//
// A download that is not a whole country database is refused, and the
// database in use stays.
func (u *Updater) Check(ctx context.Context) (Result, error) {
	kept, err := u.Store.GeoIPStamp(ctx)
	if err != nil {
		return Result{}, err
	}
	head, err := u.fetch(ctx, http.MethodHead)
	if err != nil {
		return Result{}, err
	}
	_ = head.Body.Close()
	built := strings.TrimSpace(head.Header.Get("Last-Modified"))
	if built == "" {
		return Result{}, errors.New("geoip: MaxMind sent no Last-Modified")
	}
	if built == kept {
		// Nothing new to fetch. This process may still be without the
		// kept copy, the first time it checks.
		if u.Lookup.LastModified() != kept {
			if err := u.LoadStored(ctx); err != nil {
				return Result{}, err
			}
		}
		return Result{Built: kept}, nil
	}

	resp, err := u.fetch(ctx, http.MethodGet)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	// The file's own stamp, when it has one: a build that landed between
	// the HEAD and the GET is the one that was downloaded.
	if lm := strings.TrimSpace(resp.Header.Get("Last-Modified")); lm != "" {
		built = lm
	}
	mmdb, err := extract(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return Result{}, err
	}
	r, err := open(mmdb)
	if err != nil {
		return Result{}, err
	}
	if err := u.Store.KeepGeoIP(ctx, built, mmdb); err != nil {
		return Result{}, err
	}
	u.Lookup.use(r, built)
	if u.Logger != nil {
		u.Logger.Info("downloaded GeoLite2 Country", "built", built, "bytes", len(mmdb))
	}
	// A GET served the build already kept, by storage still behind on
	// the release the HEAD saw, has fetched nothing new. It is kept
	// again, harmlessly, and the next check fetches the newer build.
	return Result{Built: built, Downloaded: built != kept, Bytes: len(mmdb), Replaced: kept}, nil
}

// LoadStored puts the kept database in use, without asking MaxMind. It is
// how a process starts, and how one picks up a build another fetched.
func (u *Updater) LoadStored(ctx context.Context) error {
	built, mmdb, err := u.Store.GeoIP(ctx)
	if err != nil {
		return err
	}
	if built == "" {
		return nil
	}
	return u.Lookup.Load(mmdb, built)
}

// Follow loads a newer kept database whenever one appears, until ctx is
// done. Only one process downloads; every process serves lookups, and
// this is how the others catch up. Reading the stamp is one row's text
// column, so looking every few minutes costs nothing.
func (u *Updater) Follow(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		stamp, err := u.Store.GeoIPStamp(ctx)
		if err != nil || stamp == "" || stamp == u.Lookup.LastModified() {
			continue
		}
		if err := u.LoadStored(ctx); err != nil && ctx.Err() == nil && u.Logger != nil {
			u.Logger.Warn("load the kept GeoLite2 database", "err", err)
		}
	}
}

// fetch makes one request for the database. The credentials go only in
// the Authorization header, never in the address.
func (u *Updater) fetch(ctx context.Context, method string) (*http.Response, error) {
	target := u.PermalinkURL
	if target == "" {
		target = permalinkURL
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, errors.New("geoip: build the download request")
	}
	// Go drops this header when the redirect leaves MaxMind's host, so
	// the storage the file is served from never sees it.
	req.SetBasicAuth(u.AccountID, u.LicenseKey)
	client := u.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, &DownloadError{Method: method, Err: err}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, fmt.Errorf("%w (HTTP %d)", ErrKey, resp.StatusCode)
	default:
		resp.Body.Close()
		return nil, &DownloadError{Method: method, Status: resp.StatusCode}
	}
}

// extract is the .mmdb inside MaxMind's tar.gz, which holds it as
// GeoLite2-Country_YYYYMMDD/GeoLite2-Country.mmdb beside the licence and
// the copyright notice.
func extract(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("geoip: the download is not gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("geoip: the download has no .mmdb in it")
		}
		if err != nil {
			return nil, fmt.Errorf("geoip: read the download: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !strings.HasSuffix(h.Name, ".mmdb") {
			continue
		}
		if h.Size > maxDatabase {
			return nil, fmt.Errorf("geoip: the database is %d bytes, more than %d", h.Size, maxDatabase)
		}
		mmdb, err := io.ReadAll(io.LimitReader(tr, maxDatabase+1))
		if err != nil {
			return nil, fmt.Errorf("geoip: read the database: %w", err)
		}
		if len(mmdb) > maxDatabase {
			return nil, fmt.Errorf("geoip: the database is more than %d bytes", maxDatabase)
		}
		return mmdb, nil
	}
}
