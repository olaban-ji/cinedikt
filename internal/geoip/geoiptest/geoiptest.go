// Package geoiptest builds GeoLite2-shaped databases, and a stand-in for
// MaxMind's download, for tests. Nothing here talks to MaxMind.
package geoiptest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// Account and License are the credentials the stand-in takes. They are
// made up, and only ever sent to the stand-in.
const (
	Account = "000000"
	License = "test-license-not-real"
)

// Database is a GeoLite2 Country database placing each network, written
// as CIDR, in the country given by its ISO code.
func Database(tb testing.TB, networks map[string]string) []byte {
	return database(tb, "GeoLite2-Country", networks)
}

// WrongEdition is a valid MaxMind database of another edition, which a
// country lookup must refuse.
func WrongEdition(tb testing.TB) []byte {
	return database(tb, "GeoLite2-ASN", map[string]string{"81.2.69.0/24": "GB"})
}

func database(tb testing.TB, edition string, networks map[string]string) []byte {
	tb.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: edition,
		// A real database describes itself, and verifying one checks
		// that it does.
		Description: map[string]string{"en": edition + " made for a test"},
		Languages:   []string{"en"},
		RecordSize:  24,
	})
	if err != nil {
		tb.Fatal(err)
	}
	for cidr, code := range networks {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			tb.Fatal(err)
		}
		record := mmdbtype.Map{
			"country": mmdbtype.Map{"iso_code": mmdbtype.String(code)},
		}
		if err := tree.Insert(network, record); err != nil {
			tb.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if _, err := tree.WriteTo(&buf); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// Archive packs a database the way MaxMind does: a tar.gz with the
// database in a dated folder, beside its licence.
func Archive(tb testing.TB, mmdb []byte) []byte {
	tb.Helper()
	return pack(tb, []file{
		{"GeoLite2-Country_20261003/LICENSE.txt", []byte("licence")},
		{"GeoLite2-Country_20261003/GeoLite2-Country.mmdb", mmdb},
		{"GeoLite2-Country_20261003/COPYRIGHT.txt", []byte("copyright")},
	})
}

// ArchiveWithoutDatabase is MaxMind's packing with the database missing.
func ArchiveWithoutDatabase(tb testing.TB) []byte {
	tb.Helper()
	return pack(tb, []file{{"GeoLite2-Country_20261003/LICENSE.txt", []byte("licence")}})
}

type file struct {
	name string
	body []byte
}

func pack(tb testing.TB, files []file) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			tb.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			tb.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		tb.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// Server stands in for MaxMind's download. It answers HEAD and GET at
// both of MaxMind's addresses, with the credentials above, and counts
// what it was asked.
type Server struct {
	*httptest.Server
	heads, gets atomic.Int32

	mu           sync.Mutex
	body         []byte
	lastModified string
}

// PermalinkPath and LegacyPath are where the stand-in answers, in place
// of MaxMind's two addresses.
const (
	PermalinkPath = "/geoip/databases/GeoLite2-Country/download"
	LegacyPath    = "/app/geoip_download"
)

// NewServer serves body as the newest database, built at built.
func NewServer(tb testing.TB, body []byte, built time.Time) *Server {
	tb.Helper()
	s := &Server{}
	s.Set(body, built)
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	tb.Cleanup(s.Close)
	return s
}

// Set changes what the stand-in serves.
func (s *Server) Set(body []byte, built time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
	s.lastModified = built.UTC().Format(http.TimeFormat)
}

// PermalinkURL and LegacyURL are the stand-in's two addresses, shaped
// like MaxMind's.
func (s *Server) PermalinkURL() string { return s.URL + PermalinkPath + "?suffix=tar.gz" }
func (s *Server) LegacyURL() string {
	return s.URL + LegacyPath + "?edition_id=GeoLite2-Country&suffix=tar.gz"
}

// Heads and Gets are how many of each the stand-in has answered.
func (s *Server) Heads() int { return int(s.heads.Load()) }
func (s *Server) Gets() int  { return int(s.gets.Load()) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case PermalinkPath:
		user, pass, ok := r.BasicAuth()
		if !ok || user != Account || pass != License {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	case LegacyPath:
		if r.URL.Query().Get("license_key") != License {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	body, lm := s.body, s.lastModified
	s.mu.Unlock()
	w.Header().Set("Last-Modified", lm)
	w.Header().Set("Content-Type", "application/gzip")
	switch r.Method {
	case http.MethodHead:
		s.heads.Add(1)
	case http.MethodGet:
		s.gets.Add(1)
		_, _ = w.Write(body)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}
