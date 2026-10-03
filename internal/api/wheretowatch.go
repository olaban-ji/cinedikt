package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"cinedikt/internal/catalog"
)

// WhereToWatch is the service behind GET /where-to-watch/{id}.
type WhereToWatch interface {
	// Answer is where the movie can be watched in country, a lowercased
	// ISO code, or "" for a reader who could not be placed.
	Answer(ctx context.Context, tconst, country string) (catalog.WatchReply, error)
}

// CountryLookup places an address in a country: GeoLite2, in this app.
type CountryLookup interface {
	Country(addr netip.Addr) (string, bool)
	// Ready is whether there is a database to look in yet.
	Ready() bool
}

// WithWhereToWatch answers GET /where-to-watch/{id} from watch, placing
// readers with geo, or with a CDN's country header alone (WithGeoHeader)
// when geo is nil.
// Without it the route answers 503, which the page treats as it treats an
// error: it leaves the section out. Saying "no coverage" instead would
// tell the reader something untrue about their country.
func (s *CatalogServer) WithWhereToWatch(watch WhereToWatch, geo CountryLookup) {
	s.watch = watch
	s.geo = geo
}

// WithGeoHeader trusts one country header, the one a CDN put in front of
// the app sets on every request, such as Cloudflare's CF-IPCountry. Empty,
// the default, trusts none. Railway's edge sets no country header, so
// without a CDN any such header came from the reader, who could name any
// covered country with it and have the metered API asked on its behalf.
func (s *CatalogServer) WithGeoHeader(name string) {
	s.geoHeader = strings.TrimSpace(name)
}

// watchCacheControl lets the reader's own browser keep an answer for an
// hour, and nothing between: the answer depends on where the reader is,
// so a shared cache would hand one country's services to another.
const watchCacheControl = "private, max-age=3600"

// whereToWatch is GET /where-to-watch/{id}: the services a movie streams
// on, is free on, and can be rented or bought from, in the reader's
// country. The server works the country out; the page never sends one.
func (s *CatalogServer) whereToWatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validTConst(id) {
		noStore(w)
		writeError(w, http.StatusBadRequest, "id must be an IMDb title id, such as tt0133093")
		return
	}
	if s.watch == nil {
		noStore(w)
		writeError(w, http.StatusServiceUnavailable, "where to watch is not set up")
		return
	}
	country := countryOf(r, s.geo, s.geoHeader)
	if country == "" && s.geo != nil && !s.geo.Ready() {
		// GeoLite2 is set up but has no database yet: on the first deploy
		// with a license key, until the download is in, or in a second
		// process until it picks that download up. Telling the reader
		// their country has no coverage would be untrue, and their browser
		// would keep it for an hour; an error is kept by nobody, and the
		// page leaves the section out.
		noStore(w)
		writeError(w, http.StatusServiceUnavailable, "where to watch is not ready")
		return
	}
	reply, err := s.watch.Answer(r.Context(), id, country)
	if err != nil {
		if gone(r) {
			return
		}
		noStore(w)
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no movie with that id")
			return
		}
		// The service's own error stays in the log. It may name the
		// API's status or its message, and none of that is the reader's.
		if errors.Is(err, catalog.ErrUpstream) {
			if s.Logger != nil {
				s.Logger.Warn("where to watch", "id", id, "err", err)
			}
			writeError(w, http.StatusBadGateway, "could not find where to watch this movie")
			return
		}
		if s.Logger != nil {
			s.Logger.Error("where to watch", "id", id, "err", err)
		}
		writeError(w, http.StatusInternalServerError, "could not read where to watch")
		return
	}
	w.Header().Set("Cache-Control", watchCacheControl)
	writeJSON(w, http.StatusOK, reply)
}

// countryOf is the reader's country, lowercased, or "" when it cannot be
// told:
//
//  1. the trusted geo header, when one is configured (WithGeoHeader),
//     unless it says Cloudflare's XX (unknown) or T1 (Tor);
//  2. otherwise the client's address (clientAddr), looked up in GeoLite2;
//  3. a private, loopback or otherwise unroutable address, or one the
//     database has no country for, cannot be placed.
func countryOf(r *http.Request, geo CountryLookup, geoHeader string) string {
	if geoHeader != "" {
		if cc, ok := headerCountry(r.Header.Get(geoHeader)); ok {
			return cc
		}
	}
	if geo == nil {
		return ""
	}
	addr, ok := clientAddr(r)
	if !ok || !routable(addr) {
		return ""
	}
	if cc, ok := geo.Country(addr); ok {
		return strings.ToLower(cc)
	}
	return ""
}

func headerCountry(v string) (string, bool) {
	v = strings.ToUpper(strings.TrimSpace(v))
	if len(v) != 2 || v[0] < 'A' || v[0] > 'Z' || v[1] < 'A' || v[1] > 'Z' {
		return "", false
	}
	if v == "XX" || v == "T1" {
		return "", false
	}
	return strings.ToLower(v), true
}

// clientAddr is the address the request came from: X-Real-IP, which
// Railway's edge sets to the client's address, then the LAST
// X-Forwarded-For entry, the one the nearest proxy appended, then the
// connection itself, which is the client only when nothing stands in
// front. The first X-Forwarded-For entry is never read: a client can send
// the header with any address it likes, and the proxy only appends to it.
func clientAddr(r *http.Request) (netip.Addr, bool) {
	if addr, ok := parseAddr(r.Header.Get("X-Real-IP")); ok {
		return addr, true
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		entries := strings.Split(xff, ",")
		if addr, ok := parseAddr(entries[len(entries)-1]); ok {
			return addr, true
		}
	}
	return parseAddr(r.RemoteAddr)
}

// parseAddr reads an address with or without a port, in brackets or not.
func parseAddr(v string) (netip.Addr, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
	addr, err := netip.ParseAddr(v)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}

// routable is an address that could belong to a reader somewhere: not
// loopback, private, link-local, multicast or unspecified.
func routable(a netip.Addr) bool {
	return a.IsValid() && !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() &&
		!a.IsLinkLocalMulticast() && !a.IsMulticast() && !a.IsUnspecified() && !a.IsInterfaceLocalMulticast()
}
