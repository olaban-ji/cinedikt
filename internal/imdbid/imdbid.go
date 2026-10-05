// Package imdbid is the shape of IMDb's ids, checked wherever one comes
// in from outside — a request, another service's answer — before it goes
// into a query, a URL path or a request of this app's own.
package imdbid

import "strings"

// Title is IMDb's title id: "tt" and digits.
func Title(id string) bool { return valid(id, "tt") }

// Name is IMDb's name id: "nm" and digits.
func Name(id string) bool { return valid(id, "nm") }

// valid is IMDb's shape for an id: a two-letter prefix and digits, seven
// of them or more these days, though the length has grown over the years
// and is not assumed beyond a bound no real id comes near.
func valid(id, prefix string) bool {
	if len(id) < 3 || len(id) > 20 || !strings.HasPrefix(id, prefix) {
		return false
	}
	for i := len(prefix); i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}
