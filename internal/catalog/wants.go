package catalog

// What the reader could not see.
//
// A title with no picture, or one whose address has stopped answering,
// is only worth repairing if somebody actually looks at it. Three
// hundred thousand titles in this catalog have no poster and fewer than
// fifteen hundred of them have so much as a hundred votes; fetching the
// rest from a second service would be paying two hundred times over for
// work nobody reads.
//
// So the map itself is the queue. Every read that draws a card without
// a picture leaves a mark, and the TMDb job works through the marks.
// What a reader has already tried to look at is the best evidence there
// is of what is worth having. A card drawn with no synopsis row is
// marked the same way, for the synopsis job.

import (
	"context"
	"time"
)

// wantQueue is how many marks may be waiting to be written. It is a
// buffer, not a backlog: past this the mark is dropped, because the
// read that made it must not wait on a write it does not need. A title
// anybody looks at twice will be marked again.
const wantQueue = 512

// wantFlush is how long a mark waits for company before it is written.
const wantFlush = 2 * time.Second

// wantBatch is how many are written in one statement.
const wantBatch = 128

// wantPoster records that a title was drawn without a picture.
//
// Never blocks and never fails: it is called from read paths, and a
// reader waiting on a bookkeeping write would be the opposite of the
// point. A full buffer drops the mark.
func (s *Store) wantPoster(ids ...string) { s.want(s.wants, ids) }

// wantSynopsis records that a title was shown with no synopsis row, so
// the synopsis job asks about it before the titles nobody has met. The
// same rules as wantPoster.
func (s *Store) wantSynopsis(ids ...string) { s.want(s.synWants, ids) }

func (s *Store) want(buffer chan string, ids []string) {
	if buffer == nil {
		return
	}
	for _, id := range ids {
		// The buffer is never closed, so a read that is still finishing
		// while the store shuts down drops its marks rather than
		// panicking on a send. Losing a note about a missing picture
		// costs the next reader one more mark.
		select {
		case <-s.stop:
			return
		default:
		}
		select {
		case buffer <- id:
		default:
			return
		}
	}
}

// collectWants writes the marks away in batches until the store closes.
// One statement per batch of each kind, off the read path entirely.
func (s *Store) collectWants() {
	defer close(s.wantsDone)
	posters := make([]string, 0, wantBatch)
	synopses := make([]string, 0, wantBatch)
	timer := time.NewTimer(wantFlush)
	defer timer.Stop()
	flush := func() {
		if len(posters) == 0 && len(synopses) == 0 {
			return
		}
		// Its own deadline: this outlives the request that caused it,
		// and the request's context is long gone.
		ctx, cancel := context.WithTimeout(context.Background(), ReadTimeout)
		// One signal for each batch, not one per mark. The reader pool
		// writes these and the runner's pool does the work, so the news
		// has to travel through the database.
		if len(posters) > 0 {
			if err := s.markWanted(ctx, posters); err == nil {
				s.notify(ctx, NotifyWanted)
			}
		}
		if len(synopses) > 0 {
			if err := s.markSynopsesWanted(ctx, synopses); err == nil {
				s.notify(ctx, NotifySynopsisWanted)
			}
		}
		cancel()
		posters, synopses = posters[:0], synopses[:0]
	}
	for {
		select {
		case id := <-s.wants:
			posters = append(posters, id)
			if len(posters) >= wantBatch {
				flush()
			}
		case id := <-s.synWants:
			synopses = append(synopses, id)
			if len(synopses) >= wantBatch {
				flush()
			}
		case <-s.stop:
			// Take what is already buffered with us: those are marks a
			// reader has made and a shutdown is no reason to lose them.
			for {
				select {
				case id := <-s.wants:
					posters = append(posters, id)
					continue
				case id := <-s.synWants:
					synopses = append(synopses, id)
					continue
				default:
				}
				break
			}
			flush()
			return
		case <-timer.C:
			flush()
			timer.Reset(wantFlush)
		}
	}
}

// markWanted stamps the titles somebody tried to look at.
//
// Only rows that still have nothing to show are stamped: by the time a
// mark is written the picture may have arrived, and stamping that row
// would put a title with a perfectly good poster into the repair queue.
func (s *Store) markWanted(ctx context.Context, ids []string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE meta.posters
		SET wanted_at = now()
		WHERE tconst = ANY($1)
		  AND (status = 'dead' OR poster_url IS NULL OR btrim(poster_url) = '')`, ids)
	return err
}

// MarkPosterDead records that an address has stopped answering.
//
// The cold screen already asks the image host whether a poster is still
// there, and until now it threw the answer away: the verdict lived in a
// map that died with the process, so the same dead addresses were
// probed again on every start and nothing ever repaired them.
//
// `dead` rather than `missing`, because those are different facts.
// Missing is "we never got an answer"; dead is "we got one, and the
// picture is gone". Only the second is worth asking another service
// about.
func (s *Store) MarkPosterDead(ctx context.Context, tconst string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE meta.posters
		SET status = 'dead', wanted_at = now()
		WHERE tconst = $1 AND status <> 'dead'`, tconst)
	if err == nil {
		s.notify(ctx, NotifyWanted)
	}
	return err
}
