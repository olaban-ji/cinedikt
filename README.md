# cinedikt

Start from a movie and see every movie its cast and directors made, arranged by release year and IMDb rating.

The searched film sits on a grid. **Y is the year. X is the rating**, low on the left and high on the right, on a scale that does not change from map to map. There are no edges. Who connects a card to the film you searched is said on the card and in the sheet that opens from it.

```
IMDb datasets --> Go importer --> Postgres --> Go API --> React grid (web/)
                      |                ^          |
                      |                |          +-- Streaming Availability, GeoLite2   where to watch
                      +-- OMDb / TMDb -+   posters, dates, synopses, trailers, photos, colours, id matches
```

A reader's request never crawls, never calls TMDb to build the map, and never writes the tables it reads. Pictures, full release dates, synopses, trailers, people's photos, poster colours, and the IMDb-to-TMDb id map are filled in beside the catalog, into a `meta` schema that survives every daily swap. The map is computed live from the tables, so a poster learned an hour ago is on the next read. Where to watch also calls out from a reader's request, as the poster stand-in and the empty-search fallback do: a movie and country nobody has asked about is asked of the Streaming Availability API once, and the answer is kept.

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

Under the header, **Everyone**, then a chip for every person the map is built from. People who were on the map shown before this one come first. After each name is how many of that person's films the map holds: every one of theirs on the spine, the searched film included, whatever the filters are doing. The spine is the four hundred most-voted, so it is their films on this map, not their career. A spine without people on its rows gives no counts. Each person gets a colour of their own the first time they appear and keeps it for the rest of the visit, on their chip, their marks on the cards and their row in the sheet. Shape says the role: a round swatch is cast, a square one a director. Selecting a chip does not move a card. It dims everything that person is not on, to 12% opacity. A card dimmed by the people selected, the rating floor or the genres cannot be opened, pointed at or focused, and a tap on it falls through to the map. A sheet already open for it stays open. The searched film stays lit: it is the centre of its own map. With nobody selected, everyone counts. Hovering a chip previews that dimming without committing it, and without reflowing the grid out from under the pointer. The preview takes nothing out of reach.

Resting the pointer on a chip, or on a face in a card's hover preview, for 400ms shows that person's photo bigger, with their name and what they did on the searched movie, the same line the sheet gives them. On touch a chip held for 450ms shows it, and letting go puts it away without toggling the chip. Once one is showing, or within 250ms of one closing, the next person's swaps in as soon as its photo has loaded. It waits for TMDb's 342-pixel photo rather than showing the soft 185, and nobody without a photo gets one. It goes when the pointer leaves, when the map scrolls, when the preview it came from closes, and on another map.

A person who both acted in and directed the searched film is shown as a director. Directors come first, in the order IMDb lists them, then the billed cast.

On a phone, selecting a single person says **Showing only {name}**, with Undo.

### The sheet

Tapping a card opens a dialog: a panel down the right on a desktop (430 wide), a tablet (400) or a landscape phone (half the screen, up to 400), and a sheet from the bottom on a phone, each held a few pixels in from the edges. Its top is washed in the film's own hue. It shows the poster, the title, the year and the rating. The searched film is labelled **Searched movie**. Any other rated film says how its rating sits against the searched one — **0.4 above {anchor}**, **0.3 below {anchor}** or **Same as {anchor}**, with both ratings — and draws it on a 4 to 9 scale, the film as a knob and the searched film as a tick.

**Map {title}** (or **Map this movie**, when the title is long) opens a new map centred on that film. The card makes the trip itself: it rises off the old map as that map fades, glides to where the new map will hold it, and settles into place as the searched card while the rest of the map spreads out around it. A map still loading keeps it waiting in the middle. It sits under the comparison; on a phone it is pinned to the foot of the sheet, where a thumb reaches it and a scroll cannot end on it. The people section is **Its cast and directors** on the anchor, and **Connected to {anchor} through** on every other card. Each person says what they did on the searched film: **Directed {anchor}**, **{character} in {anchor}**, or **In {anchor}**. **Show only** selects that chip.

On a phone the sheet is dragged down to dismiss. Anywhere, the scrim, Escape, or the close button does it. Focus is trapped while it is open. The action — filter, or remap — runs after the sheet has animated out: 280ms, or at once for a reader who has asked for reduced motion.

### Where to watch

The sheet has a **Where to watch** section after the trailer and before the rating comparison: rows of **Stream**, **Free**, **Rent** and **Buy**, in that order, each only when it has something. Every service is a chip with its logo, the dark one in the dark theme and the light one in the light, that opens the movie on that service in a new tab. Rent and buy chips carry the price. An add-on channel shows its own logo and **via Prime Video** (or whichever service it comes through). While the answer is on its way the section shows two skeleton bars. A movie on nothing in a covered country says **Not available to stream, rent or buy in the United States right now.**, naming the reader's country, and a country the data does not cover says **Streaming info isn’t available in your country yet.** If the answer fails, the section is left out.

The hover preview ends with a **Stream** row under Watch trailer: up to three services where the movie is included with a subscription or free, then **+N** for the rest. There is no row when nothing streams, when the country has no coverage, or until the answer has arrived. An answer that arrives after the preview has opened grows the row in; it folds away while the preview's trailer plays.

The reader never picks a country. The server works it out from their address.

### Narrowing a map

Two different kinds of control, and they are not the same kind of change.

**The rating floor lights films. It does not remove them.** The grid's argument is where a film sits on the scale, and a film that leaves the page cannot make it. The rungs are Any, 6.0+, 6.5+, 7.0+, 7.5+, 8.0+, 8.5+. Pressing the lit one again clears the floor. On a wide desktop they sit in the header (**Light movies by rating**). Below 1024px they move into the View panel (**Light movies rated at least**). An unrated film clears no floor, because there is nothing to compare. A floor that leaves one person's work entirely dark says so: **Nothing of {name}’s is rated {n} or higher**, with Clear. Otherwise: **Lighting movies rated {n} and up**.

**The year range removes rows.** Years are the rows themselves, so cropping them takes nothing away from what the rating is claiming. The slider's ends are the map's own oldest and newest years, so a reader is never offered a decade this cast never worked in. Over the slider, a bar for each year shows how many of the map's films it holds, whatever the other filters are doing, with the years inside the range in the accent. The range is said beside the heading once the thumb is let go, and **Reset** clears it. The searched film is never cropped. If its year falls outside the range, its row stays, separated from the rest by a **· · ·** break, because a map without the movie it is of is not a shorter map. If the range holds none of this cast's other films, the panel says so.

**The genres light movies too.** The View panel's Genres section, after the years, holds the 21 genres a mapped movie can carry as toggles in IMDb's alphabetical order, named as IMDb writes them (**Film-Noir**, **Sci-Fi**). A movie lights only when it has every genre picked (**Lights movies with all you pick**); the searched movie stays lit whatever is picked. Each toggle counts the movies on the plot that have every genre picked plus its own. The unrated switch and the year range decide what is on the plot, and the searched movie is on it; the rating floor and the people selected do not, so a count says what the map holds rather than what the other filters leave lit. A genre that would light nothing is greyed out and cannot be picked: no movie on the map has it (**none on this map**), or none has it together with the genres already picked (**none with the genres picked**). A picked genre can always be unpicked. **Clear**, beside the heading while anything is picked, unpicks them all. The View button counts the genres once, however many are picked.

**Hide empty years** collapses rows where nothing is lit — by the people selected, the rating floor, the genres, or any mix of them. It applies to every filter. If that would leave only the searched film, a toast says **Nothing else matches. Showing only {title}.** and offers **Show all years**. While the people selected, the rating floor or the genres are dimming cards, and hiding would close up at least one year (or it is already on), the same switch slides out of the right side of the View button. On a phone, Recenter shows only its icon while that switch is out.

**Show unrated movies** takes the unrated column off the plot. Those cards really do leave; the column is a different thing from the rating floor. The anchor stays even if it has no rating.

Turning the year order, the unrated column, or the year range recentres the map on the searched film. Hiding empty years animates the rows that remain into their new places.

A filter pill in the header names what is narrowed: the floor (only when the rungs are not already in the header), the year window, and the genres picked (**Sci-Fi**, **Action & Sci-Fi**, **3 genres**). Hiding empty years is not on the pill. It filters no movie out; it only closes up the gaps, and that is visible. The pill's ✕ (**Clear these filters**) clears everything the pill names: the floor, when the pill is where it is shown, the year window and the genres. The View button counts how many drawing choices differ from the defaults. The panel it opens is a popover over it on a desktop or a tablet, where a click outside closes it; a sheet from the bottom on a phone; and a panel down the left of a landscape phone, which has the width and not the height. On a phone or a landscape phone it dims the map behind it.

### What is remembered

How the map is drawn stays with the reader, in `localStorage` under `cinedikt.grid`: newest or oldest first, whether unrated films show, whether the searched year is highlighted. A filter does not. Who is selected, the rating floor, the year window, the genres picked, and whether empty years are hidden belong to the visit they were set on. They live on the history entry. Opening another movie — from search, from a card, from home — starts clear. Going back restores what that map had. A stored `density` setting, from a build that no longer has it, is dropped rather than carried forward.

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

Each spine row is six facts, and nothing else: the IMDb title id, the year, the rating or null, the month and day as `MMDD` (0 when only the year is known), which of the chip row are on it, and its genres as a bitmask over the grid's `genres` legend (bit i is `genres[i]`), 0 for none. Those people are indexes into the chip row, not name ids. There are up to four hundred films and several dozen people, so an id on every row would be most of the payload. The indexes are what let the client decide whether a year is empty before anyone has scrolled to it. Hiding empty years asks that of every year, including the ones with no detail fetched yet. The genres are on the spine for the same reason: the genre filter lights cards, and hiding empty years asks it of every year.

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
internal/streaming/ the Streaming Availability API, and the shape of a where-to-watch answer
internal/geoip/     GeoLite2 Country: placing an address, and keeping the database current
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

`OMDB_API_KEY` (free at [omdbapi.com](https://www.omdbapi.com/)) is what puts pictures, full dates and synopses on the cards. Without it the catalog still serves: cards have no posters, and order inside a year falls back to the title id because every month-day is 0. `TMDB_API_KEY` or `TMDB_ACCESS_TOKEN` is the second chance for pictures OMDb does not have, the thing an empty search asks, and where trailers are found. Either credential is enough; the access token is preferred when both are set.

`STREAMING_API_KEY` (from [Movie of the Night](https://www.movieofthenight.com/about/api)) turns on where to watch. `MAXMIND_LICENSE_KEY` and `MAXMIND_ACCOUNT_ID` (a free [GeoLite2](https://www.maxmind.com/en/geolite2/signup) account) let the server place a reader's address in a country. Without the streaming key the section is simply left out; without the MaxMind key every reader counts as a country without coverage, unless a CDN in front of the app says where they are in the header `GEO_COUNTRY_HEADER` names.

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
| `-posters-only` | fill posters, release dates and synopses against the live catalog, then the TMDb fallback, and do not import |
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

The dump has no pictures and no month or day. One OMDb lookup per title stores the poster's address, the full date and the plot. The browser loads the image from Amazon. The key never leaves the server.

That job is not part of an import. A full first pass is about 27 minutes at the default 500 requests a second across 256 workers, measured nearer 466 a second. Nothing waits on it. A poster appears the moment it lands, and the films anyone would actually search are done in the first minute, because the job takes the most voted first. It picks up where it left off. `meta` is never renamed by the daily swap, so what it learns survives every future generation.

A lookup that came back empty is still an answer, and is not asked again. Only a lookup that failed is retried: after a day, and never twice in the same run. OMDb's answers are not always valid JSON: a backslash before a letter, an escaped apostrophe, a raw tab or control byte in a plot, a backslash that swallows a field's closing quote. An answer that does not decode is repaired once and read again. One that still cannot be read is an answer as well, since OMDb sends the same bytes every time: it is stored as OMDb having no picture and no synopsis, so the TMDb stand-in and TMDb's overview fill in, and it is neither asked again nor counted as a failure. An address that has been seen to 404 is `dead`. Image edges replay a miss for about five minutes and then serve the picture again, so the first 404 only keeps a film off the draw that saw it. A second, after that window, is the picture actually being gone, and it is written down so the next cold screen does not ask and the TMDb job has something to repair. A host that does not answer at all is neither: Amazon being briefly unreachable never empties the opening screen and never queues a live poster for replacement.

OMDb has a poster for about 59% of the catalog. TMDb has one for roughly three-quarters of what is left. Without TMDb credentials those movies simply have no picture. With them, TMDb is asked by four jobs and by two things a reader can do (an empty search, a poster stand-in). TMDb counts about 40 requests a second per address, not per key, so every one of those waits on one limiter per process. `TMDB_RATE_PER_SEC` is the total for the process: 20 a second by default, half of TMDb's ceiling, with a burst of 5. A rate below 1 is refused at startup.

- **Posters.** A movie is fetched when a reader has opened one with no picture (`wanted_at`), and otherwise only when it has at least `TMDB_SWEEP_MIN_VOTES` votes. The default is 100. Around 310,000 titles have no poster and about 1,300 of them have a hundred votes, so the default sweep is about a minute, and the rest are repaired the moment somebody meets them. Set the floor to 0 to ask about every title with no picture: about 300,000 lookups, roughly four hours at 20 a second, and the same again, spread out, as those answers come due every 150 days. The well-known end of that queue yields a poster about 77% of the time. The zero-vote tail yields one about 8% of the time. TMDb's answer, a picture or "nothing", is stamped (`tmdb_at`). Once it is 150 days old it is asked again, after the wanted titles and the sweep: a picture of TMDb's, and TMDb's "nothing" for a title that still has no picture. A new picture replaces the old one in place, so no reader meets the film without one while it is refreshed. TMDb no longer having a picture takes its picture away. TMDb's "nothing" for a film whose picture from OMDb works is not asked again, since that film is not waiting on TMDb.
- **Ids.** `meta.tmdb` maps a TMDb movie id to a `tconst`, filled ahead of time. An empty catalog search asks TMDb for ids and keeps a hit only when that map already has it. The search does not ask TMDb, live, which IMDb title an id is. A row with a null id is "asked, and it is not a movie we can map." A match for a film search can still offer is asked again once it is 150 days old, after every title never asked, oldest first, and its row is replaced in place, so search and the trailer job never find it missing. Any other match waits for the backstop below.

TMDb's terms ask that anything cached from it be refreshed within six months, which is what the 150 days are for, here and for synopses, trailers and people's photos below. A re-ask can keep failing, a title or a person can leave the catalog, and a process can have no TMDb credentials to ask with, so a backstop runs every 20 minutes, whatever credentials the process has, and takes away the id matches, backup posters, release dates, trailers, people's photos and overviews from TMDb still there at 175 days. A match is deleted, and the id matcher queues its title afresh if search can still offer it. A picture of TMDb's is set back to none, a release date TMDb filled in where OMDb had none goes with it, and the stamp saying TMDb was asked is cleared, so a film left with no picture is back in the poster queue, for the sweep or the next reader who meets it. OMDb's addresses, and their `ok`, `missing` or `dead` status, are left alone.

### Synopses and trailers

A film's synopsis comes from OMDb. The poster pass asks for the full plot (`plot=full`) and keeps it with every answer it saves, so a title reached from here on costs no extra request. The titles it reached before it kept plots are asked again by the synopsis job, through the same OMDb client: the two share one rate limit, and when OMDb reports the day's limit both stop until it resets. The synopsis job asks only about titles the poster pass has answered. One the pass has not reached yet, or is waiting to retry, is left to the pass, so no title costs two requests. It asks first about titles a reader has been shown that OMDb has not answered for (a card's detail marks them, the way a missing picture is marked), then sweeps every other film, most voted first, which it looks for every 20 minutes. `SYNOPSIS_SWEEP_MIN_VOTES` (default 0, so every film) is a floor for that sweep; below a higher floor, a synopsis is asked for only once somebody has met the film. The job reads its queue again before every batch of 50, so a film a reader has just been shown waits behind one batch, however far the sweep has to go. When OMDb reports the day's limit the sweep pauses, keeping what it learned, and carries on from the same place once the limit resets.

TMDb's overview arrives free on the answers the TMDb jobs and the poster stand-in already save. It is kept only where OMDb has no plot, and never replaces one. It is not OMDb's answer either: a film showing TMDb's text is still asked of OMDb, and OMDb's plot replaces it. A synopsis from TMDb is asked of OMDb again after 150 days, which TMDb's terms ask of anything cached from it; if OMDb still has nothing, TMDb's text is dropped, and one still there at 175 days, because that re-ask keeps failing, is deleted all the same. A synopsis from OMDb is kept. `meta.synopses` says which source each row came from and when OMDb last answered (`omdb_at`), and a null overview means nobody has one.

A film's trailer is a YouTube video TMDb lists for it (`/movie/{id}/videos`): trailers before teasers, then the studio's own, then English, then the newest. Each candidate is checked with YouTube's oEmbed, which needs no key and answers 400, 401, 403 or 404 for a video that may not be embedded; the first that passes is the trailer. Those checks have their own limiter, 5 a second, apart from TMDb's. The answer, a key or "none", is kept in `meta.trailers`. The trailer job is the only thing that asks; the endpoint only reads, so no reader waits on TMDb or YouTube. A film opened before the job has reached it is marked wanted, which wakes the job within a couple of seconds and puts the film first, and the page asks again while the answer is pending. After the wanted films the job sweeps every film the id matcher would match with at least `TRAILER_SWEEP_MIN_VOTES` votes (default 0, so every film), most voted first. A film whose TMDb id is known is asked for its clips, one TMDb has no movie for is "none" without a request, and one nothing has matched yet is matched by the job itself and the match kept. It then asks again about a "none" for a film that came out in the last twelve months once that answer is a week old, since trailers are often added after release, and last about any answer older than 150 days. An answer that has come due is still served while it waits, and a reader opening its film moves it to the front. An answer still there at 175 days, because its re-ask keeps failing, TMDb has no movie for its film, its film has left the catalog, or there are no TMDb credentials to ask with, is deleted, and the sweep, or the next reader to open the film, asks again. Without TMDb credentials nothing is looked up, and a film with no stored answer is "none".

The colour job needs no credentials. It downloads posters that are already public and stores what they average to, as `#rrggbb`, for the frames on the opening screen. It colours the pool, not the eight somebody happened to see: the next visit draws a different eight.

### People's photos

A person's photo comes from TMDb, which maps an IMDb name id to its own person (`/find/{nconst}?external_source=imdb_id`). Only the path of the photo is kept, in `meta.people`, and the page loads the picture from TMDb's image host at 185 pixels wide, and at 342 for the bigger photo a chip or a preview face shows, the way it loads a backup poster. "No photo" is an answer too: TMDb has the person but no photo, has no person for the id, or marks the person adult. The people job is the only thing that asks; a map and `GET /people/photos` only read, so no reader waits on TMDb. Opening a map marks the people on it with no answer, or one that has come due, which wakes the job within a couple of seconds and puts them first. A person already waiting keeps a mark less than a minute old, so a map opened again straight away wakes nothing. An older mark is renewed, and that wakes the job and puts the person first again, so a lookup that failed is tried again the next time somebody opens their map, not once the job's pass is over, which during the sweep is days away. A person with an answer, "none" included, is never marked until it comes due. Reading a map's cards marks nobody.

After the wanted people the job sweeps everyone a map can show: everyone billed as cast or credited as director on a movie a map holds, whose best known such movie has at least `PEOPLE_SWEEP_MIN_VOTES` votes (default 0, so everyone), most voted first. That is over a million people, so the sweep keeps a pace of its own, `PEOPLE_SWEEP_RATE` lookups a second (default 5), on top of the process's TMDb limiter, and leaves the rest of that budget for what readers are waiting on: the people on a map somebody has opened skip that pace, and a mark that arrives while the sweep waits on it is served at once. At 5 a second the first sweep takes about three days. Last, the job asks again about any answer older than 150 days, at the sweep's pace. An answer that has come due is still shown while it waits; one still there at 175 days, because its re-ask keeps failing, its person has left the catalog, or there are no TMDb credentials to ask with, is deleted, and the sweep, or the next map with that person on it, asks again. Without TMDb credentials nothing is looked up, and nobody without a stored answer has a photo.

`cmd/importer -posters-only` runs the OMDb pass and then the TMDb poster pass, and does not import. The synopsis, trailer and people jobs run only in the long-running runner.

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

## Where to watch

The services that carry a movie come from the [Streaming Availability API](https://www.movieofthenight.com/about/api), by Movie of the Night (v4, `https://api.movieofthenight.com/v4`, the key in an `X-API-Key` header). It takes IMDb ids, so a map's `tconst` is asked about as it is: `GET /shows/{tconst}?country={cc}`. Only the server asks, and the key never reaches the browser. `internal/streaming` shapes the answer into four lists:

- **stream** holds `subscription` and `addon` options. An add-on keeps its own name and logo and gains `via`, the service it is watched through.
- Each list has one entry per service. The API lists a service once for every quality it carries.
- **rent** and **buy** keep a service's lowest price, and the link that goes with it. Prices in two currencies are not compared.
- Everything stays in the order the API gave it.
- An option whose `expiresOn` has passed is left out, since the API can go on listing one for a while after it has gone.

A show the API does not know (404) is on nothing, an answer like an empty list.

### Kept, and asked once

The API is metered per request, so its answers are kept in `meta.where_to_watch`, one row per movie and country, shaped, empty answers included. A failed ask is never kept: the reader is answered 502 and the page leaves the section out, and the next reader asks again. Only movies readers open are asked about. There is no sweep, and a map never asks about its cards.

- **The first ask** for a movie and country happens in the reader's request, with five seconds to answer. Readers who arrive while it is in flight wait on the same ask. It is not cancelled when the reader who started it leaves, so the others still get the answer.
- **A kept answer** is served as it stands, and kept until something changes it: the changes feed says the movie changed in that country (below), one of its options leaves, or it is 30 days old. A movie is not asked about again just because its answer is a day old.
- **Options that are leaving** carry an `expiresOn`. When an answer is kept, a job is scheduled, in the same transaction, for the moment the first of them leaves (`expires_at`). It asks again, keeps the new answer, and schedules the next. So an answer stops offering a service the moment the movie leaves it.
- **An answer 30 days old** is still served, and a refresh is queued behind it. It is only a safety net, for a change the feed missed or one from before the feed's 31 days.
- **A movie the catalog does not hold** is 404, and never asked about.

Requests go through one limiter per process, `STREAMING_RATE_PER_SEC` (5 a second by default), and are tried up to three times on 429, 5xx or a failed connection, with `Retry-After` honoured, unless the wait would outlast the caller's deadline (a reader's five seconds). A refused key (401 or 403) is its own error: it is not retried, it is logged, and a refresh job that meets it is cancelled rather than tried again with the same key. A redirect is not followed, since the key would go with it to whatever host it names.

The covered countries, and the names the page's sentence uses, come from `GET /countries` and are kept in `meta.streaming_countries`. The first reader to need the list fills it. A daily job asks for it again once it is a week old; a list that comes back empty never replaces the kept one. A reader in a country the list does not have is answered `{"country":"ng","covered":false}` without the API being asked.

### Kept right by the changes feed

The API's `GET /changes` lists what changed, one country at a time: a service that started carrying a movie (`new`), stopped (`removed`), or changed how it carries it (`updated`). Once a day the `streaming_changes` job reads those three, for movies (`item_type=show`, `show_type=movie`), oldest first, in every covered country somebody has an answer kept for. A country nobody has kept an answer for is never asked about, and neither is one the API does not cover.

- **The window** starts where that country's last run got to, kept in `meta.streaming_sync` (`synced_to`), and ends now. A country's first run starts a day back, or at its oldest kept answer when that is older. The feed goes back 31 days and no further, so a longer gap is cut to that, and logged; the 30-day safety net catches what it changed.
- **A change to a kept movie** is matched to it by the show's `imdbId`, from the `shows` that come with each page. When the show carries its options for the country, they are shaped and written as they are, through the same keep as every other answer, which schedules the job for the first that leaves: no request beyond the page. When it carries none for the country, a refresh is queued, which asks once. A show without options is never taken for a movie on nothing. A movie changed several times in a run is handled once. A change to anything not kept for that country is passed over.
- **The cost** is the size of each country's changes, not of what is kept: 25 changes a page, each page a request, kept or not. So it is at least three requests a day per country with kept answers, one more for every 25 changes, and one for each refresh queued. `STREAMING_CHANGES_MAX_PAGES` (40 by default) caps the pages one country's run reads, across the three feeds, which are read a page at a time in turn so the cap never starves the last. A run the cap stops records how far it got, the earliest any unfinished feed reached, and the next carries on from there; a run whose every page fell inside one second moves past it rather than read the same pages again. Each country's run logs its pages, the changes on them, the answers written and the refreshes queued. A capped run is a warning, and so is one with changes naming a show the page did not carry, or a page that says there are more and gives no cursor to ask for them.
- **A page that fails** (a 429 or 5xx after the client's tries, or no connection) ends that country's run. What every feed got past before it is recorded, and the rest of the window waits for that country's next daily run. That is so even when they got past nothing, once a page was read, so a page that keeps failing costs the pages before it once a day rather than on every try. When no page was read, nothing is recorded: River tries the job twice more within seconds (each try up to three requests), and after that the country is read again at the next hourly look. A refused key cancels the job, since every country would meet it.

The job looks every hour, which is a read of the database, and reads a country once its last run is a day old (`updated_at`). That way a deploy restarting the queue neither reads the changes early nor puts them off.

### The country

The page never sends a country. The server works it out, lowercased:

1. The geo header named by `GEO_COUNTRY_HEADER`, the one a CDN put in front of the app sets on every request (for example `CF-IPCountry`). Cloudflare's `XX` (unknown) and `T1` (Tor) do not count. Unset, the default, no header is read: Railway's edge sets none, so one arriving there was written by the reader, who could name any covered country with it and have the metered API asked on its behalf.
2. Otherwise the client's address, from `X-Real-IP` (which Railway's edge sets), then the last `X-Forwarded-For` entry (the one the nearest proxy appended; the first is the client's to write and is never read), then the connection, looked up in MaxMind's GeoLite2 Country. A network with no country falls back to where it is registered.
3. A loopback, private or otherwise unroutable address, or one the database cannot place, counts as a country without coverage: `{"country":"xx","covered":false}`. So does everyone when there is no MaxMind license key, unless the trusted header placed them. That is also why a local development server, reached over loopback, always says there is no coverage. With a license key set but no build loaded yet (the first deploy with one, until its download is in, or a second process until it picks that download up), a reader no header placed is answered 503 instead, and the page leaves the section out: telling them their country has no coverage would be untrue, and their browser would keep it for an hour.

The service downloads GeoLite2 Country itself. A job asks MaxMind with a `HEAD` twice a day, and when the process becomes the queue's leader. A `HEAD` does not count against MaxMind's download limits. Only a `Last-Modified` different from the kept build's leads to a `GET`, from the permalink with `MAXMIND_ACCOUNT_ID` and `MAXMIND_LICENSE_KEY` as Basic auth, or, without an account id, from the older address that takes the key alone. The `.mmdb` is read out of the tar.gz (at most 64 MB), opened and verified: a MaxMind database, a country edition, every node and record readable. Only then is it kept in `meta.geoip`, the database itself, and swapped in. A bad download keeps the database in use. Each process loads the kept one at start, so a restart or a second container never downloads it again, and looks at the kept build's stamp every ten minutes to pick up one another process fetched. Lookups read whichever database is in use and never wait for a swap.

### The queue

The refreshes, the changes feed, the GeoIP check and the country list are jobs on [River](https://riverqueue.com), run inside the API process whenever `STREAMING_API_KEY` is set, embedded importer or not. Its tables live in a `river` schema of their own, which the daily swap never touches, migrated at start under an advisory lock (`0x63696e72`) so two containers starting together take turns. It has its own pool of four connections: River holds one for as long as it runs to `LISTEN` for new jobs, and that should be neither a reader's connection nor one a bulk `COPY` is holding. The jobs themselves write through the reader pool, a row at a time.

| Job | When | What it does |
| --- | --- | --- |
| `where_to_watch_refresh` | at an option's `expiresOn`, or as soon as possible for an answer 30 days old or one the changes feed said changed without carrying its options | asks the API again, keeps the answer, schedules the next leaving option. Skipped when an answer was kept after it fell due. Up to five tries |
| `streaming_changes` | every hour, and when a process becomes leader | reads the changes feed for each covered country with kept answers whose last run is a day old, writes or refreshes the kept answers it names, and records how far it got. Up to three tries; a refused key is not retried |
| `geoip_check` | every 12 hours, and when a process becomes leader | `HEAD`, and a `GET` only for a new build. Up to three tries; a refused key is not retried |
| `streaming_countries` | every 24 hours, and when a process becomes leader | asks for the country list once the kept one is a week old. Up to three tries |

Two containers during a deploy's overlap never double a download or a refresh. Each job is claimed by one worker (`FOR UPDATE SKIP LOCKED`). A refresh is unique by its arguments (movie, country, and the moment it is for) among the jobs not yet finished, so readers finding the same old answer, or both containers scheduling the same expiry, insert it once, while an expiry's job and a stale refresh never stand in for each other. The periodic jobs are inserted only by River's elected leader, and are unique among unfinished jobs, so a new leader's run-on-start cannot stack a second check on one already queued or running, and two containers never read the same changes. On shutdown River stops fetching, gives running jobs five seconds, then cancels them, inside the ten seconds a draining container has; a job cut short is retried by whichever container runs the queue next. River logs only its warnings and errors, and it reports a failed job below that, so each job logs its own failure: a refused key, Movie of the Night's or MaxMind's, as an error, anything else as a warning. A job that panics is logged as an error too.

## Routes

Catalog routes, as the process sees them. In the browser they are the same paths under `/api`. Every catalog response is `Cache-Control: no-store`, except a where-to-watch answer. A handler error is `{"error": "…"}`. Each API request has a 40 second deadline.

| Route | What it does |
| --- | --- |
| `GET /healthz` | Pings Postgres and answers 503 if it cannot. `{"status":"ok","postgres":"ok"}` when it can. A stale catalog still passes |
| `GET /analytics-config` | `{"token","host"}`. Both empty unless `APP_ENV=production` |
| `GET /search/movies?q=matrix` | Up to ten movies. `q` must be at least two characters, or 400. 503 while the catalog has never been published. Body is `{"results":[{id,title,year,poster?,c?}]}` |
| `GET /` | The cold screen. One film per era that has a live poster, a different set each visit. A database error here is an empty list and a log line, not an error page: an empty opening is better than a failure on the way in |
| `GET /grid/{tconst}` | The whole map: anchor, people, spine, the genres legend, and `og_v`. The people on it with no photo answer, or one that has come due, are queued for the people job. 400 if the id is not `tt` plus digits. 404 if the catalog has no such title, or the title has nobody billed. 503 if the catalog is not published yet |
| `GET /grid/{tconst}/films?ids=tt1,tt2` | What the mounted cards say. `ids` is required, comma-separated, at most 200, each a `tconst`. Cards with no poster are queued for the TMDb job, and cards no source has been asked about for a synopsis are queued for the synopsis job |
| `GET /posters/{tconst}` | `{"poster":"<url>"}` when a picture the browser could not load has a TMDb stand-in. The stand-in is written back, so the next read does not ask again. 404 when there is nothing, or TMDb is not configured. 502 when the lookup failed |
| `GET /people/photos?ids=nm1,nm2` | `{"photos":{"nm0000206":"https://image.tmdb.org/t/p/w185/….jpg","nm0000401":null},"pending":["nm0000401"]}`: each person's photo, or `null` for none. `ids` is required, comma-separated, at most 50, each an IMDb name id, or 400. Only ever read from `meta.people`; it never asks TMDb. A person the people job has not answered yet is marked for it and listed in `pending`: ask again in a few seconds. A stored answer that has come due (past 150 days) is served as it is and marked the same way. A person the catalog does not hold is `null`, and so is every unanswered person when there are no TMDb credentials, with nothing pending. Never cached. 500 if it could not be read |
| `GET /trailers/{tconst}` | `{"key":"vKQi3bBA1y8"}`, the YouTube id of the film's trailer, or `{"key":null}` when there is none that can be embedded. Only ever read from `meta.trailers`; it never asks TMDb or YouTube. A film the trailer job has not answered yet is marked for it and answered `{"key":null,"pending":true}`: ask again in a few seconds. A stored answer that has come due (a recent film's week-old null, or anything past 150 days) is served as it is and marked the same way. A title the catalog does not hold is `{"key":null}`, and so is every unanswered film when there are no TMDb credentials. Never cached. 400 if the id is not a `tconst`, 500 if it could not be read |
| `GET /where-to-watch/{tconst}` | Where the movie can be watched in the reader's country, which the server works out from the request: `{"country":"us","countryName":"United States","covered":true,"stream":[…],"free":[],"rent":[…],"buy":[]}`. Each entry is `{id,name,link,logo:{dark,light},price?,via?}`: `price` on rent and buy (`"3.99 USD"`), `via` on an add-on (`"Prime Video"`). A reader who cannot be placed, or whose country has no coverage, is `{"country":"xx","covered":false}` (their own code when they were placed), and the API is not asked. Served from `meta.where_to_watch`; a movie and country nobody has asked about is asked once, in the request. `Cache-Control: private, max-age=3600` on an answer, `no-store` on an error. 400 if the id is not a `tconst`, 404 if the catalog does not hold it, 500 if it could not be read, 502 if the API could not answer (nothing is kept), 503 when there is no `STREAMING_API_KEY`, or while GeoLite2 is still being fetched. The API's own error never reaches the browser |
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
    "synopsis": "When a beautiful stranger leads computer hacker Neo to a forbidding underworld, he discovers the shocking truth…",
    "genres": ["Action", "Sci-Fi"],
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
      "order": 1,
      "photo": "https://image.tmdb.org/t/p/w185/….jpg"
    }
  ],
  "films": [
    ["tt0133093", 1999, 8.7, 331, [0, 1], 65537],
    ["tt2911666", 2014, 8.1, 1016, [1], 262177]
  ],
  "genres": [
    "Action", "Adventure", "Animation", "Biography", "Comedy", "Crime", "Drama",
    "Family", "Fantasy", "Film-Noir", "History", "Horror", "Music", "Musical",
    "Mystery", "Romance", "Sci-Fi", "Sport", "Thriller", "War", "Western"
  ],
  "og_v": "a1b2c3d4"
}
```

`photo` on a person is their photo from TMDb, at 185 pixels wide, present only when the people job has one to show. It is absent both before the job has answered and when TMDb has none; `GET /people/photos` tells the two apart.

`films[n]` is `[id, year, rating or null, MMDD, people indexes, genre bits]`. `rating` is IMDb's average, one decimal, over the vote count stored beside it and not sent on the spine. `md` is March 31 as `331` and October 16 as `1016`. The genre bits are over `genres`, bit i for `genres[i]`: The Matrix is Action and Sci-Fi, bits 0 and 16, so `65537`.

`genres` is always the server's `FilmGenres`, 21 of IMDb's 28 genres in IMDb's order. Adult and Documentary are left out because those movies are never mapped, and Game-Show, News, Reality-TV, Talk-Show and Short because the catalog keeps only titles whose `titleType` is `movie`. The legend travels with the map so the client never keeps its own copy of the list.

`GET /grid/tt0133093/films?ids=tt0133093,tt2911666` answers `{"films":[…]}` with the same movie object the anchor uses, `people` as name ids rather than indexes, because a card looks each person up to draw a marker. `synopsis` is left out when no source has one, and `genres` when IMDb lists none.

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
| `api.ts` | `/api` client. Sends PostHog's distinct id and session id once analytics is up. `fetchTrailer` asks `/api/trailers/{id}` once per film and keeps the answer for the visit. A pending answer is asked again after about 1.5, 3, 5 and 6 seconds while the panel or preview is still open, and is never kept. `fetchPeoplePhotos` asks `/api/people/photos` about the people a map came without photos for, fifty at a time, asks again about the pending ones on the same schedule until none are or the caller goes, and keeps each photo, or its "none", for the visit. A pending answer or a failure is left out and never kept |
| `whereToWatch.ts` | `useWhereToWatch(imdbId)` asks `/api/where-to-watch/{id}`, at most once at a time per movie, and keeps the answer for the visit. A failure stands for two seconds, so a preview opening just after the ask made as the pointer came to rest does not ask again, and a later open does. The preview's request starts when the pointer begins resting on a card, so the answer is usually in by the time the preview opens |
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
| `OMDB_API_KEY` | | Posters, release dates and synopses |
| `OMDB_BACKFILL_RATE` | 500 | OMDb requests a second. This bounds open sockets and how fast rows arrive, not a quota |
| `POSTER_WORKERS` | 256 | Lookups in flight. A round trip is about 400ms, so 256 in flight is about 600/s: enough that the rate stays in charge. Writes are batched, 500 titles to a statement |
| `TMDB_API_KEY`, `TMDB_ACCESS_TOKEN` | | Poster fallback, id matching, empty-search fallback, trailers, people's photos |
| `TMDB_RATE_PER_SEC` | 20 | TMDb requests a second for the whole process: every client and job waits on one limiter, burst 5. At least 1 |
| `TMDB_SWEEP_MIN_VOTES` | 100 | Vote floor for fetching a poster nobody has asked for yet. `0` sweeps every title with no picture. An explicit 0 is kept |
| `SYNOPSIS_SWEEP_MIN_VOTES` | 0 | Vote floor for asking OMDb for a synopsis nobody has met yet. `0` sweeps every film. Below a higher floor, only films a reader has been shown are asked about |
| `TRAILER_SWEEP_MIN_VOTES` | 0 | Vote floor for the trailer job's sweep. `0` sweeps every film. Below a higher floor a film is looked up only once a reader opens it, and that reader waits on the job |
| `PEOPLE_SWEEP_MIN_VOTES` | 0 | Vote floor for the people job's sweep, on each person's best known movie a map can show. `0` sweeps everyone. Below a higher floor a person is looked up only once a map with them on it is opened |
| `PEOPLE_SWEEP_RATE` | 5 | Lookups a second for the people job's sweep and re-asks, inside `TMDB_RATE_PER_SEC`. The people on a map a reader opens skip it. Must be more than 0 |
| `STREAMING_API_KEY` | | The Streaming Availability API, from Movie of the Night. Turns on where to watch and the queue that keeps it right. Unset, the route answers 503 and the page leaves the section out |
| `STREAMING_RATE_PER_SEC` | 5 | Requests a second to that API, for the process. It is metered per request; what keeps the bill down is that answers are kept. Must be more than 0 |
| `STREAMING_CHANGES_MAX_PAGES` | 40 | The most pages of the changes feed the daily changes job reads for one country in one run, 25 changes and one metered request a page. A run it stops carries on from there the next day. At least one page of each of the three kinds of change is always read. Must be more than 0 |
| `MAXMIND_LICENSE_KEY` | | Downloads GeoLite2 Country, which places a reader's address in a country. Unset, only a CDN's country header places anyone |
| `MAXMIND_ACCOUNT_ID` | | The account the license key belongs to. Set, the database comes from MaxMind's permalink with both; unset, from the older address that takes the key alone. Not a secret |
| `GEO_COUNTRY_HEADER` | | The one country header to trust, set by a CDN in front of the app (such as `CF-IPCountry`). Unset trusts none, which is right on Railway alone |
| `TMDB_CACHE_TTL` | `168h` | Redis TTL for TMDb search responses, when `REDIS_URL` is set. Without it, those responses are not cached |
| `REDIS_URL` | | Optional cache for that fallback only, prefix `cinedikt:tmdb` |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | | Job notifications, described below. Both empty, and nothing is sent. The token is from BotFather. The chat id is `message.chat.id` from `getUpdates` after Start — positive for a private chat, negative for a group. In a group the bot needs permission to pin messages |
| `NOTIFY_TIMEZONE` | UTC | The zone notification times are written in, such as `Africa/Lagos`. Unset or unknown, they are UTC and the board says so |
| `WEB_DIR` | | Built frontend. The image sets `/app/web/dist` |
| `POSTHOG_PROJECT_TOKEN`, `POSTHOG_HOST` | host `https://us.i.posthog.com` | Read only in production. The API serves them to the page; the token is a write-only key. The page may instead be built with `VITE_POSTHOG_PROJECT_TOKEN` and `VITE_POSTHOG_HOST` |
| `MIXPANEL_PROJECT_TOKEN` | | Read only in production. The API serves it to the page, which then loads Mixpanel with autocapture and session recording (text and inputs masked); unset, the page never loads it. The page may instead be built with `VITE_MIXPANEL_PROJECT_TOKEN` |
| `NEO4J_*`, `CRAWL_THRESHOLD_BASE`, `CRAWL_ORDER_PENALTY`, `MAX_COLD_CRAWLS` | | The old map. Ignored while `DATABASE_URL` is set |

If `DATABASE_URL` is empty, startup requires a TMDb credential and `NEO4J_PASSWORD`. A deployment with nothing but a database URL starts.

### Telegram

The process that runs the catalog jobs keeps one pinned message in the chat: a headline that says whether anything needs you, then one line for each of the nine jobs (Catalog, Posters, Backup posters, Search matching, Synopses, Trailers, People photos, Opening colours, Country lookup), and a footer with the next catalog check and the deploy. The Country lookup line names the GeoLite2 build in use and when the twice-daily check last found it still the newest, or says it is waiting for the first download, or off with no MaxMind key or with where to watch off (no `STREAMING_API_KEY`, or a queue that would not start), since the check runs on where to watch's queue. It is edited in place, at most once a minute while something is running and at least once an hour, and an edit never makes a sound. A next check already in the past, or an "updated" time more than an hour old, means the process is stuck.

A new message is sent only when there is news, and its second line says whether anything is needed of you, so a lock screen shows the answer before the explanation. Good news is quiet: a new catalog going live, a new GeoLite2 build downloaded and in use (the first one a database gets says so), a failure that is over, a pass that ran for half an hour or more. It lands in the notification list without a sound. A sound is kept for what has lasted or needs you: a failure that has repeated for about an hour (at once for a refused API key, at the second attempt for an error of no known kind), the database unreachable for two minutes, the catalog at 72 hours old (and at 36 while its updates are failing; late only because IMDb has published nothing, it is said at 36 without a sound), and a reminder for anything still failing a day later. The opening colours are cosmetic and never make a sound. Each is said once. What has been said lives in `meta.notify`, so a deploy does not repeat it. Only the process that holds the jobs writes the board: a container waiting for the lease never touches it, and one that loses the lease stops, except to say the database is unreachable while nobody can hold it.

The GeoLite2 check runs on the queue, in whichever container claims it, so it can report before its container holds the jobs, which is when a new deploy's first check runs. What it says is kept and said once that container takes the jobs, by it alone: a new build is said only by the check that downloaded it, never by a container that loads it afterwards. A check counts as one failure however many times River retried it, so MaxMind being down is said at the second failed check, half a day on; a refused MaxMind key is said at once, like OMDb's or TMDb's. With `EMBEDDED_IMPORTER=false` the API's queue has nowhere to report, and the board, written by `cmd/importer`, shows the country lookup as off.

A one-off `cmd/importer -once` or `-posters-only` sends one quiet summary when it ends and leaves the board alone.

## Credits

This product uses the TMDB API but is not endorsed or certified by TMDB. Backup posters, the list of trailers, people's photos and the fallback synopses come from [TMDB](https://www.themoviedb.org).

Synopses, posters and release dates come from [OMDb](https://www.omdbapi.com/), whose data is licensed [CC BY-NC 4.0](https://creativecommons.org/licenses/by-nc/4.0/).

Trailers play from YouTube.

Streaming availability by Movie of the Night.

This product includes GeoLite2 Data created by MaxMind, available from [https://www.maxmind.com](https://www.maxmind.com).

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

Variables to set on the service: `DATABASE_URL`, and, for pictures, synopses, trailers, people's photos and the search fallback, `OMDB_API_KEY` and one of the TMDb credentials. `POSTHOG_PROJECT_TOKEN` and `MIXPANEL_PROJECT_TOKEN` if analytics should report. `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` if the jobs should report to a chat, with `NOTIFY_TIMEZONE` so its times are local. `STREAMING_API_KEY`, `MAXMIND_LICENSE_KEY` and `MAXMIND_ACCOUNT_ID` for where to watch. `WEB_DIR` only if it should differ from the path the image already sets.

## Tests

```bash
go test ./...
cd web && npm test
```

The catalog tests need a database of their own. They publish fixtures over `catalog`, empty `meta.posters`, `meta.where_to_watch`, `meta.streaming_sync` and `meta.geoip`, and clear River's jobs, so pointing them at a working catalog destroys it. Nothing in the suite talks to the Streaming Availability API or MaxMind: both are stood in for, and the GeoLite2 databases the tests use are built by the tests.

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
