# The catalog

The map is read from a local copy of the [IMDb non-commercial
datasets](https://developer.imdb.com/non-commercial-datasets/). Those
five files are the whole source. A reader's request never crawls, never
calls TMDb to build a map, and never writes the tables it reads — which
is what stops a first search from returning a half-built map.

## Running it

Postgres is the only dependency. `DATABASE_URL` points both processes at
it; the API only reads, the importer only writes.

```
DATABASE_URL=postgres://user:pass@host:5432/cinedikt go run ./cmd/importer -once
DATABASE_URL=postgres://user:pass@host:5432/cinedikt go run ./cmd/api
```

| Flag | What it does |
| --- | --- |
| `-once` | one attempt, then exit. Without it the importer polls hourly |
| `-posters-only` | fill posters, release dates and synopses against the live catalog, without importing |
| `-keep` | leave the downloaded files on disk, for a development re-run |
| `-dir` | where to put them |

The first import takes about five and a half minutes: roughly two of
those are the 1.35 GB download, and the rest is reading twelve million
rows and building the indexes. It prints where it has got to every few
seconds, in four numbered steps.

The posters are a separate job and a separate wait: about 27 minutes for
all 748,544 non-adult titles, measured at 466 lookups a second. Nothing
waits on it — a poster appears the moment it lands, and the films anyone
would actually search are done in the first minute, because the job
takes the most voted first.

## Tests

The Postgres tests need a database of their own — they publish
fixtures over `catalog` and empty `meta.posters`, so pointing them at a
working catalog destroys it.

```
createdb cinedikt_test
CATALOG_TEST_URL=postgres://user:pass@host:5432/cinedikt_test go test ./...
```

Without that variable they skip, and the rest of the suite still runs.

## What happens each hour

A generation is the five `Last-Modified` stamps, read with `HEAD`.
Nothing runs until every one of them has moved past what was last
published: one file arriving ahead of the others is half a generation,
and half a generation on top of yesterday's rest is a catalog whose
credits and titles disagree. In practice IMDb publishes all five within
about a minute of each other, once a day.

When they have all moved, the files are streamed to disk and the stamps
are read again. If anything moved while it was being fetched, what is on
disk is a mixture and the attempt is thrown away.

The load goes into `catalog_next`. Readers only ever touch `catalog`, so
the whole import is invisible to them. Titles are read first, and only
movies are kept — nine of every ten rows in `title.basics` is a
television episode. Those ids are the allow-list every other file is
filtered against, and `name.basics` is read last so a person is stored
only if a kept credit named them.

Publish is one transaction: `catalog` becomes `catalog_old`,
`catalog_next` becomes `catalog`, and the generation is recorded. The
old schema is **not** dropped there. It is left for the next run, so a
reader holding a plan against it finishes against data that still
exists. `lock_timeout` bounds the swap: failing fast leaves the previous
catalog serving, which is the right way to lose.

## Posters and release dates

The dump has no pictures and no month or day. One OMDb lookup per title
stores the poster's address, the full date and the plot; the browser
loads the image from Amazon, and the key never leaves the server.

That job is not part of an import. There are about 757,000 movies, so a
full first pass takes hours at any polite rate and the catalog would
otherwise always be a generation behind its own posters. It runs on its
own, takes the most voted films first — the few thousand anyone actually
searches get their posters in the first minutes — and picks up where it
left off. `meta` is never renamed by the daily swap, so what it learns
survives every future generation.

A lookup that came back empty is still an answer, and is not asked
again. Only a lookup that *failed* is retried, and not within the same
run. OMDb's answers are not always valid JSON (a stray backslash, a raw
tab in a plot), so one that does not decode is repaired and read again.
One that still cannot be read is an answer too: OMDb sends the same
bytes every time, so it is stored as OMDb having no picture and no
plot, and the TMDb stand-in takes the title up.

## Synopses and trailers

The poster pass asks OMDb for the full plot and keeps it with every
answer, in `meta.synopses`. The synopsis job asks for the titles the
poster pass answered before it kept plots (`omdb_at` is null), through
the pass's own client, so the two share one rate limit and stop
together on OMDb's daily limit. A title the pass has not answered yet
is left to the pass. It takes the titles a reader has been shown first,
then the films at or above `SYNOPSIS_SWEEP_MIN_VOTES` (default 0, so
all of them), most voted first. It reads the queue again before every
batch, so a film a reader has just been shown never waits on the rest
of the sweep, and a spent daily limit pauses the sweep where it stands
until the limit resets. TMDb's overview,
which arrives free on the answers the TMDb jobs already save, fills in
only where OMDb has no plot, never stops OMDb being asked, and is asked
of OMDb again after 150 days.

The trailer job is the only thing that looks trailers up, through
`internal/trailer`. `GET /api/trailers/{tconst}` only reads
`meta.trailers`: a film with no answer yet is marked wanted
(`meta.trailer_queue.wanted_at`, through the same buffer as the other
marks), which sends `trailer_wanted` and wakes the job, and the reader is
answered `{"key":null,"pending":true}`; a stored answer that has come
due is served as it is and marked the same way. The job asks, in order,
about the films readers have marked, newest first, whatever their votes;
then every film the id matcher would match, at or above
`TRAILER_SWEEP_MIN_VOTES` (default 0, so all of them), most voted first;
then a recent film's week-old "none"; then anything older than 150 days
for a film TMDb has a movie for. It looks at the marks before every
round, and a mark that arrives in the middle of one stops the round at
the next title. A film with a TMDb id is asked for its clips; one TMDb
has no movie for is "none" at no cost; one nothing has matched yet is
matched by the job itself, once, and the id and overview are kept the
way the id matcher keeps them.

## People's photos

The people job is the only thing that looks a person's photo up, with
TMDb's find by IMDb name id, and it keeps only the photo's path, in
`meta.people`; a null path is "no photo", an answer like any other. A
map and `GET /api/people/photos` only read it. `Grid` reads each
chip's answer with the chip row (`peopleOn`, a primary-key lookup a
person), shows the photo at w185 while the answer is younger than 175
days, and marks the people with no answer, or one older than 150 days
(`meta.people_queue.wanted_at`, through the same buffer as the other
marks). `Films` reads the same chip row for every batch of cards and
marks nobody. A mark is re-checked when it is written, a person
already wanted keeps a mark less than a minute old, and
`person_wanted` is sent only when a mark wanted somebody new or
renewed an older one, so a map opened again straight away wakes
nothing, and a person whose lookup failed is asked again the next
time their map is opened rather than once the pass is over. The
endpoint marks the same way and answers the unanswered as `pending`.

The job asks, in order, about the people readers' maps have marked,
newest first, whatever their votes; then everyone billed as cast or
credited as director on a movie a map can show, whose best known such
movie has at least `PEOPLE_SWEEP_MIN_VOTES` votes (default 0, so everyone), most
voted first, the votes snapshotted when the queue is refilled (at most
every rest interval, or after a publish); then anything older than 150
days. The sweep and the re-asks wait on a pace of their own,
`PEOPLE_SWEEP_RATE` (default 5 a second), before the process's TMDb
limiter, so the rest of that budget stays free for readers; the marked
people skip it, and a mark that arrives while the sweep waits is served
at once.

## TMDb's six months

TMDb's terms ask that anything cached from it be refreshed within six
months. Whatever keeps something of TMDb's asks again once it is 150
days old, after everything else its job has to do: the synopsis job
asks OMDb about a film showing TMDb's overview, the trailer job asks
for the trailer again, the id matcher asks for a match in `meta.tmdb`
again, for a film search can still offer (renewed in place, so search
and the trailer job never find it missing), the people job asks for a
person's photo again, and the poster fallback asks again about a backup
poster, or about TMDb's "nothing" for a title that still has no picture
(`meta.posters.tmdb_at`). What is still there at 175 days is taken away
by a backstop that runs whatever credentials the runner has: trailers,
overviews, id matches and people's photos are deleted, and a backup
poster is set back to none, with its colour and any release
date TMDb filled in where OMDb had none (`released_tmdb`), and its
stamp is cleared, so the fallback can ask again. An address or date
from OMDb, and its status, are never touched.

## Where to watch

`WhereToWatch` answers `GET /api/where-to-watch/{tconst}` for a
reader's country. It keeps the Streaming Availability API's answers,
shaped by `internal/streaming`, in `meta.where_to_watch`, one row per
movie and country, empty answers included and failures never. A pair
nobody has asked about is asked in the request, once however many
readers arrive together (a singleflight on `tconst/country`, detached
from the reader who started it, with five seconds), and kept. A kept
answer is served as it stands, and kept until the changes feed says the
movie changed in that country, one of its options leaves, or it is
`WatchFresh` (30 days) old; then it is still served, and a
`where_to_watch_refresh` job is queued behind it. A movie the catalog
does not hold is `ErrNotFound`, and never asked about. The API
failing to answer is `ErrUpstream`, which the route answers 502; any
other error is the store's, answered 500. Only the movies readers open
are ever asked about: there is no sweep.

Keeping an answer and scheduling its next ask are one transaction. When
an answer has options with an `expiresOn` still to come, a refresh is
inserted, scheduled for the first of them (`expires_at`), with the
answer's row. The job asks again, as of that moment, so an option the
API still lists after its day is left out; keeps the answer; and
schedules the next. A refresh finds nothing to do when an answer was
kept after it fell due, and a refused key cancels it rather than
retrying it with the same key.

`streaming_changes` (`watchchanges.go`) keeps kept answers right from
the API's `/changes` feed. It looks every hour and reads each covered
country with kept answers once its last run is `ChangesEvery` (a day)
old: the `new`, `removed` and `updated` changes to movies, oldest
first, from where that country's last run got to (`meta.streaming_sync`)
until now. A first run starts a day back, or at the country's oldest
kept answer when that is older; no run starts more than the feed's 31
days back, and one cut to that says so. Each change is matched to a
tconst by its show's `imdbId`, and only pairs kept for that country are
touched, each once a run. A show carrying its options for the country
is shaped and written through `keep`, which schedules its next expiry;
one carrying none is refreshed, since the feed saying nothing is not
the movie being on nothing. The three feeds are read a page at a time in
turn, up to `ChangesMaxPages` pages a country (never fewer than one of
each), and a run records the end of its window when every feed got
there, or the earliest any unfinished one reached when the cap stopped
it, so the next carries on. A failed page ends the country's run and
records only what every feed got past, and the country waits a day; a
run that got past nothing is recorded too once it read a page, so a
page that keeps failing does not have the pages before it read again on
every try. One that read no page, or was stopped, records nothing and
is tried again. A refused key cancels the job. Each run logs its pages,
changes, writes and refreshes, and warns when the cap stopped it, when
a change named a show its page did not carry, or when a page said there
were more and gave no cursor.

`meta.streaming_countries` is the API's list of covered countries and
their names. The first reader who needs it fills it; the daily
`streaming_countries` job asks again once it is a week old, and never
replaces it with an empty list. A country not on it is answered as
uncovered without the API being asked.

`meta.geoip` is GeoLite2 Country itself, with MaxMind's
`Last-Modified` for the build. The `geoip_check` job sends a `HEAD`
twice a day and downloads only a different build, which
`internal/geoip` unpacks, opens and verifies before it is kept and
swapped in. Only the queue's leader queues the check, so only one
process downloads; every process loads the kept database at start and
looks at its stamp every ten minutes to pick up a build another one
fetched. The check tells the notifier how it ended: a new build, with
when MaxMind built it, its size and the build it replaced, only from
the check that kept it; the same build found again; or a failure, once
River has given up on that run's tries, or at once for a refused key.

## The queue

Those four jobs run on River (`queue.go`), in the API process, with
its tables in a `river` schema of their own that the daily swap never
names. `OpenQueue` migrates them under an advisory lock (`0x63696e72`),
so two containers starting together take turns, and gives River a pool
of four connections: one is held for River's `LISTEN` for as long as it
runs, which should be neither a reader's nor one a bulk `COPY` holds.
The jobs write through the store they are given.

A refresh is unique by its arguments among unfinished jobs: movie,
country, and the moment it is for, zero for a stale answer's. Readers
finding the same old answer insert it once, and so do two containers
scheduling the same expiry, while an expiry's refresh and a stale one
never stand in for each other. The periodic jobs are inserted by River's
leader alone, on start and on their period, and are unique among
unfinished jobs, so a new leader cannot stack a second check on one
already running, and two processes never read the same changes. Every
job is claimed by one worker. On shutdown River stops fetching and
gives running jobs five seconds before cancelling them, inside a
draining container's ten.

River's logger passes only its warnings and errors, and River reports
a failed job at info and a cancelled one at debug, so each job logs its
own failure (`logJob`): a refused key as an error, since only a person
can fix it, and anything else as a warning. A job cut short by a stop
logs nothing. A job that panics is logged by the queue's error handler.

The tests work the jobs by hand against a queue that is not started,
and two start it to see a refresh and a read of the changes run end to
end. The changes feed is an httptest stand-in, served through the real
client.
