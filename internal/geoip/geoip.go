// Package geoip places a reader's address in a country, with MaxMind's
// GeoLite2 Country database, and keeps that database up to date.
//
// The database is downloaded by the service itself: a HEAD asks MaxMind
// when the current one was built, and the file is fetched only when that
// has changed. What was fetched is kept beside the catalog, so a restart,
// or a second container during a deploy, reads it rather than asking
// MaxMind for it again.
//
// It reads the database with maxminddb-golang/v2 rather than
// geoip2-golang: the database lives in Postgres, not in a file, and v2
// opens one from bytes and looks up a netip.Addr directly, and decoding
// into a struct of our own reads the one field wanted instead of every
// name a GeoIP2 country record carries.
package geoip

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/oschwald/maxminddb-golang/v2"
)

// Lookup answers which country an address is in, from the last database
// it was given. A new one is swapped in whole, so a lookup reads either
// the old database or the new one and never waits for the change.
//
// The zero value has no database and places nothing.
type Lookup struct {
	db atomic.Pointer[database]
}

type database struct {
	reader *maxminddb.Reader
	// lastModified is MaxMind's stamp for the build, as it sent it. It is
	// how the processes tell one build from another.
	lastModified string
}

// countryRecord is the part of a GeoLite2 Country record this reads:
// the ISO code alone, rather than the country's name in every language,
// which is most of each record and would be decoded on every request.
type countryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	// Where the network is registered. MaxMind leaves country empty for
	// some networks it cannot place a user on, and the registration is
	// then the best guess there is.
	RegisteredCountry struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"registered_country"`
}

// Country is the ISO 3166-1 alpha-2 code of the country addr is in,
// lowercased. False when there is no database, or the database has no
// country for the address.
func (l *Lookup) Country(addr netip.Addr) (string, bool) {
	db := l.db.Load()
	if db == nil || !addr.IsValid() {
		return "", false
	}
	var rec countryRecord
	if err := db.reader.Lookup(addr.Unmap()).Decode(&rec); err != nil {
		return "", false
	}
	code := rec.Country.ISOCode
	if code == "" {
		code = rec.RegisteredCountry.ISOCode
	}
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) != 2 || code[0] < 'a' || code[0] > 'z' || code[1] < 'a' || code[1] > 'z' {
		return "", false
	}
	return code, true
}

// Ready reports whether there is a database to look in.
func (l *Lookup) Ready() bool { return l.db.Load() != nil }

// LastModified is the stamp of the database in use, or "" for none.
func (l *Lookup) LastModified() string {
	if db := l.db.Load(); db != nil {
		return db.lastModified
	}
	return ""
}

// Load opens mmdb, checks it is a whole country database, and puts it in
// use. One that fails the check leaves the database in use alone.
func (l *Lookup) Load(mmdb []byte, lastModified string) error {
	r, err := open(mmdb)
	if err != nil {
		return err
	}
	l.use(r, lastModified)
	return nil
}

func (l *Lookup) use(r *maxminddb.Reader, lastModified string) {
	// The old reader is not closed. It was opened from bytes, so there is
	// no file or mapping to give back, and a lookup that loaded it a
	// moment ago may still be reading it.
	l.db.Store(&database{reader: r, lastModified: lastModified})
}

// open reads a database from memory and checks it: that it is a MaxMind
// database at all, that it is a country edition, and that every node and
// record in it can be read. A truncated or wrong download fails here,
// before it can replace anything.
func open(mmdb []byte) (*maxminddb.Reader, error) {
	if len(mmdb) == 0 {
		return nil, errors.New("geoip: the database is empty")
	}
	r, err := maxminddb.OpenBytes(mmdb)
	if err != nil {
		return nil, fmt.Errorf("geoip: open the database: %w", err)
	}
	if !strings.Contains(r.Metadata.DatabaseType, "Country") {
		return nil, fmt.Errorf("geoip: a %q database is not a country database", r.Metadata.DatabaseType)
	}
	if err := r.Verify(); err != nil {
		return nil, fmt.Errorf("geoip: the database does not verify: %w", err)
	}
	return r, nil
}
