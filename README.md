# cinedikt

Start from a movie and see every movie its cast and directors made, arranged by release year and IMDb rating.

The searched film sits on a grid. **Y is the year. X is the rating**, low on the left and high on the right, on a scale that does not change from map to map. There are no edges. Who connects a card to the film you searched is said on the card and in the sheet that opens from it.

```
IMDb datasets --> catalog jobs --> Postgres --> Go API --> React grid (web/)
                       |                ^           |
                       |                |           +-- Streaming Availability, GeoLite2   where to watch
                       +-- OMDb / TMDb -+   posters, dates, synopses, trailers, photos, colours, id matches
```

The catalog jobs run inside the API's own process, beside the server. A reader's request never calls TMDb to build the map, and never writes the catalog's own tables: what a request does write, such as a mark that a card was drawn without a picture, a poster stand-in or a share card, goes into `meta`, or is a refresh queued on River. Pictures, full release dates, synopses, trailers, people's photos, poster colours, and the IMDb-to-TMDb id map are filled in beside the catalog, into a `meta` schema that survives every daily swap. The map is computed live from the tables, so a poster learned an hour ago is on the next read. Where to watch also calls out from a reader's request, as the poster stand-in and the empty-search fallback do: a movie and country nobody has asked about is asked of the Streaming Availability API once, and the answer is kept.

Cinedikt Daily, at `/daily`, is a puzzle made from the same maps: one hidden movie a day, with its map as the board, each reader playing the one for their own date. A catalog job picks each day's movie and deals its board more than a week ahead, copying into `meta` everything a game needs, and the game is played against the API, which checks every move and records it there. It is the one thing a reader writes that is theirs: a player, named by the server and known only by a cookie, and their games.

The whole movie catalog is a local copy of the [IMDb non-commercial datasets](https://developer.imdb.com/non-commercial-datasets/). Those five files are the source. Postgres is the only dependency the app needs.

## What you see

The page title is **Cinedikt — a movie’s cast and directors, and everything they made**. A map's tab is `{title} — everything its cast and directors made · Cinedikt`, the About page's is **About · Cinedikt**, and the Daily's is **Daily · Cinedikt**.

### Opening

`/` is a cold screen: **Start with a movie you love**, and under it, **See every movie its cast and directors made, arranged by year and rating.** Eight poster tiles, one from each era, and a different eight every visit. A short window shows fewer, as whole rows, so the last row is not cut off: two columns below 640px, all eight in one row on a landscape phone, four otherwise, never more than eight. Tiles without a picture or a year are dropped. A long title is kept and ellipsised.

The mark draws itself, then glides into the wordmark, unless the reader prefers reduced motion. Each tile is filled with the poster's average colour (`#rrggbb`) while the picture is still arriving, so a film shows its own colour before it shows itself. Until the server has worked that colour out, the client paints one derived from the title.

If the suggestions cannot be loaded, the screen says so and leaves the search.

Above the headline is the Daily's banner: three cards fanned with a bobbing **?** on top, the **Daily** pill, today's number and day (**No. 142 · Thursday 8 October**, the number alone on a phone), **Whose map is it?**, and a button whose shine crosses it every few seconds. Before the reader has played it says **Play**, with their place this week beside it, **1,204th this week**, or **61,240 playing today** when they have none (nothing while nobody has); during a game, **Keep going** and **640 points left**; after it, **Results** and **640 points today** or **Missed today**. The place is the one the Daily's title screen shows (see Daily), over the days of the week before today, since today has not been played yet. Neither opening screen shows a leaderboard: today's would be strangers the reader cannot be on yet, and it would start them off behind. The line beside the button and the day are left off a phone. The banner's name for a screen reader carries every word it can show, the button's included, so a reader who says "click Results" to their speech software is understood. The whole banner is one link to `/daily`, pushed as a new history entry as **About** is, and a click with a modifier key is left to the browser. Its box is drawn from the first paint, empty, because the tiles are counted under it: a banner that arrived late would push the last row off the bottom. It is filled once `GET /api/daily` and `GET /api/daily/me` have both answered, asked together, so the line beside the button is settled once rather than changing a moment after it is drawn. A place that cannot be had is no place, and the player count stands in. If the puzzle fails, or today's has not been picked yet, the banner goes and the tiles are measured again without it. The screen asks on every visit, since the reader's game moves on between them, and neither request writes anything. Today is the reader's own date, so a screen left open over their midnight asks again then, keeping what it shows until the server replies, and moves on to the new map.

The theme choice sits under the tiles, and under that, on one row, **About** and **hello@cinedikt.com**. The credits the data's sources ask for are on the About page, not here.

### About

`/about` (or `/about/`) says what Cinedikt is, then credits each source of its data with the notice it asks for, word for word: IMDb, OMDb, TMDB (its logo, smaller than Cinedikt's mark, white in the dark theme and in its own gradient in the light one), the Streaming Availability API by Movie of the Night, and MaxMind's GeoLite. Each source links to its site, and OMDb's notice to its CC BY-NC 4.0 licence, in a new tab. The opening screen's **About** link pushes it as a new history entry; a click with a modifier key is left to the browser. The header is whole from the first paint, as on a map, and the search works as on the opening screen: while the picked movie's map loads, the page dims behind the progress line. The intro, the credits and the footer fade up in turn, unless the reader prefers reduced motion.

### Search

The header search is **Search a movie**, or the current film's title once a map is open. While a map loads it names the film on its way, when the app was told which one (a search pick, a tile, a card); after Back, Forward or a reload it does not know, and says **Search a movie**. ⌘K or Ctrl+K from anywhere, or **/** when not typing in a field, puts you in it, except while a film sheet or the View panel is open. A **⌘K** hint shows in the empty field on anything wider than a phone. Two characters is the shortest query worth asking; shorter, and the field does not ask. Results arrive after 250ms of quiet, and the list stays open while the query is two characters or more, even once the field has lost the focus. Arrow keys move the highlight, Enter picks it, Escape clears the field and leaves it. The field is a combobox, so a screen reader hears the highlighted film as the arrows move. Ten films come back, each with a poster and a year. **Searching…** shows while the first answer is on its way, and **No movies match “…”** is the empty answer.

The Daily's header has no search field, and so none of these shortcuts: a search that opened a map would walk out of a game whose clock runs on. There, **/** goes to the Daily's own guess field.

### The map

`/movie/tt0133093-the-matrix` is one map. The id is IMDb's `tconst`. The slug is there so a pasted link says what it opens; an id alone is a valid route, and a stale slug still opens the same movie. There is no `?movie=` any more: that parameter carried a TMDb id, and a TMDb id is not an address in this catalog.

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

### Daily

`/daily` (or `/daily/`) is Cinedikt Daily: one hidden movie a day, the same one for everyone on that date, and its map as the board. Every card on the board shares an actor or director with the hidden movie. The cards sit on the map's own year rail and rating strip, and all but three of them are blank. The reader starts with 1,000 points, spends them on cards and clues, and guesses; the points left when they name it are the score. A new map arrives at the reader's own midnight, as Wordle's does.

The header keeps its row heights, and its contents change: the wordmark, whole even on a phone, which goes home; a **Daily** pill; and on the right the puzzle's number and day, **No. 142 · Thursday 8 October** (the number alone on a phone), then **How it works**. The day is the puzzle's own date, written as it stands, so the reader's zone never moves it a day either way. There is no search field and no rating floor. On a phone the header stays put above the board rather than lying over it as glass.

The page opens on an intro that is also the rules. Three cards fan out with a **?** on the middle one. Then come the **Cinedikt Daily** pill, the day, the reader's streak when they have one (**4-day streak**), and their place this week when they have one (**1,204th this week**, in the streak pill's box with a podium for the flame), in a centred row that wraps; then **Whose map is it?** and **One hidden movie. Every movie on its map shares an actor or director with it. Find it with as many of your 1,000 points left as you can.** Under that are what each thing costs (**See its year** at 200 among them), the rules in a paragraph, **Playing as {name}** with **New name**, **Play No. 142**, and **{n} people have played today. The clock starts when you press Play.** Before anyone has played, the line is only **The clock starts when you press Play.**, as the banner leaves its count out: nought would say the game is empty rather than new. New name scrambles the letters, slot-machine style, and lands on a fresh name from the server. Play waits while it spins, so the game starts under the name that lands. Play turns the three starting movies over, one after another, and starts the clock. **How it works** opens the same screen over a game, ending in **Back to the game** or **Back to your result**. It holds the focus, and as the rules Escape closes it; before Play there is no game behind it to go back to. A reader who comes back to a game already started lands on the board, at the last card they turned over.

The place is the reader's rank on this week's board, the one the result's **This week** tab shows them on, so the opening screens and the result never disagree. Until they have finished today's game it is over the days of the week before today, so not having played yet never counts against them; after it, today is in. With no points this week, as on a Monday before playing, there is none and nothing shows. The page asks for it with today's puzzle as it opens, so it is there in the screen's first frame rather than re-centring the row a moment later, and asks again when a game ends on the page, letting the old place go as it asks: one that cannot be had shows nothing, never a place that leaves today out. It shows on every visit to the screen, the rules over a finished game included.

A blank card is a ghost of a card in its year and at its rating, with a dot for each person the reader knows who is on it, up to five, in that person's colour. Pointing at it, on a device that hovers, shows what turning it over costs; below 1024px, or on a touch screen, every blank card shows it. A card costs 20 to 80 points, more the further right it sits, because a better-rated movie tends to be a better-known one. It turns over in 3D into a map card: poster, title, rating, and a mark for each person known. A close relative, a card sharing three or more people with the hidden movie (usually a sequel, which would give it away), turns over as **A close relative** and **3 in common**, never its title. Everyone playing sees each person in the same colour, taken from their place on the movie, directors first and then the billing, rather than from the order they turned up in.

The panel floats down the right on a desktop or a tablet, 380 wide, and is a sheet from the bottom on a phone. Along its top are the points left, which roll down as they are spent while the amount floats off, a meter that turns to the down colour below 250, the clock, and a button that folds the panel away. Under them is the feed, an entry for each move: **Start** (**Three movies from its map are showing.**), **Turned over**, **Director**, **Directors** or **Actor** with each person's face and how many of their movies are now marked, **Genres**, **Year** (**It came out in 1999. The map marks where that year sits.**), and **Not it** for a wrong guess. A movie in the feed is a chip that scrolls the board to its card and pulses it.

There are four clues to buy. **Director** (**Directors** when there are several) shows every director for 150. **Actor** shows the next of the billed cast for 150, skipping anyone a guess has already found. **Genres** shows the hidden movie's genres for 80, and **Year** marks its year on the map for 200. They sit in two columns, in that order. A clue bought says **Seen**, and Director or Actor says **Known** once there is nobody left for it; either way it fades and cannot be pressed, as does one the points left cannot pay for, which keeps its price.

There is no clue made of the movie's text. No clue may give the answer away in a single search, so there is no plot, tagline, quote or character's name: any line can be pasted into a search engine. "How it starts", the first sentence of the synopsis, was the fourth clue until it did exactly that: The Matrix's names Neo and Morpheus. A director or an actor can still be searched, through their filmography, but that takes several steps, and each costs 150.

Once the year is bought, the board marks its row. If a card shares the year, that row is the one. If none does, a row opens for it, empty, between its neighbours: the height one card's lane gives a year (114 on a desktop, 96 on a phone), and where its neighbours' years now run on, as 1998, 1999 and 2000 do, the gap mark between them goes. It grows out of the seam between them as everything below slides down, on the rows' own curve, while its label fades in. Its band takes the accent wash and its label the accent, a decade's in Young Serif as ever, and every other year's band and label fade to 0.35. The cards do not: no card is the answer, whatever year it is from, so only a wrong guess fades one. Dashed lines rule both edges of the row, the full width of the board, and one pill names the year inside it; on the map's first year there is no top line and the pill sits on the bottom one, and the last year has no line under it. The rating lines the wrong guesses drew stay, and their year ranges give way to the exact year. The board scrolls smoothly to the row, centring its cards in the part the panel leaves clear, or the middle one when they are wider than that; an empty row centres at the left edge. At the end the answer fills that row, so nothing jumps. For a reader who prefers reduced motion nothing grows, fades or glides, and the scroll is instant.

The guess is **Name the movie**, a search like the header's, asked after 250ms of quiet. It takes two steps, so a slip costs nothing: choose a movie, then press **Guess**. Only movies found for exactly what is typed can be chosen. While the next answer is on its way, the last answer's rows stay, faded, and cannot be chosen, so Enter never takes a movie from words already typed past. A search that never answered says **Couldn’t reach Cinedikt. Try again.**, not **No movies match “…”**. A movie already guessed is marked **Guessed**. A wrong guess costs 100, then 50 more each time, and shakes the panel. Its entry says who the guess shares with the hidden movie, or **{title} shares no one with today’s movie.**, and where the hidden movie sits from it: **Today’s movie is older and rated higher.** The people it names become known, so their dots appear. The board narrows too: cards and years the guesses have ruled out fade, and dashed lines mark what is left, **1995 or later**, **1994 or earlier** (once the year is pinned, by a guess from the same year or by Year, a line on each edge of its row and **1995** once between them), **7.4+** and **Up to 6.9**. Under the field the hint says what the next wrong guess costs: **Click a blank card to turn it over. Your next wrong guess costs 150.**, with **Tap** on a touch screen. **Show the answer** ends the game at once, with nothing.

The game ends when the reader names the movie, runs out of points, or asks for the answer. Every card turns over, rippling out from the answer, which joins the map in its own place, ringed and tagged **Today’s movie** as a searched movie is, its year's row opening up between its neighbours, unless Year has opened it already. A solve's score rises over it, and confetti bursts from the answer, following its card while the board is still gliding to it. Then the results: **640 points** and **Solved in 3:07. You used 4 cards, 1 person, the genres and the year.**, or **No points today** with **Your points ran out.** or **You asked for the answer.** After them come the answer, which scrolls the board to it; two numbers, the share of today's players who scored less (or, without a solve, the share who got it) and the streak in days in a row, which says **Your 4-day streak ended.** when today broke one; and a card to share, whose **Copy** puts the number, the score and time, a bar of ten blocks and the address of the Daily on the clipboard. Then the leaderboard, **Today** and **This week**: the reader's row says **You**, **···** marks ranks skipped, and a week row has a cell for each day so far. A board that did not load says **The leaderboard didn’t load.** with **Try again**, and choosing a tab asks again too. Last come **Next map in 11:47:03**, the time to the reader's midnight, and **Explore the map**, which folds the results away to show the whole finished board, then **Show results** to bring them back. For a reader who prefers reduced motion none of it moves: cards turn over at once, numbers land on their values, and there is no confetti.

A refused move is a toast under the header: **Not enough points left for that card.**, **You’ve already guessed that one.**, **That map has ended.** (a move on a map whose day is over, after which the page loads the new one), or **Couldn’t reach Cinedikt. Try again.** A move that got no answer can simply be made again. When the reader's midnight comes with the page open, it loads the new map then, whatever is showing: an intro or a result simply gives way, and a game cut short says **That map has ended.** If today's puzzle has not been picked yet, the page says **Today’s map isn’t ready yet**, with Try again.

### What is remembered

How the map is drawn stays with the reader, in `localStorage` under `cinedikt.grid`: newest or oldest first, whether unrated films show, whether the searched year is highlighted. A filter does not. Who is selected, the rating floor, the year window, the genres picked, and whether empty years are hidden belong to the visit they were set on. They live on the history entry. Opening another movie — from search, from a card, from home — starts clear. Going back restores what that map had.

The theme is separate, under `cinedikt.theme`: System, Light, or Dark. It is applied before the first paint, from a script in `index.html`, so a reader who chose light does not see a dark frame and then a white one. The page background is `#f9f4ee` in light and `#13100d` in dark.

The Daily keeps nothing in the browser but its cookie. The game is the server's, so it carries on after a reload or in another tab, and another browser is another player.

### Getting around

Back, in the header, is the browser's own back. It is there on a map, the About page or the Daily when it has somewhere to go back to: not on the opening screen, and not on a page opened straight from a link. The wordmark goes home, from a map, the About page or the Daily, keeping any query string and hash. The stack records a depth, so back from a film you remapped into returns you to the map you remapped from, filters and all.

On a phone, and on a short landscape phone, the header overlays the map as glass and hides as you scroll down past the first 80px. Scrolling back up, or coming within 40px of the top, brings it back. It never hides for a scroll the app makes itself — centring a new map, Recenter, rows closing up — and it stays put while the map is loading, while the sheet or the View panel is open, and while the search is focused. The Daily's header never overlays: the page under it places its panel, its intro and its toast against its own box, which starts below the header.

### Sharing

Every map has an address, so it can be shared. The API serves `index.html` for any path it does not have a file for. At `/about` it names the page **About · Cinedikt**, in the title and `og:title`, with `og:url` the canonical `/about`, and keeps the site's own description and card. At `/daily` and `/daily/` the title is **Daily · Cinedikt**, `og:title` **Cinedikt Daily: whose map is it?**, `og:description` **One hidden movie a day. Every movie on its map shares an actor or director with it.**, and `og:url` the canonical `/daily`. The card stays the site's own: a picture of the day's map would give the answer away. It rewrites the tags a scraper reads when the path is a movie: the title, the description (**See every movie {title}’s cast and directors made, arranged by year and rating.**), the canonical URL with the current slug, and a 1200×630 PNG of that film's poster beside its title. The card is drawn in Go, in the fonts the page uses, and it is dark in both themes — it appears in somebody else's chat window, where the app's theme means nothing. The lookup is given 300ms. Not knowing the film, or being too slow, leaves the generic tags alone.

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
cmd/api/            HTTP server, the share cards, and the catalog jobs
internal/catalog/   Postgres catalog: import, search, grid, posters, and the Daily's puzzles and games
internal/api/       HTTP handlers over the catalog, and the Daily's
internal/daily/     the Daily's rules, with no database or clock: prices, the engine a game is replayed through, the pick, names, boards
internal/config/    environment
internal/omdb/      poster, release-date and plot lookups
internal/tmdb/      poster fallback, id matching, empty-search fallback
internal/trailer/   which of a movie's YouTube clips is its trailer
internal/streaming/ the Streaming Availability API, and the shape of a where-to-watch answer
internal/geoip/     GeoLite2 Country: placing an address, and keeping the database current
internal/httpretry/ the retrying GET the TMDb and streaming clients share
internal/imdbid/    the shape of IMDb's title and name ids, checked wherever one comes in
internal/notify/    job notifications
internal/telegram/  optional Telegram sink for those notifications
internal/analytics/ PostHog, production only
web/                the React grid
```

## Setup

Postgres is the only dependency. Copy `.env.example` to `.env` and set `DATABASE_URL`.

```bash
cp .env.example .env
createdb cinedikt
```

`OMDB_API_KEY` (free at [omdbapi.com](https://www.omdbapi.com/)) is what puts pictures, full dates and synopses on the cards. Without it the catalog still serves: cards have no posters, and order inside a year falls back to the title id because every month-day is 0. `TMDB_API_KEY` or `TMDB_ACCESS_TOKEN` is the second chance for pictures OMDb does not have, the thing an empty search asks, and where trailers are found. Either credential is enough; the access token is preferred when both are set.

`STREAMING_API_KEY` (from [Movie of the Night](https://www.movieofthenight.com/about/api)) turns on where to watch. `MAXMIND_LICENSE_KEY` and `MAXMIND_ACCOUNT_ID` (a free [GeoLite2](https://www.maxmind.com/en/geolite2/signup) account), set together, let the server place a reader's address in a country. Without the streaming key the section is simply left out; without both MaxMind settings every reader counts as a country without coverage.

## Run

```bash
go run ./cmd/api
```

On an empty database the API starts the import itself, behind the server. Until a generation has been published, catalog routes answer **503** with `the catalog is still being built`. The health check still answers, so a deploy is not failed for being mid-import. The first import takes about five and a half minutes: roughly two of those are the 1.35 GB download, and the rest is reading the files and building the indexes. It logs where it has got to, in four numbered steps.

Two processes on one database, such as a deploy's old and new containers, never both run the jobs: a Postgres advisory lock (`0x63696e6a`) lets only one hold them, and the other waits and retries every 30 seconds.

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

How the jobs beside the catalog are built is in `internal/catalog/README.md`. What follows is what the catalog holds and how it is kept current.

### What is kept

Five files, from `https://datasets.imdbws.com/`, and nothing else. `title.akas` and `title.episode` are out of scope. `knownForTitles` on `name.basics` is a handful of highlights rather than a filmography, so that file is read for names alone.

| File | What is kept |
| --- | --- |
| `title.basics` | rows whose `titleType` is `movie`. Nine of every ten rows is a television episode; series, shorts, videos, and TV movies go with them. About 757,000 movies remain |
| `title.principals` | `actor`, `actress`, and `director` credits on those movies, with the first character name |
| `title.crew` | the director list, in IMDb's order, including directors the principals file never billed |
| `title.ratings` | average and vote count, for kept movies. An unrated title is no row, not a zero |
| `name.basics` | a person's name, only if a kept credit named them |

Adult titles and documentaries are loaded and then left off every map, every search, and the cold-screen pool.

### What happens each hour

A generation is the five `Last-Modified` stamps, read with `HEAD`. Nothing runs until every one of them has moved past what was last published: one file arriving ahead of the others is half a generation, and half a generation on top of yesterday's rest is a catalog whose credits and titles disagree. In practice IMDb publishes all five within about a minute of each other, once a day. The hourly check is not about catching that minute. It is about not waiting most of a day after it.

When they have all moved, the files are streamed to disk and the stamps are read again. If anything moved while they were being fetched, what is on disk is a mixture and the attempt is thrown away. The client names itself `cinedikt-catalog/1 (+https://cinedikt.com)`.

The load goes into `catalog_next`, by `COPY` into unlogged tables, so forty million rows are not paying for the WAL on the way in. Readers only ever touch `catalog`. Titles are read first, and those ids are the allow-list every other file is filtered against. Names are read last, so a person is stored only if a kept credit named them.

Then the tables are set logged, the indexes are built, the cold-screen pool is filled, and the schema is analyzed. Building the indexes is a minute or two and logs nothing until it is done. An index maintained during the `COPY` would cost more than one built once over finished data. Search uses a trigram index (`pg_trgm`, installed in the `meta` schema) over the primary and the original title, and only over titles that are not adult. Credits are indexed in both directions: a map is "this film's people, then everything those people made."

Before anything is published, the load is checked. The tables must be non-empty, and at least 99% of credit rows must point at a title that was stored. Less than that means the files are probably from different generations, and the attempt is not published.

Publish is one transaction: `catalog` becomes `catalog_old`, `catalog_next` becomes `catalog`, and the generation is recorded in `meta.generation`. The old schema is not dropped there. It is left for the next run, so a reader holding a plan against it finishes against data that still exists. `lock_timeout` bounds the swap at five seconds. Failing fast leaves the previous catalog serving, which is the right way to lose. A Postgres `NOTIFY catalog_published` wakes anything waiting on the new generation.

A catalog older than 36 hours is logged every hour. On Telegram, if that is configured, it is said once per catalog at 36 hours and once more at 72 hours, and a restart does not say it again. It is deliberately not a health-check failure. A stale catalog still serves, and failing the check would turn a late upstream publish into a failed deploy.

### Posters, dates, colours, ids

The dump has no pictures and no month or day. One OMDb lookup per title stores the poster's address, the full date and the plot. The browser loads the image from Amazon. The key never leaves the server.

That job is not part of an import. A full first pass is about 27 minutes at the default 500 requests a second across 256 workers, measured nearer 466 a second. Nothing waits on it. A poster appears the moment it lands, and the films anyone would actually search are done in the first minute, because the job takes the most voted first. It picks up where it left off. `meta` is never renamed by the daily swap, so what it learns survives every future generation.

A lookup that came back empty is still an answer, and is not asked again. Only a lookup that failed is retried: after a day, and never twice in the same run. OMDb's answers are not always valid JSON: a backslash before a letter, an escaped apostrophe, a raw tab or control byte in a plot, a backslash that swallows a field's closing quote. An answer that does not decode is repaired once and read again. One that still cannot be read is an answer as well, since OMDb sends the same bytes every time: it is stored as OMDb having no picture and no synopsis, so the TMDb stand-in and TMDb's overview fill in, and it is neither asked again nor counted as a failure. An address that has been seen to 404 is `dead`. Image edges replay a miss for about five minutes and then serve the picture again, so the first 404 only keeps a film off the draw that saw it. A second, after that window, is the picture actually being gone, and it is written down so the next cold screen does not ask and the TMDb job has something to repair. A host that does not answer at all is neither: Amazon being briefly unreachable never empties the opening screen and never queues a live poster for replacement.

OMDb has a poster for about 59% of the catalog. TMDb has one for roughly three-quarters of what is left. Without TMDb credentials those movies simply have no picture. With them, TMDb is asked by four jobs and by two things a reader can do (an empty search, a poster stand-in). TMDb counts about 40 requests a second per address, not per key, so every one of those waits on one limiter per process. `TMDB_RATE_PER_SEC` is the total for the process: 20 a second by default, half of TMDb's ceiling, with a burst of 5. A rate below 1 is refused at startup. A request is tried up to four times on a 429, a 5xx or a failed connection, each try waiting on that limiter, with `Retry-After` honoured, unless the wait would outlast the caller's deadline. Refused credentials (401 or 403) are not tried again.

- **Posters.** A movie is fetched when a reader has opened one with no picture (`wanted_at`), and otherwise only when it has at least `TMDB_SWEEP_MIN_VOTES` votes. The default is 100. Around 310,000 titles have no poster and about 1,300 of them have a hundred votes, so the default sweep is about a minute, and the rest are repaired the moment somebody meets them. Set the floor to 0 to ask about every title with no picture: about 300,000 lookups, roughly four hours at 20 a second, and the same again, spread out, as those answers come due every 150 days. The well-known end of that queue yields a poster about 77% of the time. The zero-vote tail yields one about 8% of the time. TMDb's answer, a picture or "nothing", is stamped (`tmdb_at`). Once it is 150 days old it is asked again, after the wanted titles and the sweep: a picture of TMDb's, and TMDb's "nothing" for a title that still has no picture. A new picture replaces the old one in place, so no reader meets the film without one while it is refreshed. TMDb no longer having a picture takes its picture away. TMDb's "nothing" for a film whose picture from OMDb works is not asked again, since that film is not waiting on TMDb.
- **Ids.** `meta.tmdb` maps a TMDb movie id to a `tconst`, filled ahead of time. An empty catalog search asks TMDb for ids and keeps a hit only when that map already has it. The search does not ask TMDb, live, which IMDb title an id is. A row with a null id is "asked, and it is not a movie we can map." A match for a film search can still offer is asked again once it is 150 days old, after every title never asked, oldest first, and its row is replaced in place, so search and the trailer job never find it missing. Any other match waits for the backstop below.

TMDb's terms ask that anything cached from it be refreshed within six months, which is what the 150 days are for, here and for synopses, trailers and people's photos below. A re-ask can keep failing, a title or a person can leave the catalog, and a process can have no TMDb credentials to ask with, so a backstop runs every 20 minutes, whatever credentials the process has, and takes away the id matches, backup posters, release dates, trailers, people's photos and overviews from TMDb still there at 175 days. A match is deleted, and the id matcher queues its title afresh if search can still offer it. A picture of TMDb's is set back to none, a release date TMDb filled in where OMDb had none goes with it, and the stamp saying TMDb was asked is cleared, so a film left with no picture is back in the poster queue, for the sweep or the next reader who meets it. OMDb's addresses, and their `ok`, `missing` or `dead` status, are left alone.

### Synopses and trailers

A film's synopsis comes from OMDb. The poster pass asks for the full plot (`plot=full`) and keeps it with every answer it saves, so a title reached from here on costs no extra request. The titles it reached before it kept plots are asked again by the synopsis job, through the same OMDb client: the two share one rate limit, and when OMDb reports the day's limit both stop until it resets. The synopsis job asks only about titles the poster pass has answered. One the pass has not reached yet, or is waiting to retry, is left to the pass, so no title costs two requests. It asks first about titles a reader has been shown that OMDb has not answered for (a card's detail marks them, the way a missing picture is marked), then sweeps every other film, most voted first, which it looks for every 20 minutes. `SYNOPSIS_SWEEP_MIN_VOTES` (default 0, so every film) is a floor for that sweep; below a higher floor, a synopsis is asked for only once somebody has met the film. The job reads its queue again before every batch of 50, so a film a reader has just been shown waits behind one batch, however far the sweep has to go. When OMDb reports the day's limit the sweep pauses, keeping what it learned, and carries on from the same place once the limit resets.

TMDb's overview arrives free on the answers the TMDb jobs and the poster stand-in already save. It is kept only where OMDb has no plot, and never replaces one. It is not OMDb's answer either: a film showing TMDb's text is still asked of OMDb, and OMDb's plot replaces it. A synopsis from TMDb is asked of OMDb again after 150 days, which TMDb's terms ask of anything cached from it; if OMDb still has nothing, TMDb's text is dropped, and one still there at 175 days, because that re-ask keeps failing, is deleted all the same. A synopsis from OMDb is kept. `meta.synopses` says which source each row came from and when OMDb last answered (`omdb_at`), and a null overview means nobody has one.

A film's trailer is a YouTube video TMDb lists for it (`/movie/{id}/videos`): trailers before teasers, then the studio's own, then English, then the newest. Each candidate is checked with YouTube's oEmbed, which needs no key and answers 400, 401, 403 or 404 for a video that may not be embedded; the first that passes is the trailer. Those checks have their own limiter, 5 a second, apart from TMDb's. The answer, a key or "none", is kept in `meta.trailers`. The trailer job is the only thing that asks; the endpoint answers from what is kept, so no reader waits on TMDb or YouTube. A film opened before the job has reached it is marked wanted, which wakes the job within a couple of seconds and puts the film first, and the page asks again while the answer is pending. After the wanted films the job sweeps every film the id matcher would match with at least `TRAILER_SWEEP_MIN_VOTES` votes (default 0, so every film), most voted first. A film whose TMDb id is known is asked for its clips, one TMDb has no movie for is "none" without a request, and one nothing has matched yet is matched by the job itself and the match kept. It then asks again about a "none" for a film that came out in the last twelve months once that answer is a week old, since trailers are often added after release, and last about any answer older than 150 days. An answer that has come due is still served while it waits, and a reader opening its film moves it to the front. An answer still there at 175 days, because its re-ask keeps failing, TMDb has no movie for its film, its film has left the catalog, or there are no TMDb credentials to ask with, is deleted, and the sweep, or the next reader to open the film, asks again. Without TMDb credentials nothing is looked up, and a film with no stored answer is "none".

The colour job needs no credentials. It downloads posters that are already public and stores what they average to, as `#rrggbb`, for the frames on the opening screen. It colours the pool, not the eight somebody happened to see: the next visit draws a different eight.

### People's photos

A person's photo comes from TMDb, which maps an IMDb name id to its own person (`/find/{nconst}?external_source=imdb_id`). Only the path of the photo is kept, in `meta.people`, and the page loads the picture from TMDb's image host at 185 pixels wide, and at 342 for the bigger photo a chip or a preview face shows, the way it loads a backup poster. "No photo" is an answer too: TMDb has the person but no photo, has no person for the id, or marks the person adult. The people job is the only thing that asks; a map and `GET /people/photos` answer from what is kept, so no reader waits on TMDb. Opening a map marks the people on it with no answer, or one that has come due, which wakes the job within a couple of seconds and puts them first. A person already waiting keeps a mark less than a minute old, so a map opened again straight away wakes nothing. An older mark is renewed, and that wakes the job and puts the person first again, so a lookup that failed is tried again the next time somebody opens their map, not once the job's pass is over, which during the sweep is days away. A person with an answer, "none" included, is never marked until it comes due. Reading a map's cards marks nobody.

After the wanted people the job sweeps everyone a map can show: everyone billed as cast or credited as director on a movie a map holds, whose best known such movie has at least `PEOPLE_SWEEP_MIN_VOTES` votes (default 0, so everyone), most voted first. That is over a million people, so the sweep keeps a pace of its own, `PEOPLE_SWEEP_RATE` lookups a second (default 5), on top of the process's TMDb limiter, and leaves the rest of that budget for what readers are waiting on: the people on a map somebody has opened skip that pace, and a mark that arrives while the sweep waits on it is served at once. At 5 a second the first sweep takes about three days. Last, the job asks again about any answer older than 150 days, at the sweep's pace. An answer that has come due is still shown while it waits; one still there at 175 days, because its re-ask keeps failing, its person has left the catalog, or there are no TMDb credentials to ask with, is deleted, and the sweep, or the next map with that person on it, asks again. Without TMDb credentials nothing is looked up, and nobody without a stored answer has a photo.

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

1. The client's address, from `X-Real-IP` (which Railway's edge sets), then the last `X-Forwarded-For` entry (the one the nearest proxy appended; the first is the client's to write and is never read), then the connection, looked up in MaxMind's GeoLite2 Country. A network with no country falls back to where it is registered. No country header is read: Railway's edge sets none, so one arriving there was written by the reader, who could name any covered country with it and have the metered API asked on its behalf.
2. A loopback, private or otherwise unroutable address, or one the database cannot place, counts as a country without coverage: `{"country":"xx","covered":false}`. So does everyone when GeoLite2 is off: no MaxMind license key, or a key without its account id, which is said at startup. That is also why a local development server, reached over loopback, always says there is no coverage. With both set but no build loaded yet (the first deploy with them, until its download is in, or a second process until it picks that download up), a reader is answered 503 instead, and the page leaves the section out: telling them their country has no coverage would be untrue, and their browser would keep it for an hour.

The service downloads GeoLite2 Country itself. A job asks MaxMind with a `HEAD` twice a day, and when the process becomes the queue's leader. A `HEAD` does not count against MaxMind's download limits. Only a `Last-Modified` different from the kept build's leads to a `GET`, from MaxMind's permalink, with `MAXMIND_ACCOUNT_ID` and `MAXMIND_LICENSE_KEY` as Basic auth. The `.mmdb` is read out of the tar.gz (at most 64 MB), opened and verified: a MaxMind database, a country edition, every node and record readable. Only then is it kept in `meta.geoip`, the database itself, and swapped in. A bad download keeps the database in use. Each process loads the kept one at start, so a restart or a second container never downloads it again, and looks at the kept build's stamp every ten minutes to pick up one another process fetched. Lookups read whichever database is in use and never wait for a swap.

### The queue

The refreshes, the changes feed, the GeoIP check and the country list are jobs on [River](https://riverqueue.com), run inside the API process whenever `STREAMING_API_KEY` is set. Its tables live in a `river` schema of their own, which the daily swap never touches, migrated at start under an advisory lock (`0x63696e72`) so two containers starting together take turns. It has its own pool of four connections: River holds one for as long as it runs to `LISTEN` for new jobs, and that should be neither a reader's connection nor one a bulk `COPY` is holding. The jobs themselves write through the reader pool, a row at a time.

| Job | When | What it does |
| --- | --- | --- |
| `where_to_watch_refresh` | at an option's `expiresOn`, or as soon as possible for an answer 30 days old or one the changes feed said changed without carrying its options | asks the API again, keeps the answer, schedules the next leaving option. Skipped when an answer was kept after it fell due. Up to five tries |
| `streaming_changes` | every hour, and when a process becomes leader | reads the changes feed for each covered country with kept answers whose last run is a day old, writes or refreshes the kept answers it names, and records how far it got. Up to three tries; a refused key is not retried |
| `geoip_check` | every 12 hours, and when a process becomes leader | `HEAD`, and a `GET` only for a new build. Up to three tries; a refused key is not retried |
| `streaming_countries` | every 24 hours, and when a process becomes leader | asks for the country list once the kept one is a week old. Up to three tries |

Two containers during a deploy's overlap never double a download or a refresh. Each job is claimed by one worker (`FOR UPDATE SKIP LOCKED`). A refresh is unique by its arguments (movie, country, and the moment it is for) among the jobs not yet finished, so readers finding the same old answer, or both containers scheduling the same expiry, insert it once, while an expiry's job and a stale refresh never stand in for each other. The periodic jobs are inserted only by River's elected leader, and are unique among unfinished jobs, so a new leader's run-on-start cannot stack a second check on one already queued or running, and two containers never read the same changes. On shutdown River stops fetching, gives running jobs five seconds, then cancels them, inside the ten seconds a draining container has; a job cut short is retried by whichever container runs the queue next. River logs only its warnings and errors, and it reports a failed job below that, so each job logs its own failure: a refused key, Movie of the Night's or MaxMind's, as an error, anything else as a warning. A job that panics is logged as an error too.

## Daily

Cinedikt Daily's rules are `internal/daily`, which has no database and no clock of its own, so they are written once, in Go, and tested without either. The catalog's store picks the puzzles (`internal/catalog/daily.go`) and keeps the players and games (`dailygames.go`), and `internal/api/daily.go` is the routes. The page mirrors only what it needs to draw. It is always on: everything it keeps is in the Postgres the catalog already uses, and it needs no key.

### The day and the number

Each puzzle belongs to a calendar date, and each reader plays the one for their own date, which changes at their own midnight: the Wordle model. The page names its time zone, as the browser's Intl reports it (`Asia/Tokyo`), in a `tz` parameter on every Daily request, and the server works the date out from its own clock in that zone. The device's clock plays no part, so winding a phone forward opens nothing early. A zone is only a name, and a reader could claim any, but every zone's date lies between UTC−12's and UTC+14's, so claiming another moves a reader a day at most and never into a puzzle that is nobody's yet. A `tz` that is missing, empty, longer than 64 characters, `Local` (the server's own zone, never a reader's) or not a zone at all, `../../etc` included, is UTC, without a word. `time.LoadLocation` refuses a path on its own, and the zone database is embedded in the API's binary (`time/tzdata`), so a container with none of its own still knows every zone.

Two dates are current somewhere on Earth for most of every day, so two puzzles are live at once, each for the readers on its date: at 23:30 UTC on 8 October, Tokyo is already playing the 9th's puzzle and Los Angeles still the 8th's. For two hours a day, from 10:00 to 12:00 UTC, while UTC+14 has reached the next date and UTC−12 is still on the last, there are three. A date is current somewhere for about 50 hours in all, from when it starts at UTC+14 to when it ends at UTC−12.

A game is held to the zone it was started in. Play takes the zone from its request and keeps it with the game (`meta.daily_games.zone`); every move after it is checked against that zone and never the one the move names, so a tab cannot hop zones to play on past its own midnight.

A player whose unfinished game has passed midnight in its own zone, read from a zone still on that date (a reader who has flown west, or a page naming another zone), would otherwise sit on a map that can never be moved again until their own midnight. So `GET /api/daily` gives them that zone's next puzzle, as they would have had it there, once the job has picked it; until then the dead game is theirs as it was. Play and the board take that puzzle too, and Play starts the new game in the old game's zone, not the zone `tz` names. That date is always the later one, and one the reader could have had by naming that zone, so it opens nothing early. Players with no game left behind, and readers with no cookie, are unaffected.

`GET /api/daily` sends the server's `now` and the `next` midnight in the reader's zone (the game's, for a player moved on as above), and the page counts down on the server's clock, corrected by the difference between the two, so a reader whose clock is wrong still sees the next map arrive on time. `next` is worked out from the date rather than by adding a day's hours, so a day a clock change makes 23 or 25 hours long still ends at midnight; where the change skips midnight itself, as Santiago's does, the new day begins when the clock jumps. Every moment the API sends (`now`, `next`, and a game's `startedAt` and `finishedAt`) is in UTC to the millisecond, so the page reads them alike; a puzzle's `date` is just a date, `2026-10-08`, the same wherever it is read.

No. 1 is the first day of the first pass's window, UTC yesterday on a new database, and every day after it is one more. A day nothing was picked for still uses its number up, so the number always says how long the game has run. The week is a week of puzzle dates, the ISO week Monday to Sunday that a puzzle's date falls in, never of when the games were played.

### Choosing the puzzle

A job beside the catalog's, **Daily puzzles** on the Telegram board, keeps a puzzle picked for every day from UTC yesterday to eight days after UTC today, ten in all. Yesterday is for the zones west of UTC, which are still on it until noon UTC at UTC−12, and tomorrow for those east of it, which are on it from 10:00 UTC at UTC+14; the week after that is to spare. It rests an hour, and a new generation wakes it. Most passes find every day picked and stop at a read; the one after midnight UTC picks the new last day. A day already picked is never picked again, and two containers picking the same day keep whichever wrote first, so a deploy neither skips a day nor changes one. A week to spare means a late catalog, or a pick failing for days, never leaves a reader's date without a puzzle: Telegram says so long before it would.

On a new database UTC yesterday is No. 1, even when the first pass could not pick it yet: the first pass runs while posters are still arriving, and a day it misses keeps its number for a later pass to fill while the day is still in the window. Once any puzzle is kept, no day before No. 1's is ever picked, since it would be No. 0 and every number after it would move; a reader whose date comes before No. 1's is told the map is not ready until their date reaches it.

The candidates are the opening screen's pool, the 250 most-voted mappable movies of each era, so each is well known and the eras are spread. Each must also be rated and have a poster that works, which the end of the game shows. Nothing is asked of its synopsis: no clue is a line of its text (see Daily, under What you see), so a movie OMDb has no plot for is as fair an answer as any. A movie that is the answer on any day within 90 days either side is left out, a day picked ahead counting as much as one already played.

They are tried in an order shuffled by a generator seeded from the day, so a pass run again tries them in the same order. The generator is the prototype's mulberry32, seeded by the date's FNV-1a hash, rather than `math/rand`, which promises no seeded order from one Go release to the next: a puzzle picked ahead must not change under a deploy. In that order, candidates of an era the six days before have not used, and a first genre the two days before have not used, go first; then any genre; then any era. The first candidate that passes every test is the answer:

- it has a director and at least three billed cast, so the Director and Actor clues each have something to sell;
- its map has at least 40 rated movies, so the board is a search rather than a choice between a handful;
- three starting cards can be found.

The map is the app's own, `peopleOn` and `spine`: the same rules for what a map can hold (see What gets onto a map) and the same 400 cap. It is read only when a candidate is reached, and the first usually fits: on the local catalog a pass picking eight days from about two thousand candidates took 1.4 seconds, the first candidate fitting each day. The board is that map with the answer taken off, and its unrated movies with it: they have no place on the rating axis the game narrows on. Card ids, `c1` to `cN`, are dealt after another seeded shuffle, so a card's number says nothing about how well known it is.

A close relative is a card that shares three or more of the answer's people, usually a sequel, which would give the answer away. The starting cards are the three least-voted cards that are not close relatives and whose people do not overlap. Walking the cards from the fewest votes up, and taking each whose people are all still unused, gives three different ways in, none of them a movie everyone knows.

Everything a game needs is copied into `meta.daily_puzzles` when it is picked: the answer, its people in slot order (the chip row's: directors in IMDb's order, then the billed cast), its genres, every card with its title, year, rating, month and day, votes and people, the starting cards, and the era and genre the next days are mixed against. The Year clue is the answer's year, already among them; nothing of a synopsis is kept. The `opening` column "How it starts" was read from is dropped (`ALTER TABLE … DROP COLUMN IF EXISTS`), and the moves' kind check is replaced once to take `year` in place of `story`, so a database made before the change, whose `CREATE TABLE IF NOT EXISTS` changes nothing, ends up as a new one does. Nothing about a puzzle is read from the catalog again. Ratings and votes move with every import, titles are withdrawn, and a card's price is its rating, so a live read would change prices halfway through a day. Posters and people's photos are the exception, read fresh for every answer: the poster jobs mend posters, and TMDb's terms allow a photo only within 175 days of asking for it.

Nothing the job logs or reports names an answer, since whoever reads the logs or the Telegram board may want to play too. A pick is logged at info with its day, number and how many cards it has. A candidate found unfit is logged at debug by its place in the day's order, not its id, because a later catalog could make it an answer. An error from reading a candidate, which names the movie it was reading, has the id taken out ("a candidate") before it reaches the logs or Telegram.

### The server owns the game

The page sends what the reader does and draws the game the server sends back. A game is its moves, in `meta.daily_moves`, and everything else about it (the points left, which cards are face up, who is known, whether it is over) is worked out by replaying them through `internal/daily`'s engine. The columns on `meta.daily_games` (points, moves, when it finished, won, gave up, its time) are only what the boards and the streak read, written with each move.

| Move | Cost | What it does |
| --- | --- | --- |
| Turn a card over | 20 to 80 | `20 + (rating − 4.5) × 13`, rounded to the nearest five with a half away from zero, so 7.0 is 55. A well-rated movie tends to be a well-known one, and knowing it is most of the way to placing its people. A close relative costs the same as any card |
| Director | 150 | Every director not yet known |
| Actor | 150 | The next of the billed cast not yet known, so nobody a guess found is sold again |
| Genres | 80 | Once |
| Year | 200 | Once. The answer's year, which nothing the page is sent says before this or the end. Dearest, because with a rating line or two from wrong guesses it leaves a handful of places for the answer to be |
| A wrong guess | 100, then 50 more each time | Never refused for points: one that cannot be paid in full takes the game to nothing, which ends it |
| The right guess | nothing | Ends the game, won. The score is the points left |
| Show the answer | | Ends the game, and the score is nothing |

A game starts with 1,000 points. Its time runs on the server's clock from Play to the end, and cannot be paused.

`story`, the clue Year replaced, is no kind at all: a buy of it is 400 `bad`, like any kind that never was, and the moves table's check refuses one. No game ever kept one, since nothing had shipped when it went, so nothing replays an old clue; the engine takes a kind it does not know as nothing, costing nothing and showing nothing, rather than fail a replay.

What a wrong guess tells is worked out when it is made and kept with the move (`detail`), so a replay never needs the live catalog. Its people are the answer's people credited on the guessed movie, as actor, actress or director in the principals or as a director in the crew list, the credits that put a movie on a map; they become known. Its year says whether the answer is `newer`, `older` or from the `same` year, and its rating whether the answer is rated `higher`, `lower` or the `same`, in tenths. A guess with no year, or no rating, says nothing about that half. A guessed movie that is on the board turns face up, a close relative included, since the reader named it.

Every move carries a `key`, the page's `crypto.randomUUID()`, and a `seq`, the number of moves the page has seen recorded. The move is made in one transaction holding the game's row (`FOR UPDATE`), so two requests for one game take turns:

1. A key already recorded is a retry, answered with the game as it stands and never charged twice, even after the game's midnight, since the move was made in time. That is why a page whose request got no answer sends the same move again under the same key.
2. A game whose puzzle's day has ended in the zone it was started in is over, and any new move on it is 409 `day`. The check is made here, holding the row, against the moment the move is recorded at.
3. A `seq` other than the number recorded is a move made from an old point, in another tab or one left open. It is refused 409 `stale`, with the game as it stands, which the page draws without a word.
4. Anything else is checked by the engine, recorded, and the game's row brought up to date.

There is one game per player per puzzle (`UNIQUE (player, no)`), and pressing Play again returns it unchanged, clock, zone and all. Play is taken only on the puzzle for the reader's date in the zone the request names, or, for a player whose game was left behind at its own zone's midnight, on the puzzle that zone has moved on to (above), and a game already started is not handed back once its own zone has passed midnight, even to a request from a zone still on that date: it could never be moved again. A move on a puzzle number that does not exist, or whose date is no longer current anywhere, is refused before the game is even read. Each of these refusals is 409 `day`, which tells a page that slept through its midnight that its map has ended; a page awake at it has already moved on. A game unfinished at the midnight of its zone is abandoned: it scores nothing and is on no board.

### Players and names

A player is a cookie, `cd_daily`, and nothing else: no account, and no name typed in. It is 128 random bits in base64url, `HttpOnly`, `Secure`, `SameSite=Lax`, on `/`, for a year from the last visit, renewed by every `GET /api/daily` or `GET /api/daily/me` that finds its player. It is the player's only credential, so `meta.daily_players` keeps its SHA-256 and never the cookie. Reading never makes a player, so the opening screen's banner can ask on every visit; Play does. A cookie of the right shape that is nobody's is never adopted, and a new player always gets a new cookie, so no client can choose its own. Every change must say it is JSON, which a form another site posts cannot do without asking first; with `SameSite=Lax`, that is the whole defence against a move made in a reader's name. A crawler, known by its user agent, is answered 204 to any change and never becomes a player.

Making a player is the one thing limited, per address, read as where to watch reads it: ten at once, then one every six minutes, held in each process's memory and swept of addresses idle long enough to have filled again. A private window is a new player, and a script making thousands would fill the table with nobody; no household sharing an address will meet the limit. A refusal is 429 `busy`, with `Retry-After: 360`. Nothing else is limited: a game is a handful of moves, each one checked.

Names are generated, never typed: a first name from one movie character joined to a surname from another, **Trinity Kimble**. Every name a player keeps is one the server drew (below), so nobody can write anything onto a board. Checking the words alone would not do it: any first word goes with any last, and a page could pick a pair that says what neither word does. The pool is the characters credited on movies with at least 25,000 votes, adult titles left out, about fifty thousand of them. A character lends its name when it reads as one: one to three words, each a capital and then Latin letters, with an apostrophe or hyphen allowed inside and no full stop anywhere. No word may be a single letter, or lack a lower-case letter, which leaves out numerals and acronyms (II, CIA). No lower-case letter may follow an apostrophe, so a possessive such as Hud's Fiancee is out and O'Brien stays. No word may be on a list of roles, titles and articles such as Himself, Narrator, Officer, Captain, Mother, King, Boss, Chief, Coach, Cousin, Doc, Ma, Pa, El, Le and La. Nor may it be on a deny list of slurs, obscenities and hate's figureheads, which checks each part of a hyphenated word on its own, whatever its case. A character with any such word lends nothing.

Each character lends its first word as a first name and its last as a surname, once per character, so a name common among characters comes up as often as they have it, and a board reads as people rather than as the catalog's rarest words. A credit is as often a role as a name, and a stop list cannot catch every role (Militiaman, Persian Emissary); what a role's word lacks is a person who has it. So a character's first word is drawn only if it begins some credited person's name, and its last word only if it ends one. Of about thirty-five thousand characters that read as names, that keeps about 21,000 first and 16,000 last entries, some 4,000 distinct words each, and drops names only a character has, such as Korben and Furiosa, along with the roles. A draw is thrown back when both halves come from one character, the two words are the same, the result is some character's whole name or a real person's, or it runs past 18 characters, which is what fits a board's row on a phone. The real people are the actors, actresses and directors credited on the same movies, about thirty-five thousand of them. With every word drawn some person's, two of them make one ("David" and "Lynch") about one draw in ninety, and a board should never seem to list one. Only the two-word names a draw could make are kept to check against, about twelve thousand. After 80 draws the name is **Neo Kimble**.

Play keeps the name the reader brings only when this process offered it within the last 12 hours, by `GET /api/daily` or **New name**, it is still one the pool could draw, and nobody has it. Otherwise it draws a fresh one, and the page shows the name Play answers with. Offers are kept in memory, at most 50,000, swept as the join limiter is (those past 12 hours, or all of them when none are), and lost on a restart, which costs a reader only the name they were offered. `UNIQUE (name)` settles a race, and a name taken in the meantime is drawn again, up to eight times. **New name** renames a player, and for a reader who has not played only offers another name, writing nothing; it never offers the name on screen again. Each player gets a hue, 0 to 359, at random, for their avatar on the leaderboard.

The API reads the pool the first time a name is needed, characters and people in one query, in about a fifth of a second, and keeps it, about ten megabytes, for a day, reading it again behind the caller. While the catalog cannot answer, on a first start before the first import, it names players from a few dozen built-in characters, with no people to check them against, so every word they lend is drawn, and asks again a minute later. A catalog whose characters lend nothing is treated the same way, since it would name everybody **Neo Kimble**. It is not read again on a publish: only the process holding the jobs hears `catalog_published`, and a day-old pool names players just as well.

### The leaderboards

Each puzzle has two boards, both kept by its number rather than by any clock. **Today** ranks the finished games of that puzzle by points, then time: everyone who has played it, from every zone, so it stays open for about 50 hours as the zones roll through its date. The count of people who have played, on the intro and the banner, is likewise every game started on that puzzle, in every zone. **This week** ranks each player by their points over the puzzles whose dates fall in the same ISO week as this one's, Monday up to and including it, so a game counts in its puzzle's week wherever and whenever it was played; then by their total time. It shows each day's points, Monday first. A tie beyond points and time goes to whoever joined first, so ranks run on without repeating. A board shows the top five and the two either side of the reader, with a gap wherever the ranks jump, and only those rows are read.

A player is listed once they have finished three earlier puzzles, or every earlier puzzle while there have been fewer than three, so the first days' boards are not empty. A private window is a new player, and a board anyone could top on a first try would be a board of first tries. A reader not yet listed still sees their own score and rank among those who are, marked **Not on the board yet**. Today also gives the share of everyone who finished, listed or not, who kept fewer points than the reader, and the share of finished games that were won.

The streak is the run of consecutive puzzles finished with more than nothing, ending at the reader's own puzzle, the one for their date, or the one before it. A day the job missed has no puzzle to play, and breaks nobody's run. Until the reader's puzzle has scored, the streak shown is the run that ended at the one before, which theirs can still extend.

The opening screens show no board, only the reader's own standing (`GET /api/daily/me`): the streak the title screen shows, `now` once they have finished their puzzle and `before` until then, and their place on its **This week** board. The place has the board's players and its order, those listed by its own rule and the reader whether listed or not, ranked by points, then time, then who joined first, and the same SQL ranks both (`weekSQL`), so the two can never disagree. Until the reader has finished their puzzle it adds up only the puzzles of its week before it, so not having played yet never counts against them, and the players are those with a finished game on those days; once they have, it is exactly the tab the result shows them. A reader on no such board, or with nothing on it, has no place, which is every reader on a Monday before playing. Both opening screens ask on every visit, so each answer is kept a minute in the process's memory, by player, puzzle and whether they have finished it: finishing is a key nothing was kept under and is worked out at once, and anything else that moves a place, another player finishing, waits out the minute. At most 50,000 are kept, swept as the name offers are.

### Keeping the answer off the page

The page is never sent the answer, nor what a blank card is, until the game is over. At load every card is its id, year, rating, and month and day, which is where it goes and nothing more; the three starting cards are the only movies sent whole. A card turned over is sent as its movie, except a close relative, which is sent as how many people it shares until the reader names it by guessing it or the game ends. People are sent once bought or found, and the answer's year only in the Year clue's entry. The tests in `internal/daily` and `internal/api` read every response a game makes before its end, from the first read through Play, every flip and buy, a close relative's flip, wrong guesses on and off the board, a stale refusal and the reads after, and fail if any of them holds the answer's id or title, or a blank card's. They also fail if any field holds the answer's year before Year is bought, or anywhere but that entry after, other than the year of a card or movie, which says its own: the fixtures have a starting card and a guessed movie from the answer's year to prove the difference. Every Daily response is `Cache-Control: no-store`.

## Routes

The API's routes, as the process sees them. In the browser they are the same paths under `/api`. Every catalog response is `Cache-Control: no-store`, except a where-to-watch answer. A handler error is `{"error": "…"}`, and a Daily one adds a `reason` (below). Each API request has a 40 second deadline.

| Route | What it does |
| --- | --- |
| `GET /healthz` | Pings Postgres and answers 503 if it cannot. `{"status":"ok","postgres":"ok"}` when it can. A stale catalog still passes |
| `GET /search/movies?q=matrix` | Up to ten movies. `q` must be at least two characters, or 400. 503 while the catalog has never been published. Body is `{"results":[{id,title,year,poster?,c?}]}` |
| `GET /` | The cold screen. One film per era that has a live poster, a different set each visit. A database error here is an empty list and a log line, not an error page: an empty opening is better than a failure on the way in |
| `GET /grid/{tconst}` | The whole map: anchor, people, spine, the genres legend, and `og_v`. The people on it with no photo answer, or one that has come due, are queued for the people job. 400 if the id is not `tt` plus digits. 404 if the catalog has no such title, or the title has nobody billed. 503 if the catalog is not published yet |
| `GET /grid/{tconst}/films?ids=tt1,tt2` | What the mounted cards say. `ids` is required, comma-separated, at most 200, each a `tconst`. Cards with no poster are queued for the TMDb job, and cards no source has been asked about for a synopsis are queued for the synopsis job |
| `GET /posters/{tconst}` | `{"poster":"<url>"}` when a picture the browser could not load has a TMDb stand-in. The stand-in is written back, so the next read does not ask again. 404 when there is nothing, or TMDb is not configured. 502 when the lookup failed |
| `GET /people/photos?ids=nm1,nm2` | `{"photos":{"nm0000206":"https://image.tmdb.org/t/p/w185/….jpg","nm0000401":null},"pending":["nm0000401"]}`: each person's photo, or `null` for none. `ids` is required, comma-separated, at most 50, each an IMDb name id, or 400. Only ever read from `meta.people`; it never asks TMDb. A person the people job has not answered yet is marked for it and listed in `pending`: ask again in a few seconds. A stored answer that has come due (past 150 days) is served as it is and marked the same way. A person the catalog does not hold is `null`, and so is every unanswered person when there are no TMDb credentials, with nothing pending. Never cached. 500 if it could not be read |
| `GET /trailers/{tconst}` | `{"key":"vKQi3bBA1y8"}`, the YouTube id of the film's trailer, or `{"key":null}` when there is none that can be embedded. Only ever read from `meta.trailers`; it never asks TMDb or YouTube. A film the trailer job has not answered yet is marked for it and answered `{"key":null,"pending":true}`: ask again in a few seconds. A stored answer that has come due (a recent film's week-old null, or anything past 150 days) is served as it is and marked the same way. A title the catalog does not hold is `{"key":null}`, and so is every unanswered film when there are no TMDb credentials. Never cached. 400 if the id is not a `tconst`, 500 if it could not be read |
| `GET /where-to-watch/{tconst}` | Where the movie can be watched in the reader's country, which the server works out from the request: `{"country":"us","countryName":"United States","covered":true,"stream":[…],"free":[],"rent":[…],"buy":[]}`. Each entry is `{id,name,link,logo:{dark,light},price?,via?}`: `price` on rent and buy (`"3.99 USD"`), `via` on an add-on (`"Prime Video"`). A reader who cannot be placed, or whose country has no coverage, is `{"country":"xx","covered":false}` (their own code when they were placed), and the API is not asked. Served from `meta.where_to_watch`; a movie and country nobody has asked about is asked once, in the request. `Cache-Control: private, max-age=3600` on an answer, `no-store` on an error. 400 if the id is not a `tconst`, 404 if the catalog does not hold it, 500 if it could not be read, 502 if the API could not answer (nothing is kept), 503 when there is no `STREAMING_API_KEY`, or while GeoLite2 is still being fetched. The API's own error never reaches the browser |
| `GET /daily?tz=Asia/Tokyo` | Today's puzzle for the reader, the one for their date in the zone `tz` names, as the page lays it out, and the reader's place in it: `{no, date, now, next, cards, start, clues, player, played, streak, game}`. For a player whose unfinished game of that puzzle has passed midnight in its own zone, it is that zone's next puzzle instead, once it has been picked. `date` is the puzzle's date (`"2026-10-08"`), `now` the server's clock and `next` the next midnight in the zone the puzzle was worked out in (the reader's, or for such a player the game's), when the puzzle ends for them, both in UTC to the millisecond. `cards` is every card as `{id, year, rating, md}`, where it goes and nothing more; `start` is the three starting cards as `{card, film}`, the only movies sent whole; `clues` is `{directors, cast}`, how many of each the answer has. `player` is `{name, saved}`: the reader's own name, or, with no player, a name to offer, remembered for Play, and `saved: false`. `played` is how many games of that puzzle have been started, in every zone; `streak` is `{now, before}`, ending at that puzzle; `game` is the reader's game of it, or null before Play. Writes nothing; a reader with a player has the cookie renewed. 503 `not-ready` while the reader's date has no puzzle |
| `GET /daily/me?tz=Asia/Tokyo` | The reader's standing, all the opening screens show of the boards: `{"streak": 3, "week": {"rank": 1204, "players": 83500}}`, on the reader's puzzle as `GET /daily` works it out, a player moved on by a left-behind game included. `streak` is the number the title screen shows: `now` once the reader has finished that puzzle, `before` until then. `week` is their place on its This week board, with its players and its order; `players` counts the reader. Until they have finished the puzzle it adds up only the days of its week before it, and after, through it. `week` is null when the reader is on no such board or has nothing on it, as on a Monday before playing. With no cookie, or one that is nobody's, it is `{"streak": 0, "week": null}` and sets no cookie; a player has theirs renewed. Writes nothing. Each answer is kept a minute, by player, puzzle and whether it is finished, so finishing is worked out afresh. 503 `not-ready` while a player's date has no puzzle |
| `POST /daily/name?tz=…` | `{}`, or `{"name": "…"}` with the name on screen, which is never offered back. With a player, gives them a fresh name and answers `{name, saved: true}`. Without one, answers another name to offer, `saved: false`, remembered for Play, and writes nothing. `tz` plays no part |
| `POST /daily/{no}/play?tz=…` | `{name}`, the name offered. Makes the reader's player if they have none, keeping that name only when this process offered it within the last 12 hours, it is still one the pool could draw, and nobody has it, sets `cd_daily`, and starts their game with 1,000 points, in the zone `tz` names, which it keeps: `{game, player: {name, saved: true}}`. For a player whose game was left behind at its own zone's midnight, it also takes the puzzle that zone has moved on to, and starts that game in the old game's zone. A game already started is answered as it is, in its own zone. 409 `day` when `no` is neither the puzzle for the reader's date in `tz` nor, for such a player, that one, or is the puzzle of a game already started whose own zone has passed midnight. 429 `busy`, with `Retry-After: 360`, when the address has made too many players |
| `POST /daily/{no}/flip?tz=…` | `{key, seq, card}`: turns a card over. `{game}`. On this and the three moves below, `tz` plays no part: a move keeps to the zone its game was started in, and is 409 `day` once the puzzle's date has ended there |
| `POST /daily/{no}/buy?tz=…` | `{key, seq, kind}`, where `kind` is `director`, `actor`, `genres` or `year`. `{game}`. Year is 402 `points` under 200 and 409 `known` once bought. `story`, the clue Year replaced, is 400 `bad` |
| `POST /daily/{no}/guess?tz=…` | `{key, seq, film}`, where `film` is a `tconst`. `{game}` |
| `POST /daily/{no}/reveal?tz=…` | `{key, seq}`: Show the answer. `{game}` |
| `GET /daily/{no}/board?tab=week&tz=…` | One board of a puzzle, by its number, from every zone: `tab` is `today` (or left out) or `week`. `{tab, total, you, rows, beat, solved}`. `total` counts the players listed, and the reader when they are not. `you` is the reader's own `{rank, pts, secs, listed, days?}`, or null with no player or no finished game. Each row is `{rank, name, hue, pts, you, secs?, days?}` or `{"gap": true}`: `secs` on today's, and on the week's `days`, the points of each day Monday to Sunday, null for a day not played. `beat` and `solved` are today's percentages, the share of finished players who kept fewer points than the reader and the share of finished games that were won; both are null on the week's, and `beat` is null without a finished game of the reader's. Works without a cookie. 400 `bad` for any other tab. 404 `no-puzzle` for a puzzle that does not exist, or whose date is after the reader's in `tz` and, for a player whose game was left behind at its own zone's midnight, after the date that zone has moved on to |
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

Every Daily request carries the reader's time zone as `tz`, an IANA name such as `America/New_York` (URL-encoded, so `Etc/GMT+5` is `Etc%2FGMT%2B5`), which the page adds to every call so none can leave it out. It decides which date's puzzle is the reader's; the server's own clock decides the moment. A `tz` that is missing, empty, over 64 characters, `Local` or no zone the server knows is UTC, without a refusal. Every Daily response is `Cache-Control: no-store`, and a refusal is `{"error": "…", "reason": "…"}`. The page never shows `error`, which is written for whoever reads the logs; it words its own message from `reason`. A change must be sent as `Content-Type: application/json`, at most 4 KB, and an empty body reads as `{}`. A move's `key` is 8 to 64 of `A–Z`, `a–z`, `0–9`, `_` and `-`, and its `seq` is required and never negative. A crawler's change is answered 204 and does nothing. The reasons:

| Status | Reason | When |
| --- | --- | --- |
| 400 | `bad` | A body, key or `seq` that cannot be read, a card that is not on the board, a kind that is not a clue (`story` among them), a `film` that is not a `tconst`, or a `no` in any `{no}` route that is not a whole number from 1 to 2147483647, the most the int4 a puzzle's number is kept in holds |
| 402 | `points` | Not enough points for that card or clue |
| 403 | `cookie` | A move with no cookie, or one that is nobody's |
| 404 | `no-game` | A move before Play |
| 404 | `unknown` | A guess the catalog does not hold |
| 404 | `no-puzzle` | A board for a puzzle that does not exist, or whose date is after the reader's and after any date their left-behind game's zone has moved on to |
| 409 | `known` | A card already face up, a clue already bought or with nobody left, a movie already guessed |
| 409 | `done` | A move in a game that is over |
| 409 | `stale` | A move from an old point. The body carries `game`, the game as it stands |
| 409 | `day` | Play on a puzzle that is neither the one for the reader's date in `tz` nor, for a player whose game was left behind at its own zone's midnight, the one that zone has moved on to, or on a game already started whose own zone has passed midnight; a move once the puzzle's date has ended in the zone its game was started in; Play or a move on a number that is no puzzle, or whose date is current nowhere |
| 415 | `content-type` | A change not sent as JSON |
| 429 | `busy` | Too many new players from one address |
| 503 | `not-ready` | `GET /daily`, or `GET /daily/me` for a player, while the reader's date has no puzzle: before it is picked, or a date before the first puzzle |
| 503 | `unavailable` | The database failed, or the server was started without the Daily's store. The cause goes only to the log |

`game` is `{phase, pts, seq, startedAt, finishedAt, secs, won, gaveUp, nextCost, log, known, end}`. `phase` is `play` or `done`. `seq` is the number of moves recorded, which the next move sends back. `startedAt` and `finishedAt` are in UTC to the millisecond, as `now` is, and `finishedAt` is null until the end. `secs` is whole seconds from Play to the end, null until there is one. `nextCost` is what the next wrong guess costs. `known` is everyone bought or found, in slot order. `log` starts with `{"type": "start"}`, then has an entry for each move: `flip` (`card`, `cost`, and `film`, or `relative: {shared}` for a close relative), `person` (`role`, `director` or `actor`, `cost` and `people`), `genres` (`cost`, `genres`), `year` (`cost`, and `year`, the answer's, as a number), `guess` (`cost`, `film`, `card` or null, `shared`, `year` as `older`, `newer` or `same`, and `rating` as `higher`, `lower` or `same`, each null when the guess has nothing to compare), and to end it `win`, `gaveup` or `out`. A right guess logs only `win`, and a wrong guess that spends the last point logs `guess` then `out`. `end` is null until the game is over, then `{answer, cards, people}`: the answer with its `genres`, every card as `{id, film, people}` with its people as slots, and everyone.

A movie is `{id, title, year, rating, md, poster?}`. `rating` is a number for every card and the answer; only a guessed movie can be unrated, with `rating` null, and one whose year the catalog lacks has `year` 0. A person is `{id, name, role, slot, photo?, cards}`: `role` is `director` or `cast`, `slot` their place in the answer's people (directors first, then the billing), which is also their colour, and `cards` the board cards they are on, so the page can mark them. `poster` and `photo` are left out when there is none, and a photo past TMDb's 175 days is none.

Hashed assets under `/assets/` are cached for a year. `index.html` is `no-cache`, so a deploy's new asset hashes are picked up.

## Frontend

React 19 and TypeScript, bundled with Vite 6. There is no router and no state library. The URL and `history.state` are the route. Styling is one stylesheet, `web/src/grid.css`, with Young Serif for the display type (the wordmark, headlines and card titles) and Figtree for everything else.

| Module | What it decides |
| --- | --- |
| `GridApp.tsx` | The screen: route, search, cold start, header, toasts, when to fetch. Which page is drawn when there is no map, the opening screen, the About page or the Daily, and the one held, dimmed, while a map loads over it |
| `GridMap.tsx` | The scroller, the cards, the reveal, recenter |
| `grid.ts` | Layout, what is lit, lane packing, the rating domain. Pure: a payload, a width, and the settings go in, and positions come out. Also what the map and the Daily's board share: the warm band's step (`WARM_STEP`, `warmTop`) and the band and year-label classes (`bandClass`, `railYearClass`) |
| `trail.ts` | Which filters belong to this history entry, and which preferences belong to the reader |
| `api.ts` | `/api` client. `fetchTrailer` asks `/api/trailers/{id}` once per film and keeps the answer for the visit. A pending answer is asked again after about 1.5, 3, 5 and 6 seconds while the panel or preview is still open, and is never kept. `fetchPeoplePhotos` asks `/api/people/photos` about the people a map came without photos for, fifty at a time, asks again about the pending ones on the same schedule until none are or the caller goes, and keeps each photo, or its "none", for the visit. A pending answer or a failure is left out and never kept. A refusal is an `ApiError`, with the server's `reason` and its whole body, which is how the Daily reads a `stale` game. `postJSON` is every write, always sent as JSON. The Daily's calls are `fetchDaily`, `fetchDailyMe` (and `fetchDailyWeek`, its place alone, null when it fails, so a place that cannot be had is no place), `renameDaily`, `playDaily`, `sendDailyMove` and `fetchDailyBoard`, and every one goes through the same two helpers, which add the reader's zone as `tz`: `readerZone()`, Intl's name for it, read afresh for each request and left off when Intl throws or names none, so the server keeps to UTC |
| `whereToWatch.ts` | `useWhereToWatch(imdbId)` asks `/api/where-to-watch/{id}`, at most once at a time per movie, and keeps the answer for the visit. A failure stands for two seconds, so a preview opening just after the ask made as the pointer came to rest does not ask again, and a later open does. The preview's request starts when the pointer begins resting on a card, so the answer is usually in by the time the preview opens |
| `movieParam.ts` | `/movie/{tconst}-{slug}`, `/about`, `/daily`, the tab title, the slug rules the server's `og:url` is kept in step with, and the one check of an IMDb id's shape the page makes |
| `AboutPage.tsx` | The About page: what Cinedikt is, and each source's credit and notice |
| `DailyPage.tsx` | The Daily under the header. Asks for today's puzzle and the reader's place this week together as it opens, and for the place again when a game ends on the page, letting the old one go as it asks; posts each move with a fresh key and the game's `seq`, the same key again after a request that got no answer, one move at a time; draws the game the server sends back, quietly for a `stale` one, and reloads after `day`, `done`, `cookie` or `no-game`. Watches for the reader's midnight, `next` on the server's clock, and loads the new map then, telling a reader cut off mid-game why. The motion plays from the difference between the log before and after: the stagger, the flip, the points floating off, the shake, the finish. Owns the toast, the confetti, and **/** for the guess field. Sends `daily_play` and `daily_finish` (won, points, seconds) when analytics is on. `DailyHeaderTail` is the header's pill, date and **How it works** |
| `DailyBoard.tsx` | The board: the map's own rail, rating strip, bands and gridlines from `layoutGrid`, given a stand-in searched movie that is on no card and from year 0 until the game ends, then the answer, so no row is lit and nothing is laid out around a movie the page must not know. Once Year is bought the stand-in takes the year, which lights its row as a searched movie's is, and a year no card shares is opened by a placeholder (`__year`) that `boardLayout` takes off the cards again. Blank, turned and close-relative faces, the bounds' lines (the guesses' fade the cards; the guesses' and the year's together fade the rows and draw the lines), the answer's card. Only cards in the warm band are mounted, as on a map |
| `DailyPanel.tsx`, `DailyIntro.tsx` | The panel (points, feed, clues, the guess field, the results and their leaderboards), and the intro that is also the rules |
| `daily.ts` | What the page draws from a game, pure and tested without a DOM: the prices it shows (the server charges them), which cards are face up and what they show, where the wrong guesses leave the answer, what the feed, the buttons, the results and the leaderboard say, the year row and where buying it scrolls, the reader's place as the opening screens write it (`ord`, `standingText`), the share text, the puzzle's date written from its own parts so no zone moves it, the clock and the countdown on the server's time, the midnight watch (`watchMidnight`, in steps of at most 30 seconds, so a machine that slept through midnight catches up), and every timing |
| `DailyBanner.tsx` | The opening screen's way into the Daily, what it says before, during and after a game, and the banner gone when today's puzzle cannot be had. It asks `/daily/me` beside `/daily` (`fetchBanner`), for the reader's place this week, and a failed one is no place. `followBanner` asks again at the reader's midnight, keeping what it shows until the server replies |
| `firstRun.ts` | How many cold-screen tiles fit under the Daily's banner, which is counted whether or not it stays: a row too few is room to spare, a row too many is cut off |
| `PeopleChips.tsx`, `GridSheet.tsx`, `ViewPanel.tsx` | The chip row, the film sheet, the View panel |
| `poster.ts`, `PosterImage.tsx` | Resize Amazon and TMDb poster URLs, retry a miss, ask `/api/posters/{id}` for a stand-in |
| `theme.ts` | System, light, dark |
| `screen.ts` | The screen classes — phone below 640px, short (under 500px tall), tablet below 1024px, desktop — and touch sizing. Below 1024px the rating rungs move into the panel. `grid.css` names the same classes as media queries |
| `overHeader.ts` | The overlay header, and when it hides on scroll |
| `sheet.ts` | Enter, exit, drag-to-close, focus trap |
| `analytics.ts` | PostHog and Mixpanel, each in its own chunk, set up from the `cinedikt-analytics` tag the server writes into the page. No tag, and neither loads and nothing is fetched. Loopback never initialises |

The dev server is port 5173. `npm run build` typechecks and writes `web/dist`. `npm test` is Vitest, in Node, over the layout, the history trail, the posters, the sheet, the cold-screen arithmetic, and the Daily's rules, words, moves and banner. `web/src/fixtures/matrix-grid.json` is a fixture for those tests, not something the app loads.

## Configuration

`APP_ENV` is `development` (the default) or `production`. Only production reports to PostHog, and only with `ANALYTICS_ENABLED=true`. Then, and only then, the server writes the trackers' tokens into the page it serves, as a `<meta name="cinedikt-analytics">` tag at the end of the head; otherwise the page has no tag, loads neither tracker, and fetches nothing to find out. Nothing infers the environment from a hostname or a log level: a production deploy that happens to reach its database over localhost would otherwise go quiet. `dev` and `prod` are accepted. Anything else is a startup error. The Docker image sets `APP_ENV=production` itself.

`API_ADDR` wins over `PORT`. Unset, with no `PORT`, the process listens on `:8080`. `LOG_LEVEL=debug` turns on debug logs.

| Variable | Default | What it does |
| --- | --- | --- |
| `DATABASE_URL` | | Postgres, where the catalog lives. Required: without it the process will not start |
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
| `MAXMIND_LICENSE_KEY` | | Downloads GeoLite2 Country, with `MAXMIND_ACCOUNT_ID`, to place a reader's address in a country. Unset, nobody is placed |
| `MAXMIND_ACCOUNT_ID` | | The account the license key belongs to, required with it: the database comes from MaxMind's permalink, which takes both. A key without it is said at startup and leaves GeoLite2 off. Not a secret |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | | Job notifications, described below. Both empty, and nothing is sent. The token is from BotFather. The chat id is `message.chat.id` from `getUpdates` after Start — positive for a private chat, negative for a group. In a group the bot needs permission to pin messages |
| `NOTIFY_TIMEZONE` | UTC | The zone notification times are written in, such as `Africa/Lagos`. Unset or unknown, they are UTC and the board says so |
| `WEB_DIR` | | Built frontend. The image sets `/app/web/dist` |
| `ANALYTICS_ENABLED` | off | The switch for every tracker: PostHog and Mixpanel in the page, and PostHog's error reports from the server. Off unless `true`, whatever tokens are set, so they can stay in place while nothing is tracked |
| `POSTHOG_PROJECT_TOKEN`, `POSTHOG_HOST` | host `https://us.i.posthog.com` | Read only in production, and only with `ANALYTICS_ENABLED=true`. The server writes them into the page it serves, so nothing is fetched for them; the token is a write-only key |
| `MIXPANEL_PROJECT_TOKEN` | | Read only in production, and only with `ANALYTICS_ENABLED=true`. The server writes it into the page it serves, which then loads Mixpanel with autocapture and session recording (text and inputs masked); unset, the page never loads it |

Nothing but `DATABASE_URL` is required. A deployment with only a database URL imports the catalog and serves maps; the other keys add the pictures, synopses, trailers, photos and where to watch.

### Telegram

The process that runs the catalog jobs keeps one pinned message in the chat: a headline that says whether anything needs you, then one line for each of the ten jobs (Catalog, Posters, Backup posters, Search matching, Synopses, Trailers, People photos, Opening colours, Daily puzzles, Country lookup), and a footer with the next catalog check, the environment, and how long the process has been up. The Country lookup line names the GeoLite2 build in use and when the twice-daily check last found it still the newest, or says it is waiting for the first download, or off without both MaxMind settings or with where to watch off (no `STREAMING_API_KEY`, or a queue that would not start), since the check runs on where to watch's queue. It is edited in place, at most once a minute while something is running and at least once an hour, and an edit never makes a sound. A next check already in the past, or an "updated" time more than an hour old, means the process is stuck.

A new message is sent only when there is news, and its second line says whether anything is needed of you, so a lock screen shows the answer before the explanation. Good news is quiet: a new catalog going live, a new GeoLite2 build downloaded and in use (the first one a database gets says so), a failure that is over, a pass that ran for half an hour or more. It lands in the notification list without a sound. A sound is kept for what has lasted or needs you: a failure that has repeated for about an hour (at once for a refused API key, at the second attempt for an error of no known kind), the database unreachable for two minutes, the catalog at 72 hours old (and at 36 while its updates are failing; late only because IMDb has published nothing, it is said at 36 without a sound), and a reminder for anything still failing a day later. The opening colours are cosmetic and never make a sound. Each is said once. What has been said lives in `meta.notify`, so a deploy does not repeat it. Only the process that holds the jobs writes the board: a container waiting for the lease never touches it, and one that loses the lease stops, except to say the database is unreachable while nobody can hold it.

The GeoLite2 check runs on the queue, in whichever container claims it, so it can report before its container holds the jobs, which is when a new deploy's first check runs. What it says is kept and said once that container takes the jobs, by it alone: a new build is said only by the check that downloaded it, never by a container that loads it afterwards. A check counts as one failure however many times River retried it, so MaxMind being down is said at the second failed check, half a day on; a refused MaxMind key is said at once, like OMDb's or TMDb's.

## Credits

The About page, `/about`, credits IMDb, OMDb, TMDB, Movie of the Night and MaxMind with the notices below, word for word.

Information courtesy of IMDb (https://www.imdb.com). Used with permission.

This product uses the TMDB API but is not endorsed or certified by TMDB. Backup posters, the list of trailers, people's photos and the fallback synopses come from [TMDB](https://www.themoviedb.org).

OMDb’s content is licensed under [CC BY-NC 4.0](https://creativecommons.org/licenses/by-nc/4.0/). Synopses, posters and release dates come from [OMDb](https://www.omdbapi.com/).

Trailers play from YouTube.

Streaming availability information is provided by [Streaming Availability API by Movie of the Night](https://www.movieofthenight.com/about/api).

This product includes GeoLite Data created by MaxMind, available from [https://www.maxmind.com](https://www.maxmind.com).

## Logs

In production the API writes single-line JSON to stdout (`config.NewLogger`). Locally it writes readable text to stderr. Railway turns anything on stderr into an error, so plain text there makes every served request look like a failure. JSON hands `method`, `path`, `status`, `duration_ms`, and `bytes` over as fields — `@status:>=500`, `@duration_ms:>500` — rather than a string to grep. The logger is built from `APP_ENV` before configuration has finished loading, so a bad variable can still be reported.

Panics at the request boundary are logged and answered 500. In production, with analytics switched on, that log is also an exception in PostHog.

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

The restart policy is left at Railway's default, `ON_FAILURE`, and the service is not allowed to sleep. Declaring either would leave `config plan` permanently dirty, because the platform stores a default as null. Both matter: the catalog jobs run between requests, and a sleeping machine would drop that work.

Variables to set on the service: `DATABASE_URL`, and, for pictures, synopses, trailers, people's photos and the search fallback, `OMDB_API_KEY` and one of the TMDb credentials. `POSTHOG_PROJECT_TOKEN` and `MIXPANEL_PROJECT_TOKEN`, with `ANALYTICS_ENABLED=true`, if analytics should report. `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` if the jobs should report to a chat, with `NOTIFY_TIMEZONE` so its times are local. `STREAMING_API_KEY`, `MAXMIND_LICENSE_KEY` and `MAXMIND_ACCOUNT_ID` for where to watch. `WEB_DIR` only if it should differ from the path the image already sets.

## Tests

```bash
go test ./...
cd web && npm test
```

The catalog tests need a database of their own. They drop `catalog` and publish fixtures in its place, empty the `meta` tables the jobs keep (posters, id matches, synopses, trailers, people's photos, where to watch, GeoLite2, the notifier's memory, the published generation, and the Daily's puzzles, players, games and moves among them), and clear River's jobs, so pointing them at a working catalog destroys it, and its players with it. Nothing in the suite talks to the Streaming Availability API or MaxMind: both are stood in for, and the GeoLite2 databases the tests use are built by the tests.

```bash
createdb cinedikt_test
CATALOG_TEST_URL=postgres://user:pass@host:5432/cinedikt_test go test ./...
```

Without `CATALOG_TEST_URL` those tests skip, and the rest of the suite still runs. That includes the Daily's rules in `internal/daily` and its routes in `internal/api`, which stand in for the store, and which between them hold every response a game makes before its end to never naming the answer or a blank card, nor the answer's year before Year is bought.

The Telegram board and messages are compared with what the chat would receive, kept in `internal/telegram/testdata`. After a deliberate change to the wording, `UPDATE_GOLDEN=1 go test ./internal/telegram/` rewrites them; reading what it wrote is the review. `UPDATE_GOLDEN=1 go test ./cmd/api/` redraws the share-card references the same way.
