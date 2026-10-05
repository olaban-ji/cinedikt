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
// marked the same way, for the synopsis job, a film opened before its
// trailer has been looked up, for the trailer job, and the people on a
// map opened before their photos have been, for the people job.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
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

// WantTrailer records that a reader opened a film whose trailer has not
// been looked up, or whose answer has come due, so the trailer job asks
// about it before anything else. The same rules as wantPoster: the
// reader is told the answer is on its way, and is not kept waiting on
// the note that makes it so.
func (s *Store) WantTrailer(tconst string) { s.want(s.trWants, []string{tconst}) }

// WantPeople records that a reader opened a map whose people the people
// job has no photo answer for, or one that has come due, so the job asks
// about them before anybody the sweep has still to reach. The same rules
// as wantPoster.
func (s *Store) WantPeople(nconsts ...string) { s.want(s.peWants, nconsts) }

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
	trailers := make([]string, 0, wantBatch)
	people := make([]string, 0, wantBatch)
	timer := time.NewTimer(wantFlush)
	defer timer.Stop()
	flush := func() {
		if len(posters) == 0 && len(synopses) == 0 && len(trailers) == 0 && len(people) == 0 {
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
		if len(trailers) > 0 {
			if err := s.markTrailersWanted(ctx, trailers); err == nil {
				s.notify(ctx, NotifyTrailerWanted)
			}
		}
		// Every map a reader opens marks the people on it who have no
		// answer yet, and opening it again marks the same people. Only a
		// mark that wanted somebody new, or renewed a mark at least a
		// minute old, wakes the job: a signal for marks that changed
		// nothing would start a pass that asks again about the people it
		// just failed on, as often as a reader reopens the map.
		if len(people) > 0 {
			if n, err := s.markPeopleWanted(ctx, people); err == nil && n > 0 {
				s.notify(ctx, NotifyPersonWanted)
			}
		}
		cancel()
		posters, synopses, trailers, people = posters[:0], synopses[:0], trailers[:0], people[:0]
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
		case id := <-s.trWants:
			trailers = append(trailers, id)
			if len(trailers) >= wantBatch {
				flush()
			}
		case id := <-s.peWants:
			people = append(people, id)
			if len(people) >= wantBatch {
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
				case id := <-s.trWants:
					trailers = append(trailers, id)
					continue
				case id := <-s.peWants:
					people = append(people, id)
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

// heard reports whether a reader's mark has arrived on wanted, taking
// the wake if so: the pass that hears it is the one that serves it.
func heard(wanted <-chan struct{}) bool {
	select {
	case <-wanted:
		return true
	default:
		return false
	}
}

// wantKind is one kind of work a job that readers wait on takes its
// rounds from: the FROM and WHERE it shares with the job's count, the
// arguments its placeholders after $1 take, the reader's mark (a NULL
// timestamptz for a kind no reader marked), and the order it is worked
// in.
type wantKind struct {
	from  string
	args  []any
	want  string
	order string
}

// marked is a round's item that carries the id it was tried under and
// the reader's mark it was taken for, zero for one no reader marked.
type marked interface {
	mark() (id string, wantedAt time.Time)
}

// nextByKind is a job's next round: the head of the first of kinds that
// has anything this pass has not already tried. A lookup that failed
// stores nothing, so it is still at the head of its kind, and is left
// out here until a reader marks it after the mark that try served; tried
// holds that mark, zero for one that served none. columns is what each
// row is read from, before the mark, which scan reads last.
func nextByKind[T marked](ctx context.Context, s *Store, limit int, columns string, kinds []wantKind,
	tried map[string]time.Time, scan func(pgx.Rows) (T, error)) ([]T, error) {
	for _, kind := range kinds {
		rows, err := s.pool.Query(ctx, `
			SELECT `+columns+`, `+kind.want+` `+kind.from+`
			ORDER BY `+kind.order+`
			LIMIT $1`, append([]any{limit}, kind.args...)...)
		if err != nil {
			return nil, err
		}
		var fresh []T
		for rows.Next() {
			item, err := scan(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			id, wantedAt := item.mark()
			if at, ok := tried[id]; ok && !wantedAt.After(at) {
				continue
			}
			fresh = append(fresh, item)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(fresh) > 0 {
			return fresh, nil
		}
	}
	return nil, nil
}
