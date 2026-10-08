package daily

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// Player is someone playing: who the boards say they are.
type Player struct {
	ID   int64
	Name string
	Hue  int
}

// A player is their cookie: there are no accounts. The cookie is 128
// random bits, base64url without padding, and it is the player's only
// credential, so the database keeps its hash (TokenHash) and never the
// cookie itself.

// NewToken is a new player's cookie.
func NewToken() string {
	b := make([]byte, 16)
	// crypto/rand.Read never fails on the platforms Go supports, and
	// says so; a failure would be a broken machine, not a request.
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// TokenOK is whether a cookie has the shape NewToken gives one. Anything
// else is not looked up at all.
func TokenOK(token string) bool {
	b, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(b) == 16
}

// TokenHash is what a cookie is kept as.
func TokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
