# The catalog

The map is read from a local copy of the [IMDb non-commercial
datasets](https://developer.imdb.com/non-commercial-datasets/). Those
five files are the whole source. A reader's request never calls TMDb
to build a map, and never writes the catalog's own tables — which is
what stops a first search from returning a half-built map. What a
request does write, a mark that something is wanted or an answer worth
keeping, goes into `meta`, or onto River's queue.

How to run it, how to point its tests at a database of their own, what
the hourly import does, and how posters and release dates are filled in
are described once, in the [top-level README](../../README.md) (Run,
The catalog, Tests). What follows is how the jobs beside the catalog
are built.

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
`internal/trailer`. `GET /api/trailers/{tconst}` answers from
`meta.trailers` alone: a film with no answer yet is marked wanted
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
map and `GET /api/people/photos` answer from it alone. `Grid` reads each
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

## Cinedikt Daily

What makes a fair answer, how a board is dealt and how a game is
scored are `internal/daily`'s; this package reads the catalog for it
and keeps what it decides. The puzzles are `daily.go`, the players and
games `dailygames.go`. Each reader plays the puzzle for their own
date, which the API works out from the server's clock and the time
zone the page names; all this package keeps of zones is the one each
game was started in.

`DailyJob` keeps a puzzle picked for every day from `DailyBehind`
(one) before UTC today to `DailyAhead` (eight) after it, ten in all:
the zones west of UTC are still on yesterday until noon UTC at UTC−12,
those east of it are on tomorrow from 10:00 UTC at UTC+14, and the
week after that is to spare. It is a Runner loop, "daily puzzles", not
a River job: it needs nothing but the catalog, so it runs wherever the
catalog's jobs do, waits for a published catalog like them, rests
`DailyRest` (an hour), is woken by a publish through its own
`Wakes.Daily`, and reports as `notify.JobDaily`, the Telegram board's
"Daily puzzles" line. A pass asks, for each of those days, whether
`meta.daily_puzzles` has it, and most passes stop there; the one after
midnight UTC picks the new last day, and a publish wakes it in case a
day it could not pick for has an answer in the new catalog. For a
missing day it reads the candidates once a pass (`dailyCandidates`:
the `first_run` pool, rated, with a poster that is `ok` with an
address, and `gridFilm`), orders them with `daily.Order` against what
the days around it used (`dailyRecent`: the answers 90 days either
side, the eras of the six days before and the first genres of the
two), and tries them in turn. Nothing is asked of a candidate's
synopsis: no clue is a line of its text, since any line can be pasted
into a search engine, so a movie OMDb has no plot for is as fair an
answer as any. Each candidate's people and map are read with the map's
own `peopleOn` and `spine`, plus one query for the titles and votes
the spine leaves out (`titlesAndVotes`), and `daily.Build` deals the
board or says why it cannot be one, as a `daily.Unfit`. The puzzle is
inserted with `ON CONFLICT (day) DO NOTHING`, so a second process
picking the same day keeps whichever wrote first. A clash on `no` is
not skipped: it means the numbering has gone wrong and a day would go
without a puzzle for good, so it is an error the pass returns. A day
no candidate fits is an error, the other days are picked all the same,
and the pass returns every such day joined, which reaches Telegram the
way any job's failure does. On the local catalog a pass picking eight
days from about two thousand candidates takes 1.4 seconds, the first
candidate fitting each day. On a new database the first pass runs the
moment the first catalog is published, while the poster pass is still
reaching the candidates' posters, so it can find few or none; a day it
cannot pick waits for the next pass.

Nothing a pass logs or returns names a candidate: the logs and the
Telegram board are read by whoever runs the site, who may want to play
too. Each pick is logged at info with its day, number and cards, and a
pass that picked or failed anything says how many of each, how many
candidates there were and how long it took. A `daily.Unfit` is logged
at debug by the candidate's place in the day's order rather than its
id, since a later catalog could grow it a map fit to be an answer. The
store's errors name the movie they were reading by its `tconst`, so
`firstFair`, the loop that tries the candidates in turn, hands one on
as `unnamed`, which says "a candidate" in its place and still unwraps
to the error it was.

Development's Play again is `RepickDailyPuzzle`, in `dailydev.go`,
the only thing that ever deals a picked day again. The API offers it
only outside production (`WithDailyDev`); nothing here knows which it
is in, so nothing else may call it. It reads the puzzle, the
candidates and `dailyRecent` for its day, adds the answer it is
replacing to the answers left out, though the day's own is among them
already, and orders the candidates with `daily.OrderBy` from a
generator seeded at random rather than from the day: in the day's
order each press would only swap the same two movies back and forth.
The same `firstFair` deals the board, so a day dealt again is held to
every rule the job holds it to. `replaceDailyPuzzle` then writes it
over the old one in one transaction, keeping `no` and `day`, with an
`UPDATE` guarded on the answer it read, so a second reset of the same
puzzle at once fails rather than perhaps dealing back the answer the
first just dealt, and deletes every game of the puzzle, the moves
going with them by cascade: a game is moves made on one board, and
replayed on another it would be nonsense. The players stay. When no
other candidate fits it is `ErrNoOtherAnswer` and nothing changes, and
nothing it returns names the answer it dealt or any candidate.

A puzzle's number is its day's distance from No. 1's day, plus one.
No. 1's day is worked back from the lowest-numbered puzzle
(`firstDailyDay`: `day - (no - 1)`), never read as `min(day)`. On an
empty table it is the first day of the pass's window, UTC yesterday,
whether or not that day can be picked. The first pass runs while
posters are still arriving, and if it misses its first day but keeps
the next, `min(day)` would count from 1 again, onto numbers already
taken; worked back, the missed day keeps No. 1 and a later pass fills
it while it is still in the window
(`TestAMissedFirstDayKeepsItsNumber`). Once any puzzle is kept no day
before No. 1's is ever picked: it would be No. 0, and every number
after it would move. A reader whose date is earlier gets 503
`not-ready` until theirs comes. `DailyPuzzle` and `DailyPuzzleNo` read
one back, or `ErrNotFound`, with its day as its date at midnight UTC
(`daily.Today`), as a day is kept everywhere.

`meta.daily_players` is a cookie's SHA-256, a generated name, unique,
and a hue; `CreateDailyPlayer` and `RenameDailyPlayer` answer
`ErrNameTaken` when the name is somebody's, and the API draws again. A
game is a row in `meta.daily_games`, made at Play with every point and
the zone it was started in, and its moves in `meta.daily_moves`.
`zone` is the IANA name as `zone.String()` writes it. `StartDailyGame`
on a game the player already has returns it as it is, its zone
included, so pressing Play from somewhere else never moves the
midnight it ends at. The column is added by
`ALTER TABLE … ADD COLUMN IF NOT EXISTS`, `NOT NULL DEFAULT 'UTC'`, so
a database that held the table before gains it, and the games already
in it, all played on UTC's day, read as UTC.

The Year clue, which replaced "How it starts", changed a database made
before it the same way. `meta.daily_puzzles` loses `opening`, the
first sentence of a synopsis that clue sold, by
`ALTER TABLE … DROP COLUMN IF EXISTS`; the year needs nothing kept,
since it is the puzzle's own `year`. `meta.daily_moves`' kind check,
which `CREATE TABLE IF NOT EXISTS` leaves as it was, naming `story`,
is dropped and added again naming `year`, once: a `DO` block does it
only while the check does not name `year`, since adding a check reads
every move ever made. `TestMetaBringsTheFirstDailyTablesToTheYear`
applies `meta.sql` twice over fresh tables and twice over the old
ones.

`DailyAct` makes each move in one transaction holding the game's row
(`FOR UPDATE`), in this order: a key already recorded is a retry,
answered with the game as it is, even past midnight, since the move
was made in time; a game whose puzzle's day is not the date in its own
zone at the moment of the move (`Puzzle.On`), never the zone the
request names, is over, and the move is `daily.ErrDay`; a `seq` other
than the number of moves recorded is `daily.ErrStale`, returned with
the game; anything else is replayed, checked by `daily.Apply`,
inserted, and copied onto the game's row (`pts`, `moves`, and at the
end `finished_at`, `won`, `gave_up` and `ms`), which is all the boards
and the streak read. A wrong guess is looked up live, in the same
transaction (`lookGuess`): its title, year, rating and date, and which
of the answer's people it credits in the principals as actor, actress
or director, or in `directors`, the credits that put a movie on a map.
What that says is kept in the move's `detail`, so a game replays
without the catalog, whatever later imports do to the guessed movie.

`DailyLive` reads the posters and photos a game shows, fresh for every
answer: posters from `meta.posters`, photos from `meta.people` only
while younger than the 175 days anything of TMDb's is kept
(`photoServed`). Neither is copied into a puzzle, and nothing of a
synopsis is, OMDb's or TMDb's, so the Daily keeps nothing of TMDb's
past its six months.

`DailyBoard` ranks in SQL and reads only the rows the board shows: the
players who have finished `daily.EarlierGames` earlier puzzles, and
always the reader, ranked by points, then time, then player id, the
top `daily.TopRows` and `daily.Around` either side of the reader. Both
tabs go by puzzle number and puzzle day, never by a clock. Today's is
every finished game of the puzzle, whichever zone it was played in, so
it stays open for the fifty hours or so its date is current somewhere.
The week adds up each player's finished games of the puzzles whose
days fall from the ISO Monday of the puzzle's day (`daily.Monday`) to
that day, with each day's points. Today's two shares count every
finished game, listed or not. `DailyStreak` reads the puzzles up to
the reader's own, newest first, each with whether the player finished
it with points, and stops at the first that breaks the run; a missing
day has no row and breaks nothing. `DailyPlayed` counts the games
started on a puzzle, in every zone, through `daily_games_no`, an index
on `meta.daily_games (no)`: the board's index is partial, finished
games only, and `UNIQUE (player, no)` leads with the player, so
neither can count all of a puzzle's games, which the opening screen's
banner asks for on every visit.

`DailyStanding` is the reader's place on the week's board, which is
all the opening screens show of it: the same SQL as the tab
(`weekSQL`), so the same players in the same order, over the days
`daily.StandingDays` gives, from the Monday through the day before the
puzzle's until the reader has finished it, and through its day after.
It answers the reader's rank and how many players the board ranks, the
reader counted, or nil when they are not on it or have nothing on it,
as on a Monday before playing. Like the tab, it ranks the whole board
to read one row; the API keeps each answer a minute.

`DailyNames` reads what the API names players from, off every movie
that is not adult and has at least `daily.NameVotes` (25,000) votes:
each distinct character credited on one, about fifty thousand, and the
name of each actor, actress and director credited on one, about
thirty-five thousand, which no generated name may be. It is one query,
each row tagged with which half it is, in about a fifth of a second.
The movies are a `NOT MATERIALIZED` CTE, inlined into both halves:
kept as a table, the planner read all of principals for the
characters, about three times slower.

None of the four tables has a key into `catalog`, which is renamed
every night: ids are plain text, so a game played months ago still
replays. A player's games and moves go with them
(`ON DELETE CASCADE`), but `daily_games` refers to `daily_puzzles`
with no `ON DELETE`, so a puzzle somebody has played cannot be deleted
by accident. The tests empty all four (`resetDaily`) and add what a
puzzle needs to the published fixture: ten candidates, one for each
day a full pass keeps, one in each of the opening screen pool's eight
eras and a second in two of them, all sharing their people; sixty
movies through those people, so every candidate's map holds 69 rated
cards, the other nine candidates among them as close relatives; and
one unrated movie that must stay off every board.
