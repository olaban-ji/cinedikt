# cinedikt

Start from a movie and see every movie its cast and directors made, arranged by release year and IMDb rating.

The searched film sits on a grid. **Y is the year. X is the rating**, low on the left and high on the right, on a scale that does not change from map to map. There are no edges. Who connects a card to the film you searched is said on the card and in the sheet that opens from it.

```
IMDb datasets --> Go importer --> Postgres --> Go API --> React grid (web/)
                      |                ^
                      +-- OMDb / TMDb -+   posters, dates, colours, id matches
```

A reader's request never crawls, never calls TMDb to build the map, and never writes the tables it reads. Pictures, full release dates, poster colours, and the IMDb-to-TMDb id map are filled in beside the catalog, into a `meta` schema that survives every daily swap. The map is computed live from the tables, so a poster learned an hour ago is on the next read.

The whole movie catalog is a local copy of the [IMDb non-commercial datasets](https://developer.imdb.com/non-commercial-datasets/). Those five files are the source. Postgres is the only dependency the current app needs.

An older map, crawled from TMDb into Neo4j, is still in the tree. Nothing uses it while `DATABASE_URL` is set. It is described at the end.

## What you see

The page title is **Cinedikt — a movie’s cast and directors, and everything they made**. A map's tab is `{title} — everything its cast and directors made · Cinedikt`.

### Opening

`/` is a cold screen: **Start with a movie you love**, and under it, **See every movie its cast and directors made, arranged by year and rating.** Eight poster tiles, one from each era, and a different eight every visit. A short window shows fewer, as whole rows, so the last row is not cut off: two columns below 640px, all eight in one row on a landscape phone, four otherwise, never more than eight. Tiles without a picture or a year are dropped. A long title is kept and ellipsised.

The mark draws itself, then glides into the wordmark, unless the reader prefers reduced motion. Each tile is filled with the poster's average colour (`#rrggbb`) while the picture is still arriving, so a film shows its own colour before it shows itself. Until the server has worked that colour out, the client paints one derived from the title.

If the suggestions cannot be loaded, the screen says so and leaves the search.

### Search

The header search is **Search a movie**, or the current film's title once a map is open. While a map loads it names the film on its way, when the app was told which one (a search pick, a tile, a card); after Back, Forward or a reload it does not know, and says **Search a movie**. ⌘K or Ctrl+K from anywhere, or **/** when not typing in a field, puts you in it, except while a film sheet or the View panel is open. A **⌘K** hint shows in the empty field on anything wider than a phone. Two characters is the shortest query worth asking; shorter, and the field does not ask. Results arrive after 250ms of quiet, and the list stays open while the query is two characters or more, even once the field has lost the focus. Arrow keys move the highlight, Enter picks it, Escape clears the field and leaves it. The field is a combobox, so a screen reader hears the highlighted film as the arrows move. Ten films come back, each with a poster and a year. **Searching…** shows while the first answer is on its way, and **No movies match “…”** is the empty answer.

### The map

`/movie/tt0133093-the-matrix` is one map. The id is IMDb's `tconst`. The slug is there so a pasted link says what it opens; an id alone is a valid route, and a stale slug still opens the same film. Old `/film/…` links are redirected, permanently, to `/movie/…`. There is no `?movie=` any more: that parameter carried a TMDb id, and a TMDb id is not an address in this catalog.

While the map loads, the screen stays empty rather than drawing a skeleton. The header shows chip-shaped placeholders and a progress bar. A toast says **Finding everyone who made {title}…**, then **Laying out their movies…**. Cards then ripple out from the searched film, delayed by how far they sit from it, capped at about half a second.

The region is labelled **Movies by year and rating**. Down the left is a year rail: decades emphasised, and the searched film's year highlighted unless that highlight is turned off. Across the top is a rating strip, because once the map has been panned sideways the gridlines alone no longer say where you are: a label on each gridline, and **IMDb rating →** where the scale starts. Gridlines sit at 4.0 through 9.0. A year with nothing in it still takes a gap, marked, so a jump in the cast's careers is visible. Inside a year, cards stack by month and day. Those dates are not labelled. The order is the label.

Unrated films sit in their own column on the left, headed **Unrated** and set off by a dashed rule, rather than at the bottom of the scale. A rating of zero would be a lie; the datasets write no rating at all.

The rating domain is fixed at 3.5 to 9.2, so a 7.4 sits in the same place on every map. Cards in the same year share lanes. A card may be nudged sideways by up to a quarter of its width to join a lane that is already open. Past that it would be lying about its rating, and a new lane opens instead. A card never takes a lane above the one placed before it, so vertical position inside a year means calendar order, whichever way the years run. Newest-first flips the months with the years: time does not run backwards between years and forwards inside them.

Every card is a poster, a title once its detail has arrived, a rating or **No rating**, and a mark for each person from the searched film who is on it: a swatch in that person's colour, up to five of them, or four and **+N**. A desktop or tablet card with one or two people adds their initials. When the footer runs out of room the initials go first, then swatches, into the **+N**; the rating always keeps its place, and where not even one swatch fits beside it (an unrated card on a phone) the card shows no marks. The sheet still names everyone. Initials grow from two letters toward four until two people on the same card no longer collide. The searched film's card has its own fill, an accent ring and a **Searched** tag on its corner; a screen reader hears its title, year and rating, then **the searched movie**. Recenter plays that ring wider for a moment when it lands. Pointing at a card, on a device that hovers, lights the matching chips. A tap opens the sheet. A tap that moved, or that landed while the map was still settling from a scroll, does not.

Only the cards in a warm band are mounted: one viewport above the fold and two below. Their titles and posters are fetched for those ids. Hovering a card, or opening its sheet, prefetches that film's own map so **Map this movie** does not wait on a cold read.

**Recenter** scrolls the searched film back to the middle and rings it briefly.

### People

Under the header, **Everyone**, then a chip for every person the map is built from. People who were on the map shown before this one come first. After each name is how many of that person's films the map holds: every one of theirs on the spine, the searched film included, whatever the filters are doing. The spine is the four hundred most-voted, so it is their films on this map, not their career. A spine without people on its rows gives no counts. Each person gets a colour of their own the first time they appear and keeps it for the rest of the visit, on their chip, their marks on the cards and their row in the sheet. Shape says the role: a round swatch is cast, a square one a director. Selecting a chip does not move a card. It dims everything that person is not on, to 12% opacity. The searched film stays lit: it is the centre of its own map. With nobody selected, everyone counts. Hovering a chip previews that dimming without committing it, and without reflowing the grid out from under the pointer.

A person who both acted in and directed the searched film is shown as a director. Directors come first, in the order IMDb lists them, then the billed cast.

On a phone, selecting a single person says **Showing only {name}**, with Undo.

### The sheet

Tapping a card opens a dialog: a panel down the right on a desktop (430 wide), a tablet (400) or a landscape phone (half the screen, up to 400), and a sheet from the bottom on a phone, each held a few pixels in from the edges. Its top is washed in the film's own hue. It shows the poster, the title, the year and the rating. The searched film is labelled **Searched movie**. Any other rated film says how its rating sits against the searched one — **0.4 above {anchor}**, **0.3 below {anchor}** or **Same as {anchor}**, with both ratings — and draws it on a 4 to 9 scale, the film as a knob and the searched film as a tick.

**Map {title}** (or **Map this movie**, when the title is long) opens a new map centred on that film. The card makes the trip itself: it rises off the old map as that map fades, glides to where the new map will hold it, and settles into place as the searched card while the rest of the map spreads out around it. A map still loading keeps it waiting in the middle. It sits under the comparison; on a phone it is pinned to the foot of the sheet, where a thumb reaches it and a scroll cannot end on it. The people section is **Its cast and directors** on the anchor, and **Connected to {anchor} through** on every other card. Each person says what they did on the searched film: **Directed {anchor}**, **{character} in {anchor}**, or **In {anchor}**. **Show only** selects that chip.

On a phone the sheet is dragged down to dismiss. Anywhere, the scrim, Escape, or the close button does it. Focus is trapped while it is open. The action — filter, or remap — runs after the sheet has animated out: 280ms, or at once for a reader who has asked for reduced motion.

### Narrowing a map

Two different kinds of control, and they are not the same kind of change.

**The rating floor lights films. It does not remove them.** The grid's argument is where a film sits on the scale, and a film that leaves the page cannot make it. The rungs are Any, 6.0+, 6.5+, 7.0+, 7.5+, 8.0+, 8.5+. Pressing the lit one again clears the floor. On a wide desktop they sit in the header (**Light movies by rating**). Below 1024px they move into the View panel (**Light movies rated at least**). An unrated film clears no floor, because there is nothing to compare. A floor that leaves one person's work entirely dark says so: **Nothing of {name}’s is rated {n} or higher**, with Clear. Otherwise: **Lighting movies rated {n} and up**.

**The year range removes rows.** Years are the rows themselves, so cropping them takes nothing away from what the rating is claiming. The slider's ends are the map's own oldest and newest years, so a reader is never offered a decade this cast never worked in. Over the slider, a bar for each year shows how many of the map's films it holds, whatever the other filters are doing, with the years inside the range in the accent. The range is said beside the heading once the thumb is let go, and **Reset** clears it. The searched film is never cropped. If its year falls outside the range, its row stays, separated from the rest by a **· · ·** break, because a map without the movie it is of is not a shorter map. If the range holds none of this cast's other films, the panel says so.

**Hide empty years** collapses rows where nothing is lit — by the people selected, the rating floor, or both. It applies to every filter. If that would leave only the searched film, a toast says **Nothing else matches. Showing only {title}.** and offers **Show all years**.

**Show unrated movies** takes the unrated column off the plot. Those cards really do leave; the column is a different thing from the rating floor. The anchor stays even if it has no rating.

Turning the year order, the unrated column, or the year range recentres the map on the searched film. Hiding empty years animates the rows that remain into their new places.

A filter pill in the header names what is hidden: the floor (only when the rungs are not already in the header), and the year window. Hiding empty years is not on the pill. It filters no movie out; it only closes up the gaps, and that is visible. The pill's ✕ clears everything the pill names: the floor, when the pill is where it is shown, and the year window. The View button counts how many drawing choices differ from the defaults. The panel it opens is a popover over it on a desktop or a tablet, where a click outside closes it; a sheet from the bottom on a phone; and a panel down the left of a landscape phone, which has the width and not the height. On a phone or a landscape phone it dims the map behind it.

### What is remembered

How the map is drawn stays with the reader, in `localStorage` under `cinedikt.grid`: newest or oldest first, whether unrated films show, whether the searched year is highlighted. A filter does not. Who is selected, the rating floor, the year window, and whether empty years are hidden belong to the visit they were set on. They live on the history entry. Opening another movie — from search, from a card, from home — starts clear. Going back restores what that map had. A stored `density` setting, from a build that no longer has it, is dropped rather than carried forward.

The theme is separate, under `cinedikt.theme`: System, Light, or Dark. It is applied before the first paint, from a script in `index.html`, so a reader who chose light does not see a dark frame and then a white one. The page background is `#f9f4ee` in light and `#13100d` in dark.

### Getting around

Back, in the header, is the browser's own back. It is there on a map that has somewhere to go back to: not on the opening screen, and not on a map opened straight from a link. The wordmark goes home and clears the movie, keeping any query string and hash. The stack records a depth, so back from a film you remapped into returns you to the map you remapped from, filters and all.

On a phone, and on a short landscape phone, the header overlays the map as glass and hides as you scroll down past the first 80px. Scrolling back up, or coming within 40px of the top, brings it back. It never hides for a scroll the app makes itself — centring a new map, Recenter, rows closing up — and it stays put while the map is loading, while the sheet or the View panel is open, and while the search is focused.

### Sharing

Every map has an address, so it can be shared. The API serves `index.html` for any path it does not have a file for, and rewrites the tags a scraper reads when the path is a movie: the title, the description (**See every movie {title}’s cast and directors made, arranged by year and rating.**), the canonical URL with the current slug, and a 1200×630 PNG of that film's poster beside its title. The card is drawn in Go, in the fonts the page uses, and it is dark in both themes — it appears in somebody else's chat window, where the app's theme means nothing. The lookup is given 300ms. Not knowing the film, or being too slow, leaves the generic tags alone.

The picture lives at `/og/movie/{tconst}.png?v={stamp}`. The stamp changes when the poster, the title, or the template does, which is how an unfurler that caches by address ever sees a new one. Rendered cards are stored in `meta.og_images`. As a map opens, the client fetches that address (unless the reader has asked for reduced data), so the picture exists before anyone copies the link.

### When it fails

**We couldn’t open this map. The movie database didn’t answer. Check your connection, then try again.** Try again repeats the read. Pick another movie goes home.

## What gets onto a map

A map is three things: the searched movie, the people billed on it, and a spine of the other movies those people made.

The people are the billed cast (`actor`, `actress`) and the directors. IMDb splits cast by gender; the map does not, so both are cast. Writers, composers, and the rest of the crew are not stored. A director is often missing from the billed principals, so directors are also read from `title.crew`. That is why the file is imported at all. The first character name on a credit is kept (`["Neo"]` becomes `Neo`); a card has room for one part.

A film is on the spine when someone from that chip row acted in it or directed it, and when it passes the film test:

- it is not marked adult
- it has a release year
- it is not a Documentary
- its year is not in the future
- its full release date, when one is known, is not in the future

A documentary is a movie and an adult title is a movie, so both are stored. Neither is mapped. A movie the poster job has not reached yet has no release date, and is placed on its IMDb year. Holding it back would empty most of a map until the backfill finished.

The spine is capped at **400 films**. A career is wider than a screen, and a thousand cards is a map nobody can read. The searched film is kept first; after that, the most voted survive. The cap is there so the map can be read. It is not a complete filmography.

There is no precomputed spine. The two joins are indexed, and reading them live means the map is never a generation behind its own posters.

Each spine row is five facts, and nothing else: the IMDb title id, the year, the rating or null, the month and day as `MMDD` (0 when only the year is known), and which of the chip row are on it. Those people are indexes into the chip row, not name ids. There are up to four hundred films and several dozen people, so an id on every row would be most of the payload. The indexes are what let the client decide whether a year is empty before anyone has scrolled to it. Hiding empty years asks that of every year, including the ones with no detail fetched yet.

What a card *says* — title, poster, the people's name ids — arrives a screen at a time, at most 200 ids a request, for the cards that are actually mounted. A card in that batch with no poster is marked wanted, and the TMDb job repairs it off the request. Nothing the reader is waiting on waits for that.

A movie with nobody billed has no map. The API answers 404.

## Layout

```
cmd/api/            HTTP server, the share cards, and (by default) the importer
cmd/importer/       the same catalog jobs, run on their own
cmd/crawler/        the old Neo4j crawler; unused while DATABASE_URL is set
internal/catalog/   Postgres catalog: import, search, grid, posters
internal/api/       handlers; catalog when a database is configured, graph otherwise
internal/config/    environment
internal/omdb/      poster and release-date lookups
internal/tmdb/      poster fallback, id matching, empty-search fallback
internal/notify/    job notifications
internal/telegram/  optional Telegram sink for those notifications
internal/analytics/ PostHog, production only
web/                the React grid
```

The packages below belong to the crawling map and are not on the path a catalog deployment runs: `internal/graph`, `internal/crawl`, `internal/app`, `internal/rediscache`, `internal/seen`. Redis is still read, optionally, to cache the TMDb responses of the search fallback.

## Setup

Postgres is the only dependency. Copy `.env.example` to `.env` and set `DATABASE_URL`.

```bash
cp .env.example .env
createdb cinedikt
```

`OMDB_API_KEY` (free at [omdbapi.com](https://www.omdbapi.com/)) is what puts pictures and full dates on the cards. Without it the catalog still serves: cards have no posters, and order inside a year falls back to the title id because every month-day is 0. `TMDB_API_KEY` or `TMDB_ACCESS_TOKEN` is the second chance for pictures OMDb does not have, and the thing an empty search asks. Either credential is enough; the access token is preferred when both are set.

## Run

```bash
go run ./cmd/api
```

On an empty database the API starts the import itself, behind the server. Until a generation has been published, catalog routes answer **503** with `the catalog is still being built`. The health check still answers, so a deploy is not failed for being mid-import. The first import takes about five and a half minutes: roughly two of those are the 1.35 GB download, and the rest is reading the files and building the indexes. It logs where it has got to, in four numbered steps.

Set `EMBEDDED_IMPORTER=false` when a separate process is doing that work. Two of them is safe and pointless: a Postgres advisory lock (`0x63696e6a`) means only one runs, and the loser waits and retries every 30 seconds.

```bash
go run ./cmd/importer              # hourly, until interrupted
go run ./cmd/importer -once        # one attempt, then exit
go run ./cmd/importer -posters-only
go run ./cmd/importer -dir /tmp/imdb -keep
```

| Flag | What it does |
| --- | --- |
| `-once` | one attempt, then exit. A decision not to import is a success: most hours are |
| `-posters-only` | fill posters and release dates against the live catalog, then the TMDb fallback, and do not import |
| `-keep` | leave the downloaded files on disk, for a development re-run |
| `-dir` | where to put them. The default is a temp directory |

The frontend, against an API on `:8080`:

```bash
cd web && npm install && npm run dev      # http://localhost:5173
```

Vite proxies `/api` to `:8080` and strips the prefix, which is what the API expects when it is not itself serving the bundle.

For a single process, build the bundle and point the API at it:

```bash
cd web && npm run build
WEB_DIR=web/dist go run ./cmd/api         # http://localhost:8080/movie/tt0133093-the-matrix
```

Without `WEB_DIR` the API also answers at `/`, so the curl examples below keep working. With it, the same routes live under `/api`.

```bash
curl -s 'localhost:8080/search/movies?q=matrix' | head -c 400
curl -s 'localhost:8080/grid/tt0133093' | head -c 600
```

## The catalog

`internal/catalog/README.md` is the operational note for the importer. What follows is what that work is for.

### What is kept

Five files, from `https://datasets.imdbws.com/`, and nothing else. `title.akas` and `title.episode` are out of scope. `knownForTitles` on `name.basics` is a handful of highlights rather than a filmography, so that file is read for names alone.

| File | What is kept |
| --- | --- |
| `title.basics` | rows whose `titleType` is `movie`. Nine of every ten rows is a television episode; series, shorts, videos, and TV movies go with them. About 757,000 movies remain |
| `title.principals` | `actor`, `actress`, and `director` credits on those movies, with the first character name |
| `title.crew` | the director list, in IMDb's order, including directors the principals file never billed |
| `title.ratings` | average and vote count, for kept movies. An unrated title is no row, not a zero |
| `name.basics` | a person only if a kept credit named them, with birth and death year |

Adult titles and documentaries are loaded and then left off every map, every search, and the cold-screen pool.

### What happens each hour

A generation is the five `Last-Modified` stamps, read with `HEAD`. Nothing runs until every one of them has moved past what was last published: one file arriving ahead of the others is half a generation, and half a generation on top of yesterday's rest is a catalog whose credits and titles disagree. In practice IMDb publishes all five within about a minute of each other, once a day. The hourly check is not about catching that minute. It is about not waiting most of a day after it.

When they have all moved, the files are streamed to disk and the stamps are read again. If anything moved while they were being fetched, what is on disk is a mixture and the attempt is thrown away. The client names itself `cinedikt-catalog/1 (+https://cinedikt.com)`.

The load goes into `catalog_next`, by `COPY` into unlogged tables, so forty million rows are not paying for the WAL on the way in. Readers only ever touch `catalog`. Titles are read first, and those ids are the allow-list every other file is filtered against. Names are read last, so a person is stored only if a kept credit named them.

Then the tables are set logged, the indexes are built, the cold-screen pool is filled, and the schema is analyzed. Building the indexes is a minute or two and logs nothing until it is done. An index maintained during the `COPY` would cost more than one built once over finished data. Search uses a trigram index (`pg_trgm`, installed in the `meta` schema) over the primary and the original title, and only over titles that are not adult. Credits are indexed in both directions: a map is "this film's people, then everything those people made."

Before anything is published, the load is checked. The tables must be non-empty, and at least 99% of credit rows must point at a title that was stored. Less than that means the files are probably from different generations, and the attempt is not published.

Publish is one transaction: `catalog` becomes `catalog_old`, `catalog_next` becomes `catalog`, and the generation is recorded in `meta.generation`. The old schema is not dropped there. It is left for the next run, so a reader holding a plan against it finishes against data that still exists. `lock_timeout` bounds the swap at five seconds. Failing fast leaves the previous catalog serving, which is the right way to lose. A Postgres `NOTIFY catalog_published` wakes anything waiting on the new generation.

`meta.last_check` records every HEAD, including the hours that did not import, so "checked an hour ago, nothing had moved" is distinguishable from "nothing has run for a day" without reading logs.

A catalog older than 36 hours is logged every hour. On Telegram, if that is configured, it is said once per catalog at 36 hours and once more at 72 hours, and a restart does not say it again. It is deliberately not a health-check failure. A stale catalog still serves, and failing the check would turn a late upstream publish into a failed deploy.

### Posters, dates, colours, ids

The dump has no pictures and no month or day. One OMDb lookup per title stores the poster's address and the full date. The browser loads the image from Amazon. The key never leaves the server.

That job is not part of an import. A full first pass is about 27 minutes at the default 500 requests a second across 256 workers, measured nearer 466 a second. Nothing waits on it. A poster appears the moment it lands, and the films anyone would actually search are done in the first minute, because the job takes the most voted first. It picks up where it left off. `meta` is never renamed by the daily swap, so what it learns survives every future generation.

A lookup that came back empty is still an answer, and is not asked again. Only a lookup that failed is retried: after a day, and never twice in the same run. An address that has been seen to 404 is `dead`. Image edges replay a miss for about five minutes and then serve the picture again, so the first 404 only keeps a film off the draw that saw it. A second, after that window, is the picture actually being gone, and it is written down so the next cold screen does not ask and the TMDb job has something to repair. A host that does not answer at all is neither: Amazon being briefly unreachable never empties the opening screen and never queues a live poster for replacement.

OMDb has a poster for about 59% of the catalog. TMDb has one for roughly three-quarters of what is left. Without TMDb credentials those movies simply have no picture. With them, two jobs share one rate-limited client, so they do not each spend TMDb's ceiling:

- **Posters.** A movie is fetched when a reader has opened one with no picture (`wanted_at`), and otherwise only when it has at least `TMDB_SWEEP_MIN_VOTES` votes. The default is 100. Around 310,000 titles have no poster and about 1,300 of them have a hundred votes, so the default sweep is half a minute, and the rest are repaired the moment somebody meets them. Set the floor to 0 to ask about every title with no picture: about 300,000 lookups, roughly two hours at 40 a second, once. The well-known end of that queue yields a poster about 77% of the time. The zero-vote tail yields one about 8% of the time. The default TMDb rate is 40 requests a second, which is TMDb's own ceiling.
- **Ids.** `meta.tmdb` maps a TMDb movie id to a `tconst`, filled ahead of time. An empty catalog search asks TMDb for ids and keeps a hit only when that map already has it. The search does not ask TMDb, live, which IMDb title an id is. A row with a null id is "asked, and it is not a movie we can map."

The colour job needs no credentials. It downloads posters that are already public and stores what they average to, as `#rrggbb`, for the frames on the opening screen. It colours the pool, not the eight somebody happened to see: the next visit draws a different eight.

`cmd/importer -posters-only` runs the OMDb pass and then the TMDb poster pass, and does not import.

### Cold screen pool

The eight eras are not equal spans. The point is a spread a reader recognises, and more films anyone has heard of were made recently than in the 1930s:

| Era | Years |
| --- | --- |
| 1 | 1920–1959 |
| 2 | 1960–1979 |
| 3 | 1980–1994 |
| 4 | 1995–2004 |
| 5 | 2005–2012 |
| 6 | 2013–2018 |
| 7 | 2019–2023 |
| 8 | 2024 onward |

The pool is the 250 most-voted mappable films of each era, ranked once at import. Ranking them live would mean sorting a quarter of a million rows to choose eight, and no index can help: the year is on `titles` and the votes are on `ratings`. Posters are deliberately not part of the pool. They arrive on their own schedule, long after the import, so the read picks from whichever candidates have a picture by then, up to twelve per era, in random order, and probes the image host for at most four seconds. A tile whose poster 404s is skipped and the next candidate in the era takes its place. A film with no colour yet simply goes without one.

The pool only changes when a generation does.

### Search ranking

Search sees the primary title and the original title, case-insensitive, as a substring. A name IMDb files only as an alternative comes back empty, and that is when TMDb is asked. The fallback is given three seconds. TMDb being down, or naming a film this catalog cannot map, is the empty list the catalog itself returned, not an error.

Votes do almost all of the ranking. They are the best proxy the dataset has for "the one they meant": `matrix` has to put The Matrix above the thirty other films with the word in the title. An exact primary title is worth fifty times the votes, as a multiplier rather than a rank of its own. There is a film called "Godfather", and it is not the one anybody means; a multiplier lets the famous one win, and still lets a small film typed in full beat a blockbuster that merely contains the words. A bonus for titles *starting* with the query was tried and removed. It put "Matrix Zone" above "The Matrix", because an article at the front is enough to lose a prefix match and no number of votes could win it back.

A hit also has to be a film a map can be built from: the film test, plus at least one billed principal.

## Routes

Catalog routes, as the process sees them. In the browser they are the same paths under `/api`. Every catalog response is `Cache-Control: no-store`. A handler error is `{"error": "…"}`. Each API request has a 40 second deadline.

| Route | What it does |
| --- | --- |
| `GET /healthz` | Pings Postgres and answers 503 if it cannot. `{"status":"ok","postgres":"ok"}` when it can. A stale catalog still passes |
| `GET /analytics-config` | `{"token","host"}`. Both empty unless `APP_ENV=production` |
| `GET /search/movies?q=matrix` | Up to ten movies. `q` must be at least two characters, or 400. 503 while the catalog has never been published. Body is `{"results":[{id,title,year,poster?,c?}]}` |
| `GET /` | The cold screen. One film per era that has a live poster, a different set each visit. A database error here is an empty list and a log line, not an error page: an empty opening is better than a failure on the way in |
| `GET /grid/{tconst}` | The whole map: anchor, people, spine, and `og_v`. 400 if the id is not `tt` plus digits. 404 if the catalog has no such title, or the title has nobody billed. 503 if the catalog is not published yet |
| `GET /grid/{tconst}/films?ids=tt1,tt2` | What the mounted cards say. `ids` is required, comma-separated, at most 200, each a `tconst`. Cards with no poster are queued for the TMDb job |
| `GET /posters/{tconst}` | `{"poster":"<url>"}` when a picture the browser could not load has a TMDb stand-in. The stand-in is written back, so the next read does not ask again. 404 when there is nothing, or TMDb is not configured. 502 when the lookup failed |
| `GET /og/movie/{tconst}.png` | The share card. Not under `/api`. Its own, shorter budget |

`c` on a search or cold-screen hit is the poster colour, present only once it has been worked out. A wrong colour is worse than none; the client has its own fallback.

```json
{
  "anchor": {
    "id": "tt0133093",
    "title": "The Matrix",
    "year": 1999,
    "rating": 8.7,
    "md": 331,
    "poster": "https://m.media-amazon.com/images/M/….jpg",
    "released": "1999-03-31",
    "people": ["nm0000206", "nm0905154"],
    "isAnchor": true
  },
  "people": [
    {
      "id": "nm0905154",
      "name": "Lana Wachowski",
      "role": "director",
      "order": 0
    },
    {
      "id": "nm0000206",
      "name": "Keanu Reeves",
      "role": "cast",
      "character": "Neo",
      "order": 1
    }
  ],
  "films": [
    ["tt0133093", 1999, 8.7, 331, [0, 1]],
    ["tt2911666", 2014, 8.1, 1016, [1]]
  ],
  "og_v": "a1b2c3d4"
}
```

`films[n]` is `[id, year, rating or null, MMDD, people indexes]`. `rating` is IMDb's average, one decimal, over the vote count stored beside it and not sent on the spine. `md` is March 31 as `331` and October 16 as `1016`.

`GET /grid/tt0133093/films?ids=tt0133093,tt2911666` answers `{"films":[…]}` with the same movie object the anchor uses, `people` as name ids rather than indexes, because a card looks each person up to draw a marker.

Reads are bounded at five seconds. A map is two indexed joins; anything slower is a problem to see rather than to wait through.

Hashed assets under `/assets/` are cached for a year. `index.html` is `no-cache`, so a deploy's new asset hashes are picked up. `/film/…` is a 301 to `/movie/…`, query string included.

## Frontend

React 19 and TypeScript, bundled with Vite 6. There is no router and no state library. The URL and `history.state` are the route. Styling is one stylesheet, `web/src/grid.css`, with Young Serif for the display type (the wordmark, headlines and card titles) and Figtree for everything else.

| Module | What it decides |
| --- | --- |
| `GridApp.tsx` | The screen: route, search, cold start, header, toasts, when to fetch |
| `GridMap.tsx` | The scroller, the cards, the reveal, recenter |
| `grid.ts` | Layout, what is lit, lane packing, the rating domain. Pure: a payload, a width, and the settings go in, and positions come out |
| `trail.ts` | Which filters belong to this history entry, and which preferences belong to the reader |
| `api.ts` | `/api` client. Sends PostHog's distinct id and session id once analytics is up |
| `movieParam.ts` | `/movie/{tconst}-{slug}`, the tab title, the slug rules the server's `og:url` is kept in step with |
| `firstRun.ts` | How many cold-screen tiles fit |
| `PeopleChips.tsx`, `GridSheet.tsx`, `ViewPanel.tsx` | The chip row, the film sheet, the View panel |
| `poster.ts`, `PosterImage.tsx` | Resize Amazon and TMDb poster URLs, retry a miss, ask `/api/posters/{id}` for a stand-in |
| `theme.ts` | System, light, dark |
| `screen.ts` | The screen classes — phone below 640px, short (under 500px tall), tablet below 1024px, desktop — and touch sizing. Below 1024px the rating rungs move into the panel. `grid.css` names the same classes as media queries |
| `overHeader.ts` | The overlay header, and when it hides on scroll |
| `sheet.ts` | Enter, exit, drag-to-close, focus trap |
| `analytics.ts` | PostHog, in its own chunk. Loopback never initialises |

`web/src/api.ts` still has a client for `GET /movies/{id}/pathways`. The grid does not call it. It belongs to the crawling map.

The dev server is port 5173. `npm run build` typechecks and writes `web/dist`. `npm test` is Vitest, in Node, over the layout, the history trail, the posters, the sheet, and the cold-screen arithmetic. `web/src/fixtures/matrix-grid.json` is a fixture for those tests, not something the app loads.

## Configuration

`APP_ENV` is `development` (the default) or `production`. Only production reports to PostHog. Nothing infers the environment from a hostname or a log level: a production deploy that happens to reach its database over localhost would otherwise go quiet. `dev` and `prod` are accepted. Anything else is a startup error. The Docker image sets `APP_ENV=production` itself.

`API_ADDR` wins over `PORT`. Unset, with no `PORT`, the process listens on `:8080`. `LOG_LEVEL=debug` turns on debug logs.

| Variable | Default | What it does |
| --- | --- | --- |
| `DATABASE_URL` | | Postgres. Set, and this is the catalog app. Unset, and the process requires the old map's credentials instead |
| `EMBEDDED_IMPORTER` | `true` | Run the catalog jobs inside the API. `false` leaves them to `cmd/importer` |
| `CATALOG_API_MAX_CONNS` | 10 | The pool that serves reads |
| `CATALOG_IMPORTER_MAX_CONNS` | 4 | The pool the jobs write through. Kept apart so a bulk `COPY` cannot take the connections a search is waiting for |
| `OMDB_API_KEY` | | Posters and release dates |
| `OMDB_BACKFILL_RATE` | 500 | OMDb requests a second. This bounds open sockets and how fast rows arrive, not a quota |
| `POSTER_WORKERS` | 256 | Lookups in flight. A round trip is about 400ms, so 256 in flight is about 600/s: enough that the rate stays in charge. Writes are batched, 500 titles to a statement |
| `TMDB_API_KEY`, `TMDB_ACCESS_TOKEN` | | Poster fallback, id matching, empty-search fallback |
| `TMDB_RATE_PER_SEC` | 40 | |
| `TMDB_SWEEP_MIN_VOTES` | 100 | Vote floor for fetching a poster nobody has asked for yet. `0` sweeps every title with no picture. An explicit 0 is kept |
| `TMDB_CACHE_TTL` | `168h` | Redis TTL for TMDb search responses, when `REDIS_URL` is set. Without it, those responses are not cached |
| `REDIS_URL` | | Optional cache for that fallback only, prefix `cinedikt:tmdb` |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | | Job notifications, described below. Both empty, and nothing is sent. The token is from BotFather. The chat id is `message.chat.id` from `getUpdates` after Start — positive for a private chat, negative for a group. In a group the bot needs permission to pin messages |
| `NOTIFY_TIMEZONE` | UTC | The zone notification times are written in, such as `Africa/Lagos`. Unset or unknown, they are UTC and the board says so |
| `WEB_DIR` | | Built frontend. The image sets `/app/web/dist` |
| `POSTHOG_PROJECT_TOKEN`, `POSTHOG_HOST` | host `https://us.i.posthog.com` | Read only in production. The API serves them to the page; the token is a write-only key. The page may instead be built with `VITE_POSTHOG_PROJECT_TOKEN` and `VITE_POSTHOG_HOST` |
| `NEO4J_*`, `CRAWL_THRESHOLD_BASE`, `CRAWL_ORDER_PENALTY`, `MAX_COLD_CRAWLS` | | The old map. Ignored while `DATABASE_URL` is set |

If `DATABASE_URL` is empty, startup requires a TMDb credential and `NEO4J_PASSWORD`. A deployment with nothing but a database URL starts.

### Telegram

The process that runs the catalog jobs keeps one pinned message in the chat: a headline that says whether anything needs you, then one line for each of the five jobs (Catalog, Posters, Backup posters, Search matching, Opening colours), and a footer with the next catalog check and the deploy. It is edited in place, at most once a minute while something is running and at least once an hour, and an edit never makes a sound. A next check already in the past, or an "updated" time more than an hour old, means the process is stuck.

A new message is sent only when there is news, and its second line says whether anything is needed of you, so a lock screen shows the answer before the explanation. Good news is quiet: a new catalog going live, a failure that is over, a pass that ran for half an hour or more. It lands in the notification list without a sound. A sound is kept for what has lasted or needs you: a failure that has repeated for about an hour (at once for a refused API key, at the second attempt for an error of no known kind), the database unreachable for two minutes, the catalog at 72 hours old (and at 36 while its updates are failing; late only because IMDb has published nothing, it is said at 36 without a sound), and a reminder for anything still failing a day later. The opening colours are cosmetic and never make a sound. Each is said once. What has been said lives in `meta.notify`, so a deploy does not repeat it. Only the process that holds the jobs writes the board: a container waiting for the lease never touches it, and one that loses the lease stops, except to say the database is unreachable while nobody can hold it.

A one-off `cmd/importer -once` or `-posters-only` sends one quiet summary when it ends and leaves the board alone.

## Logs

In production the API writes single-line JSON to stdout (`config.NewLogger`). Locally it writes readable text to stderr. Railway turns anything on stderr into an error, so plain text there makes every served request look like a failure. JSON hands `method`, `path`, `status`, `duration_ms`, and `bytes` over as fields — `@status:>=500`, `@duration_ms:>500` — rather than a string to grep. The logger is built from `APP_ENV` before configuration has finished loading, so a bad variable can still be reported.

Panics at the request boundary are logged and answered 500. In production that log is also an exception in PostHog.

## Deploy

Railway is the deployment target. The service is defined in [`.railway/railway.ts`](.railway/railway.ts), which replaces the deprecated `railway.toml`: the Dockerfile builder, a health check on `/api/healthz` with a 120 second timeout for a cold start, and every variable name the service holds. Values are `preserve()`d, so secrets stay in Railway and out of the repository. A name missing from that list is a name the next `railway config apply` deletes.

```bash
railway up
```

```bash
cd .railway && npm install     # once, so the CLI can evaluate the config
railway config plan            # review; expect "0 to destroy"
railway config apply
```

`partial = "cinedikt"` owns this service only. Postgres is provisioned beside it and is deliberately not claimed here.

The [Dockerfile](Dockerfile) builds the map with Node 22, the API with Go 1.26 (`-trimpath -ldflags="-s -w"`, `CGO_ENABLED=0`), and ships neither toolchain. The runtime is Alpine, with CA certificates, running as `nobody`. It sets `APP_ENV=production` and `WEB_DIR=/app/web/dist`. Railway injects `PORT`.

The restart policy is left at Railway's default, `ON_FAILURE`, and the service is not allowed to sleep. Declaring either would leave `config plan` permanently dirty, because the platform stores a default as null. Both matter: the importer runs between requests, and a sleeping machine would drop that work.

Variables to set on the service: `DATABASE_URL`, and, for pictures and the search fallback, `OMDB_API_KEY` and one of the TMDb credentials. `POSTHOG_PROJECT_TOKEN` if analytics should report. `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` if the jobs should report to a chat, with `NOTIFY_TIMEZONE` so its times are local. `WEB_DIR` only if it should differ from the path the image already sets.

## Tests

```bash
go test ./...
cd web && npm test
```

The catalog tests need a database of their own. They publish fixtures over `catalog` and empty `meta.posters`, so pointing them at a working catalog destroys it.

```bash
createdb cinedikt_test
CATALOG_TEST_URL=postgres://user:pass@host:5432/cinedikt_test go test ./...
```

Without `CATALOG_TEST_URL` those tests skip, and the rest of the suite still runs.

The `graph` package has an integration test that runs only when pointed at Neo4j. It writes ids from 900000000 upward and deletes them after:

```bash
NEO4J_TEST_URI=bolt://localhost:7687 NEO4J_TEST_PASSWORD=password123 go test ./...
```

## The old crawling map

Before the catalog, the map was a graph. Given a TMDb movie id, a crawler wrote everyone who acted in it or directed it, and the other movies those people appeared in or directed, into Neo4j. The API crawled a movie to depth 1 on the first request and warmed further hops in the background. Redis cached the raw TMDb and OMDb responses.

That path is what runs when `DATABASE_URL` is empty. It still answers `GET /movies/{id}/pathways` (TMDb numeric ids, not `tconst`s), and its own `/grid/{id}` and `/search/movies`. Cold crawls are capped (`MAX_COLD_CRAWLS`, default 8); a request that cannot get a slot answers 503 with `Retry-After`. Health checks Neo4j, and Redis when `REDIS_URL` is set. Seed it by hand with:

```bash
go run ./cmd/crawler -movie 603            # The Matrix, depth 1
go run ./cmd/crawler -movie 603 -depth 2 -v
```

The React app no longer speaks this API. It addresses movies by IMDb id and reads the catalog's grid. The crawler, the graph store, and the pathways handler are still accepted so the old path can be run until those packages leave the tree. A catalog deployment does not need Neo4j, Redis, or a crawl.
