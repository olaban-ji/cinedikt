package daily

import "hash/fnv"

// Rand is a small seeded generator, mulberry32, the one the prototypes
// draw with. A day's order of candidates is seeded from its day, so
// picking the same day again from the same catalog picks the same movie:
// a pick that is retried after a failure, or run by a second process,
// does not choose afresh.
//
// It is deliberately not math/rand: Go keeps no promise that a seeded
// shuffle there gives the same order in the next release, and a puzzle
// picked ahead must not change under a deploy.
type Rand struct{ s uint32 }

// Seeded is a generator seeded from a string, by its 32-bit FNV-1a hash.
func Seeded(seed string) *Rand {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return &Rand{s: h.Sum32()}
}

// Uint32 is the next number.
func (r *Rand) Uint32() uint32 {
	r.s += 0x6d2b79f5
	t := (r.s ^ r.s>>15) * (r.s | 1)
	t = (t + (t^t>>7)*(t|61)) ^ t
	return t ^ t>>14
}

// IntN is a number in [0, n), drawn the way the prototype draws one:
// the next number as a fraction of 2³², scaled.
func (r *Rand) IntN(n int) int {
	return int(uint64(r.Uint32()) * uint64(n) >> 32)
}

// Shuffle puts xs in an order drawn from r: Fisher–Yates, from the end.
func Shuffle[T any](r *Rand, xs []T) {
	for i := len(xs) - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		xs[i], xs[j] = xs[j], xs[i]
	}
}
