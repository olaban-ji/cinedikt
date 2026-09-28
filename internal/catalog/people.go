package catalog

// The photos of the people on a map.
//
// Kept in meta.people, as the path of each photo on TMDb's image host.
// PersonPhotoJob does every lookup; a map and GET /api/people/photos only
// read what it has kept, so no reader ever waits on TMDb. The people on a
// map a reader opens that the job has no answer for are marked wanted,
// which wakes the job and puts them at the front of its queue. Behind
// them the job sweeps everyone a map can show, best known first and at a
// pace of its own, and then asks again about the answers that have come
// due.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/time/rate"

	"cinedikt/internal/notify"
	"cinedikt/internal/tmdb"
)

// PeopleSweepMinVotes is how well known a person's best known film a map
// can show has to be for the sweep to reach them. Zero is everyone a map
// can show, and a person below a higher floor is still looked up the
// moment a reader opens a map they are on.
const PeopleSweepMinVotes = 0

// PeopleSweepPerSecond is the pace of the sweep and the re-asks, on top of
// the process's TMDb limiter. The sweep is over a million people long,
// and at the process's whole budget it would keep that budget spent for
// most of a day: a reader's trailer, or the photos of a map somebody has
// just opened, would queue behind it. A quarter of the default budget leaves the rest
// free for them. The people a reader is waiting on skip it.
const PeopleSweepPerSecond = 5

// PeopleBatch is how many people the job claims per round. A reader's
// mark is heard between people, not between rounds, so this is not how
// long a reader waits.
const PeopleBatch = 50

// PeopleRest is how long the job waits after catching up, and how often
// it looks for people to queue. A reader opening a map with people it
// has no answer for, or a new generation, wakes it sooner.
const PeopleRest = 30 * time.Minute

// PhotoRow is what the endpoint knows of one person's photo.
type PhotoRow struct {
	// Person is whether the catalog holds the person at all.
	Person bool
	// Asked is whether meta.people has an answer young enough to show;
	// Photo is that answer as a full address, empty for "no photo".
	Asked   bool
	Photo   string
	AskedAt time.Time
}

// Due reports whether an answer is old enough that TMDb's terms want it
// asked again. It is still shown while it waits, and the person is
// marked so the job asks again. photoDue says the same thing in SQL, and
// the two must agree.
func (r PhotoRow) Due(now time.Time) bool {
	return r.Asked && now.Sub(r.AskedAt) > TMDbRefreshAfter
}

// photoServed is the answers a read shows, as a condition on the answer
// ph: those younger than days. The backstop deletes anything older, but
// it runs every twenty minutes, and a read shows nothing it has yet to
// reach.
func photoServed(days string) string {
	return `ph.asked_at > now() - make_interval(days => ` + days + `)`
}

// photoDue is PhotoRow.Due in SQL, for the answer ph: asked more than
// days ago.
func photoDue(days string) string {
	return `ph.asked_at < now() - make_interval(days => ` + days + `)`
}

// photoURL is the address a stored path is shown at.
func photoURL(path *string) string {
	if path == nil {
		return ""
	}
	return tmdb.PosterURL(*path, tmdb.ProfileWidth)
}

// PeoplePhotos reads what is stored about each person's photo, and
// whether the catalog holds them: one query, a primary-key lookup a
// person. A person missing from the result was not asked about.
func (s *Store) PeoplePhotos(ctx context.Context, nconsts []string) (map[string]PhotoRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT q.nconst, n.nconst IS NOT NULL, ph.nconst IS NOT NULL, ph.profile_path, ph.asked_at
		FROM unnest($1::text[]) AS q(nconst)
		LEFT JOIN `+Live+`.names n ON n.nconst = q.nconst
		LEFT JOIN meta.people ph ON ph.nconst = q.nconst AND `+photoServed("$2"), nconsts, tmdbForgetDays)
	if err != nil {
		return nil, fmt.Errorf("catalog: people's photos: %w", err)
	}
	defer rows.Close()
	out := make(map[string]PhotoRow, len(nconsts))
	for rows.Next() {
		var id string
		var row PhotoRow
		var path *string
		var asked *time.Time
		if err := rows.Scan(&id, &row.Person, &row.Asked, &path, &asked); err != nil {
			return nil, fmt.Errorf("catalog: scan a person's photo: %w", err)
		}
		row.Photo = photoURL(path)
		if asked != nil {
			row.AskedAt = *asked
		}
		out[id] = row
	}
	return out, rows.Err()
}

// keepPersonPhoto stores TMDb's answer for a person: the path of their
// photo, or none. Either way the person leaves the job's queue, wanted
// or not.
func (s *Store) keepPersonPhoto(ctx context.Context, nconst string, got tmdb.FoundPerson) error {
	var id *int
	if got.ID > 0 {
		id = &got.ID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.people (nconst, tmdb_id, profile_path, asked_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (nconst) DO UPDATE
		SET tmdb_id      = EXCLUDED.tmdb_id,
		    profile_path = EXCLUDED.profile_path,
		    asked_at     = EXCLUDED.asked_at`, nconst, id, textOrNull(got.Profile))
	if err != nil {
		return fmt.Errorf("catalog: keep photo %s: %w", nconst, err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.people_queue WHERE nconst = $1`, nconst); err != nil {
		return fmt.Errorf("catalog: people queue %s: %w", nconst, err)
	}
	return nil
}

// PersonFinder is what the people job needs from TMDb. An interface so a
// test never reaches the network.
type PersonFinder interface {
	FindPersonByIMDb(ctx context.Context, nconst string) (tmdb.FoundPerson, error)
}

// PersonPhotoJob looks up the photo of everyone a map can show.
type PersonPhotoJob struct {
	Store *Store
	// TMDb is the runner's client, and with it the process's one
	// limiter.
	TMDb   PersonFinder
	Logger *slog.Logger
	// MinVotes is the sweep's floor, on the votes of a person's best
	// known film a map can show. A person on a map a reader has opened
	// is looked up whatever their votes.
	MinVotes int
	// Sweep is the pace of the sweep and the re-asks, waited on before
	// the process's limiter, so a sweep over a million people long leaves
	// room in that budget for what readers are waiting on. The people on
	// a map a reader has opened do not wait on it. Nil sets no pace of
	// its own.
	Sweep *rate.Limiter
	// Batch is how many are claimed per round; zero takes PeopleBatch.
	Batch int
	// Wanted is the wake a reader's mark sends. A pass that hears it,
	// between people or while waiting on Sweep, goes back for the wanted
	// people, so a reader waits on the lookup in hand rather than on the
	// rest of a round of the sweep. Nil is never interrupted.
	Wanted <-chan struct{}
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink

	// refilled is when the queue was last refilled from the catalog.
	// That reads every credit in it, and every map a reader opens can
	// wake the job, so only the rest interval and a new generation bring
	// a refill: the marks queue their own people. The zero value refills
	// on the next pass.
	refilled time.Time
}

// personToAsk is one person the job is about to ask about.
type personToAsk struct {
	nconst string
	// wantedAt is when a reader's map last marked the person, for a
	// person taken as wanted; zero for the sweep and the re-asks.
	wantedAt time.Time
}

// Run asks what it can before ctx is done, in three kinds, each only
// once the ones before it have nothing left to give: the people on maps
// readers have opened, newest mark first; everyone never asked, best
// known first; and answers old enough that TMDb's terms want them asked
// again. The wanted people are looked at again before every round, and
// whenever a reader's mark arrives.
func (j *PersonPhotoJob) Run(ctx context.Context) error {
	batch := j.Batch
	if batch <= 0 {
		batch = PeopleBatch
	}
	if time.Since(j.refilled) >= PeopleRest {
		if err := j.Store.refillPeopleQueue(ctx, j.MinVotes); err != nil {
			if stopping(err) {
				return nil
			}
			return err
		}
		j.refilled = time.Now()
	}
	outstanding, err := j.Store.peopleOutstanding(ctx, j.MinVotes)
	if stopping(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if outstanding == 0 {
		return nil
	}
	track := newProgress(j.Logger, "finding people's photos", outstanding)
	track.watch(j.Notify, notify.Event{Job: notify.JobPeople})
	run := pass{sink: j.Notify, job: notify.JobPeople}
	var found, none, failed int64
	var last error
	// A failed lookup stores nothing, so it stays at the head of its
	// kind; this pass must not ask it again. A kind whose head is nothing
	// but this pass's failures gives way to the next, and the pass ends
	// once every kind's head is, so a TMDb that is down costs a round of
	// failed lookups per kind rather than the whole queue. A try is kept
	// with the mark it served, zero when it served none, and holds back
	// only that mark: a person who failed in the sweep is asked about
	// again as soon as a reader's map marks them, and one who failed
	// while wanted as soon as a map renews their mark.
	tried := make(map[string]time.Time)
	for {
		if ctx.Err() != nil {
			j.Logger.Info("people's photos paused", "found", found, "none", none, "failed", failed)
			return nil
		}
		people, err := j.Store.peopleWanted(ctx, batch, j.MinVotes, tried)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(people) == 0 {
			if found+none+failed > 0 {
				track.done(found + none + failed)
				j.Logger.Info("people's photos caught up", "found", found, "none", none, "failed", failed)
			}
			if failed >= failedLookups && found+none == 0 {
				return &LookupsFailedError{Provider: "TMDb", Count: failed, Last: last}
			}
			run.finish(found, none, failed)
			return nil
		}
		run.start(outstanding)
		for _, person := range people {
			if ctx.Err() != nil {
				return nil
			}
			if j.heard() {
				// A reader has opened a map with people who have no
				// answer. The rest of this round waits behind them.
				break
			}
			if person.wantedAt.IsZero() {
				heard, err := j.sweepTurn(ctx)
				if stopping(err) {
					return nil
				}
				if err != nil {
					return err
				}
				if heard {
					break
				}
			}
			tried[person.nconst] = person.wantedAt
			got, err := j.TMDb.FindPersonByIMDb(ctx, person.nconst)
			if errors.Is(err, tmdb.ErrNotFound) {
				got, err = tmdb.FoundPerson{}, nil
			}
			switch {
			case stopping(err):
				return nil
			case refused(err):
				return &KeyError{Provider: "TMDb", Err: err}
			case err != nil:
				failed++
				last = err
				j.Logger.Warn("photo lookup failed", "nconst", person.nconst, "err", err)
				track.step(found + none + failed)
				continue
			}
			if err := j.Store.keepPersonPhoto(ctx, person.nconst, got); err != nil {
				if stopping(err) {
					return nil
				}
				return err
			}
			if got.Profile != "" {
				found++
			} else {
				none++
			}
			track.step(found + none + failed)
		}
	}
}

// heard reports whether a reader's mark has arrived, taking the wake if
// so: the pass that hears it is the one that serves it.
func (j *PersonPhotoJob) heard() bool {
	select {
	case <-j.Wanted:
		return true
	default:
		return false
	}
}

// sweepTurn waits for the sweep's own pace. A reader's mark ends the wait
// at once, and heard says that is why it returned: however slow the
// sweep is set, nobody waiting on a photo waits on it.
func (j *PersonPhotoJob) sweepTurn(ctx context.Context) (heard bool, err error) {
	if j.Sweep == nil {
		return false, nil
	}
	turn := j.Sweep.Reserve()
	if !turn.OK() {
		return false, fmt.Errorf("catalog: the people sweep's pace allows no lookups (%v a second, burst %d)", j.Sweep.Limit(), j.Sweep.Burst())
	}
	delay := turn.Delay()
	if delay <= 0 {
		return false, nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		turn.Cancel()
		return false, ctx.Err()
	case <-j.Wanted:
		// The turn goes back, so the sweep does not lose it to the
		// reader it gave way to.
		turn.Cancel()
		return true, nil
	case <-timer.C:
		return false, nil
	}
}

// refillPeopleQueue clears what has been answered since it was queued, or
// has left the catalog, and queues everyone with no answer whose best
// known film a map can show is at or above the floor. votes is that
// film's, so the sweep takes the people readers are likeliest to meet
// first. Only a person new to the queue, or whose votes have moved, is
// written. A wanted person whose answer came due before the want is
// kept: that is the answer they are waiting to have replaced.
func (s *Store) refillPeopleQueue(ctx context.Context, minVotes int) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM meta.people_queue q
		WHERE EXISTS (
		      SELECT 1 FROM meta.people ph
		      WHERE ph.nconst = q.nconst
		        AND (q.wanted_at IS NULL OR ph.asked_at >= q.wanted_at))
		   OR NOT EXISTS (SELECT 1 FROM `+Live+`.names n WHERE n.nconst = q.nconst)`); err != nil {
		return fmt.Errorf("catalog: clear answered people queue: %w", err)
	}
	err := s.fillQueue(ctx, `
		INSERT INTO meta.people_queue (nconst, votes)
		SELECT best.nconst, best.votes
		FROM (
		    SELECT who.nconst, max(coalesce(r.num_votes, 0)) AS votes
		    FROM (
		        SELECT pr.tconst, pr.nconst
		        FROM `+Live+`.principals pr
		        WHERE pr.category IN ('actor', 'actress', 'director')
		        UNION ALL
		        SELECT d.tconst, d.nconst
		        FROM `+Live+`.directors d
		    ) who
		    JOIN `+Live+`.titles t ON t.tconst = who.tconst
		    LEFT JOIN `+Live+`.ratings r ON r.tconst = who.tconst
		    LEFT JOIN meta.posters p ON p.tconst = who.tconst
		    WHERE `+gridFilm+`
		    GROUP BY who.nconst
		) best
		WHERE best.votes >= $1
		  AND EXISTS (SELECT 1 FROM `+Live+`.names n WHERE n.nconst = best.nconst)
		  AND NOT EXISTS (SELECT 1 FROM meta.people ph WHERE ph.nconst = best.nconst)
		  AND NOT EXISTS (
		      SELECT 1 FROM meta.people_queue q
		      WHERE q.nconst = best.nconst AND q.votes = best.votes)
		ON CONFLICT (nconst) DO UPDATE SET votes = EXCLUDED.votes`, minVotes)
	if err != nil {
		return fmt.Errorf("catalog: fill people queue: %w", err)
	}
	return nil
}

// markPeopleWanted puts the people on a map a reader opened at the front
// of the people job's queue, whatever their votes, and says how many it
// wanted afresh.
//
// Only a person who still needs asking is marked: one with no answer, or
// one whose answer has come due. By the time a mark is written the job
// may already have answered them, and marking that would ask again for
// an answer a few seconds old.
//
// A person who is already wanted keeps a mark less than a minute old: a
// map opened again straight away, or the page asking again while the
// photos are pending, changes nothing, so it wakes nothing either. An
// older mark is renewed. A lookup that failed is held back for the rest
// of the pass unless the person is marked after it, and that pass can be
// the sweep's, days long; renewing the mark is what brings them back to
// the front when a reader opens their map again.
func (s *Store) markPeopleWanted(ctx context.Context, nconsts []string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO meta.people_queue AS q (nconst, wanted_at)
		SELECT n.nconst, now()
		FROM `+Live+`.names n
		LEFT JOIN meta.people ph ON ph.nconst = n.nconst
		WHERE n.nconst = ANY($1)
		  AND (ph.nconst IS NULL OR `+photoDue("$2")+`)
		ON CONFLICT (nconst) DO UPDATE SET wanted_at = EXCLUDED.wanted_at
		WHERE q.wanted_at IS NULL
		   OR q.wanted_at < EXCLUDED.wanted_at - interval '1 minute'`, nconsts, tmdbRefreshDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// The three kinds of work, as the FROM and WHERE that peopleWanted and
// peopleOutstanding share. Each names the person n, and takes the
// placeholders for its settings, so every query names only the
// parameters it is given.

// personWantedFrom is the people on maps readers have opened who are
// still waiting: never asked, or asked before the reader wanted them,
// which the mark allows only for an answer that has come due.
const personWantedFrom = `
		FROM meta.people_queue q
		JOIN ` + Live + `.names n ON n.nconst = q.nconst
		WHERE q.wanted_at IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM meta.people ph
		      WHERE ph.nconst = q.nconst AND ph.asked_at >= q.wanted_at)`

// personQueued is the sweep: the people never asked, as far as the queue
// holds them at the floor.
func personQueued(votes string) string {
	return `
		FROM meta.people_queue q
		JOIN ` + Live + `.names n ON n.nconst = q.nconst
		WHERE q.votes >= ` + votes + `
		  AND NOT EXISTS (SELECT 1 FROM meta.people ph WHERE ph.nconst = q.nconst)`
}

// stalePeople is any answer old enough that TMDb's terms want it asked
// again. One whose person has left the catalog, or whose re-ask keeps
// failing, is deleted by forgetDueTMDbData instead.
func stalePeople(days string) string {
	return `
		FROM meta.people ph
		JOIN ` + Live + `.names n ON n.nconst = ph.nconst
		WHERE ` + photoDue(days)
}

// peopleOutstanding counts the three kinds of work, once, at the start of
// a pass. A person who is in two kinds is counted twice; the figure is
// for a progress line, not for bookkeeping.
func (s *Store) peopleOutstanding(ctx context.Context, minVotes int) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) `+personWantedFrom+`)
		     + (SELECT count(*) `+personQueued("$1")+`)
		     + (SELECT count(*) `+stalePeople("$2")+`)`,
		minVotes, tmdbRefreshDays).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count people wanting a photo: %w", err)
	}
	return n, nil
}

// peopleWanted is the next round: the head of the first of the three
// kinds that has anybody this pass has not already tried. A lookup that
// failed stores nothing, so it is still at the head of its kind, and is
// left out here until a reader marks the person after the mark that try
// served; tried holds that mark, zero for one that served none.
func (s *Store) peopleWanted(ctx context.Context, limit, minVotes int, tried map[string]time.Time) ([]personToAsk, error) {
	for _, kind := range []struct {
		from  string
		args  []any
		want  string
		order string
	}{
		{personWantedFrom, nil, "q.wanted_at", "q.wanted_at DESC, q.nconst"},
		{personQueued("$2"), []any{minVotes}, "NULL::timestamptz", "q.votes DESC, q.nconst"},
		{stalePeople("$2"), []any{tmdbRefreshDays}, "NULL::timestamptz", "ph.asked_at, ph.nconst"},
	} {
		rows, err := s.pool.Query(ctx, `
			SELECT n.nconst, `+kind.want+` `+kind.from+`
			ORDER BY `+kind.order+`
			LIMIT $1`, append([]any{limit}, kind.args...)...)
		if err != nil {
			return nil, fmt.Errorf("catalog: people wanting a photo: %w", err)
		}
		var fresh []personToAsk
		for rows.Next() {
			var p personToAsk
			var wantedAt *time.Time
			if err := rows.Scan(&p.nconst, &wantedAt); err != nil {
				rows.Close()
				return nil, err
			}
			if wantedAt != nil {
				p.wantedAt = *wantedAt
			}
			if at, ok := tried[p.nconst]; ok && !p.wantedAt.After(at) {
				continue
			}
			fresh = append(fresh, p)
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

// fillPeople keeps the people job running for as long as the process
// does. It shares the runner's TMDb client, and with it the process's one
// limiter, so its requests and the other jobs' add up to one budget.
func fillPeople(ctx context.Context, job *PersonPhotoJob, logger *slog.Logger, wakes *Wakes) {
	waited := false
	for {
		ready, err := job.Store.LiveReady(ctx)
		wait := PeopleRest
		switch {
		case err != nil || !ready:
			if !waited {
				logger.Info("people's photos waiting for a catalog")
				waited = true
			}
			wait = PosterWaitForCatalog
		default:
			waited = false
			err := job.Run(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Warn("people's photos", "err", err)
			}
			reportRun(ctx, job.Notify, notify.JobPeople, err, time.Now().Add(wait))
		}
		// A reader opening a map whose people have no answer is waiting
		// on this job, and should not wait out its rest to be heard. A
		// new generation brings people to queue, so the next pass
		// refills; the rest interval covers a wake sent while nobody was
		// listening, and brings a refill of its own.
		woke, published := waitForPeople(ctx, wakes, wait)
		if !woke {
			return
		}
		if published {
			job.refilled = time.Time{}
		}
	}
}

// waitForPeople is waitFor for the people job, which has two wakes: a
// reader's mark, and a new generation. published says it was the second.
func waitForPeople(ctx context.Context, wakes *Wakes, backstop time.Duration) (woke, published bool) {
	timer := time.NewTimer(backstop)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, false
	case <-wakes.People:
		return true, true
	case <-wakes.PeopleWanted:
		return true, false
	case <-timer.C:
		return true, false
	}
}
