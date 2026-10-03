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

// MaxMind's two addresses for the newest GeoLite2 Country. The permalink
// takes the account id and the license key as Basic auth and redirects to
// the file on MaxMind's storage; the older address takes the key alone,
// in the query.
const (
	permalinkURL = "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz"
	legacyURL    = "https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-Country&suffix=tar.gz"
)

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
	// AccountID and LicenseKey are MaxMind's credentials. Without an
	// account id the older address is used, which takes the key alone.
	AccountID  string
	LicenseKey string
	Store      Store
	Lookup     *Lookup
	// HTTP is the client downloads go through. Nil takes one with a
	// timeout long enough for the file on a slow day.
	HTTP   *http.Client
	Logger *slog.Logger
	// PermalinkURL and LegacyURL replace MaxMind's addresses, for the
	// tests. Empty takes the real ones.
	PermalinkURL string
	LegacyURL    string
}

// Check asks MaxMind when the newest database was built and downloads it
// only when that is not the build already kept. A HEAD costs nothing
// against MaxMind's download limits, and a GET does, so the check can run
// as often as it likes.
//
// A download that is not a whole country database is refused, and the
// database in use stays. updated is whether a new one was put in use.
func (u *Updater) Check(ctx context.Context) (updated bool, err error) {
	kept, err := u.Store.GeoIPStamp(ctx)
	if err != nil {
		return false, err
	}
	head, err := u.fetch(ctx, http.MethodHead)
	if err != nil {
		return false, err
	}
	_ = head.Body.Close()
	built := strings.TrimSpace(head.Header.Get("Last-Modified"))
	if built == "" {
		return false, errors.New("geoip: MaxMind sent no Last-Modified")
	}
	if built == kept {
		// Nothing new to fetch. This process may still be without the
		// kept copy, the first time it checks.
		if u.Lookup.LastModified() != kept {
			return false, u.LoadStored(ctx)
		}
		return false, nil
	}

	resp, err := u.fetch(ctx, http.MethodGet)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// The file's own stamp, when it has one: a build that landed between
	// the HEAD and the GET is the one that was downloaded.
	if lm := strings.TrimSpace(resp.Header.Get("Last-Modified")); lm != "" {
		built = lm
	}
	mmdb, err := extract(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return false, err
	}
	r, err := open(mmdb)
	if err != nil {
		return false, err
	}
	if err := u.Store.KeepGeoIP(ctx, built, mmdb); err != nil {
		return false, err
	}
	u.Lookup.use(r, built)
	if u.Logger != nil {
		u.Logger.Info("downloaded GeoLite2 Country", "built", built, "bytes", len(mmdb))
	}
	return true, nil
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

// fetch makes one request for the database, by whichever address the
// credentials allow. An error never quotes the address: the older one
// carries the license key in its query.
func (u *Updater) fetch(ctx context.Context, method string) (*http.Response, error) {
	var target string
	if u.AccountID != "" {
		target = u.PermalinkURL
		if target == "" {
			target = permalinkURL
		}
	} else {
		target = u.LegacyURL
		if target == "" {
			target = legacyURL
		}
		target += "&license_key=" + url.QueryEscape(u.LicenseKey)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, errors.New("geoip: build the download request")
	}
	if u.AccountID != "" {
		// Go drops this header when the redirect leaves MaxMind's host,
		// so the storage the file is served from never sees it.
		req.SetBasicAuth(u.AccountID, u.LicenseKey)
	}
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
		return nil, fmt.Errorf("geoip: %s the database: %w", method, err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, fmt.Errorf("%w (HTTP %d)", ErrKey, resp.StatusCode)
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("geoip: %s the database: HTTP %d", method, resp.StatusCode)
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
