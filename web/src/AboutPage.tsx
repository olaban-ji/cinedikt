import { useId, useLayoutEffect, useRef } from 'react';
import { ABOUT_IN_MS, ABOUT_RISE_PX, ABOUT_STEP_MS, EASE, animate } from './motion';

/** The About page arriving: each part, in the order given, fades up
 *  into place a step behind the one before. A reader who has asked for
 *  no movement gets the page as it stands (see animate), so nothing is
 *  played and nothing comes back. The rest are handed back to be
 *  called off if the page goes before they finish. */
export function enterAbout(parts: Iterable<Element>): Animation[] {
  const played: Animation[] = [];
  let i = 0;
  for (const part of parts) {
    const a = animate(
      part,
      [
        { opacity: 0, transform: `translateY(${ABOUT_RISE_PX}px)` },
        { opacity: 1, transform: 'none' },
      ],
      {
        duration: ABOUT_IN_MS,
        delay: i * ABOUT_STEP_MS,
        easing: EASE.settle,
        // Held at the start through its delay, so a later part is not
        // seen in place before it has risen into it.
        fill: 'backwards',
      },
    );
    i += 1;
    if (a) played.push(a);
  }
  return played;
}

/** Who to write to. The opening screen's footer ends with it, and the
 *  About page's is it alone. */
export function MailLink() {
  return (
    <a className="cd-foot-link" href="mailto:hello@cinedikt.com">
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <rect x="3" y="5" width="18" height="14" rx="2.5" />
        <path d="m4 7.5 8 5.5 8-5.5" />
      </svg>
      hello@cinedikt.com
    </a>
  );
}

/** A source named in text: its name with the ↗ the trailer row's
 *  YouTube link wears, which says the link leaves the app, and under
 *  it, where it has one, who makes it. A screen reader hears both
 *  lines, and that the link leaves the app. */
function CreditLink({ href, name, by }: { href: string; name: string; by?: string }) {
  return (
    <a
      className="cd-credit-link"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={`${by ? `${name} ${by}` : name}, opens in a new tab`}
    >
      <span className="cd-credit-name">
        {name}
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M7 17 17 7M9 7h8v8" />
        </svg>
      </span>
      {by && <span className="cd-credit-by">{by}</span>}
    </a>
  );
}

/** What Cinedikt is, and the credit each source of its data asks for.
 *
 *  The notices are the sources' own words, and are not to be edited:
 *  IMDb's statement and MaxMind's example are word for word, OMDb's
 *  licence asks for a credit and a link to it, Movie of the Night's
 *  terms ask for this sentence and this link, and TMDB asks for its
 *  logo and its notice, the logo smaller than Cinedikt's own mark. The
 *  page names each source and gives its notice; it does not say what
 *  each one supplies.
 *
 *  The opening screen's About link is how a reader reaches it, which is
 *  what lets the credits live here rather than on that screen. */
export function AboutPage({ dim }: { dim: boolean }) {
  const page = useRef<HTMLElement>(null);
  const title = useId();
  const credits = useId();
  // Played as the page is put down, before it is painted, so no part of
  // it is ever seen in place before it has risen into it.
  useLayoutEffect(() => {
    const played = enterAbout(page.current?.children ?? []);
    return () => played.forEach((a) => a.cancel());
  }, []);
  return (
    // While a map picked from the search loads, the page steps back
    // behind the progress line, as the opening screen does.
    <main ref={page} className={`cd-about${dim ? ' cd-about-dim' : ''}`} aria-labelledby={title}>
      <div className="cd-about-intro">
        <h1 id={title} className="cd-about-head">
          About Cinedikt
        </h1>
        <p className="cd-about-lead">
          Cinedikt starts with a movie you love and shows every movie its cast and directors made,
          arranged by year and IMDb rating.
        </p>
        <p className="cd-about-body">
          Years run down the side and ratings run across, on the same scale on every map, so a 7.4
          always sits in the same place. Pick someone from the row of people to light only their
          movies, or open any movie to map it next.
        </p>
      </div>
      <section className="cd-about-credits" aria-labelledby={credits}>
        <h2 id={credits} className="cd-about-credits-head">
          Credits
        </h2>
        <ul className="cd-credits">
          <li className="cd-credit">
            <CreditLink href="https://www.imdb.com" name="IMDb" />
            <p className="cd-credit-notice">
              Information courtesy of IMDb (https://www.imdb.com). Used with permission.
            </p>
          </li>
          <li className="cd-credit">
            <CreditLink href="https://www.omdbapi.com" name="OMDb API" by="The Open Movie Database" />
            <p className="cd-credit-notice">
              OMDb’s content is licensed under{' '}
              <a
                href="https://creativecommons.org/licenses/by-nc/4.0/"
                target="_blank"
                rel="noopener noreferrer"
              >
                CC BY-NC 4.0
              </a>
              .
            </p>
          </li>
          <li className="cd-credit">
            {/* TMDB is named by its logo alone, which the link's label
                says aloud. */}
            <a
              className="cd-credit-tmdb"
              href="https://www.themoviedb.org"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="TMDB, opens in a new tab"
            >
              <span className="cd-about-tmdb-logo" aria-hidden="true" />
            </a>
            <p className="cd-credit-notice">
              This product uses the TMDB API but is not endorsed or certified by TMDB.
            </p>
          </li>
          <li className="cd-credit">
            <CreditLink
              href="https://www.movieofthenight.com/about/api"
              name="Streaming Availability API"
              by="by Movie of the Night"
            />
            <p className="cd-credit-notice">
              Streaming availability information is provided by Streaming Availability API by Movie
              of the Night.
            </p>
          </li>
          <li className="cd-credit">
            <CreditLink href="https://www.maxmind.com" name="GeoLite" by="by MaxMind" />
            <p className="cd-credit-notice">
              This product includes GeoLite Data created by MaxMind, available from
              https://www.maxmind.com.
            </p>
          </li>
        </ul>
      </section>
      <footer className="cd-about-foot">
        <MailLink />
      </footer>
    </main>
  );
}
