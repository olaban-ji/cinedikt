import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';

/** The design's colour table, as the handoff gives it. Recreated
 *  exactly, so any drift from it is a bug rather than a taste. */
const DARK: Record<string, string> = {
  '--g': 'oklch(0.175 0.008 60)',
  '--s': 'oklch(0.215 0.009 60)',
  '--c': 'oklch(0.235 0.01 60)',
  '--c2': 'oklch(0.29 0.01 60)',
  '--t': 'oklch(0.965 0.008 80)',
  '--t2': 'oklch(0.78 0.012 70)',
  '--t3': 'oklch(0.63 0.012 70)',
  // The About page's TMDB logo, in white here.
  '--tmdb': '#ffffff',
  '--ln': 'oklch(0.95 0.01 80 / 0.05)',
  '--ln2': 'oklch(0.95 0.01 80 / 0.08)',
  '--ln3': 'oklch(0.95 0.01 80 / 0.16)',
  '--band': 'oklch(0.95 0.01 80 / 0.018)',
  '--dec': 'oklch(0.95 0.01 80 / 0.16)',
  '--acc': 'oklch(0.74 0.15 295)',
  '--accInk': 'oklch(0.17 0.03 295)',
  '--accText': 'oklch(0.8 0.13 295)',
  '--accWash': 'oklch(0.74 0.15 295 / 0.08)',
  '--accSoft': 'oklch(0.74 0.15 295 / 0.16)',
  '--ancBg': 'oklch(0.26 0.04 295)',
  '--sh': 'inset 0 1px 0 oklch(0.95 0.01 80 / 0.04), 0 6px 18px rgba(0,0,0,0.28)',
  '--shHi': 'inset 0 0 0 1px oklch(0.95 0.01 80 / 0.25), 0 16px 36px rgba(0,0,0,0.5)',
  '--pop': '0 20px 50px rgba(0,0,0,0.5)',
  '--scrim': 'oklch(0.12 0.006 60 / 0.62)',
  '--glass': 'oklch(0.175 0.008 60 / 0.86)',
  '--up': 'oklch(0.8 0.13 150)',
  '--down': 'oklch(0.76 0.13 25)',
  '--skel': 'oklch(0.95 0.01 80 / 0.06)',
  '--track': 'oklch(0.95 0.01 80 / 0.16)',
  '--hist': 'oklch(0.95 0.01 80 / 0.14)',
};

const LIGHT: Record<string, string> = {
  '--g': 'oklch(0.97 0.01 80)',
  '--s': '#ffffff',
  '--c': '#ffffff',
  '--c2': 'oklch(0.93 0.01 80)',
  '--t': 'oklch(0.22 0.01 60)',
  '--t2': 'oklch(0.42 0.012 60)',
  '--t3': 'oklch(0.52 0.012 60)',
  // And in TMDB's own colours here.
  '--tmdb': 'linear-gradient(90deg, #90cea1 0%, #3cbec9 56%, #00b3e5 100%)',
  '--ln': 'oklch(0.25 0.01 60 / 0.06)',
  '--ln2': 'oklch(0.25 0.01 60 / 0.1)',
  '--ln3': 'oklch(0.25 0.01 60 / 0.2)',
  '--band': 'oklch(0.25 0.01 60 / 0.022)',
  '--dec': 'oklch(0.25 0.01 60 / 0.2)',
  '--acc': 'oklch(0.5 0.2 295)',
  '--accInk': '#ffffff',
  '--accText': 'oklch(0.45 0.2 295)',
  '--accWash': 'oklch(0.5 0.2 295 / 0.07)',
  '--accSoft': 'oklch(0.5 0.2 295 / 0.1)',
  '--ancBg': 'oklch(0.96 0.03 295)',
  '--sh': '0 1px 2px rgba(40,30,10,0.07), 0 4px 14px rgba(40,30,10,0.06)',
  '--shHi': '0 0 0 1px oklch(0.25 0.01 60 / 0.16), 0 12px 28px rgba(40,30,10,0.14)',
  '--pop': '0 20px 50px rgba(40,30,10,0.2)',
  '--scrim': 'oklch(0.3 0.02 60 / 0.3)',
  '--glass': 'oklch(0.97 0.01 80 / 0.88)',
  '--up': 'oklch(0.48 0.13 150)',
  '--down': 'oklch(0.52 0.16 25)',
  '--skel': 'oklch(0.25 0.01 60 / 0.06)',
  '--track': 'oklch(0.25 0.01 60 / 0.16)',
  '--hist': 'oklch(0.25 0.01 60 / 0.14)',
};

/** The five curves, by the names the design gives them. */
const CURVES: Record<string, string> = {
  '--ease-glide': 'cubic-bezier(0.22, 0.9, 0.24, 1)',
  '--ease-settle': 'cubic-bezier(0.16, 1, 0.3, 1)',
  '--ease-exit': 'cubic-bezier(0.4, 0, 0.8, 0.4)',
  '--ease-spread': 'cubic-bezier(0.2, 0.8, 0.2, 1)',
  '--ease-focus': 'cubic-bezier(0.25, 0.7, 0.2, 1)',
};

interface Rule {
  selector: string;
  decls: [string, string][];
}

/** Every rule in the sheet that holds declarations, @media ones
 *  included, with comments gone. Enough of a parser for a stylesheet
 *  that has no nesting beyond @media and no braces inside strings. */
function rules(sheet: string): Rule[] {
  const text = sheet.replace(/\/\*[\s\S]*?\*\//g, '');
  const out: Rule[] = [];
  for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const decls = m[2]
      .split(';')
      .map((d) => d.trim())
      .filter(Boolean)
      .map((d): [string, string] => {
        const at = d.indexOf(':');
        return [d.slice(0, at).trim(), d.slice(at + 1).trim()];
      });
    out.push({ selector: m[1].trim(), decls });
  }
  return out;
}

/** The custom properties set by every rule with exactly this selector. */
function props(selector: string): Map<string, string> {
  const found = new Map<string, string>();
  for (const r of rules(css)) {
    if (r.selector !== selector) continue;
    for (const [k, v] of r.decls) if (k.startsWith('--')) found.set(k, v);
  }
  return found;
}

/** Whitespace is not part of a value. */
const flat = (v: string | undefined) => v?.replace(/\s+/g, '');

describe('the theme tokens', () => {
  it('are the design’s dark values, as the default', () => {
    const got = props(':root');
    for (const [k, v] of Object.entries(DARK)) expect(flat(got.get(k)), k).toBe(flat(v));
  });

  it('are the design’s light values, when the page is light', () => {
    const got = props(":root[data-theme='light']");
    for (const [k, v] of Object.entries(LIGHT)) expect(flat(got.get(k)), k).toBe(flat(v));
  });

  it('are the only colours the light theme changes', () => {
    // Anything else set per theme would be a colour one theme has and
    // the other quietly inherits. The people's colours are not tokens:
    // each is worked out per person for the theme and set inline.
    const light = [...props(":root[data-theme='light']").keys()];
    expect(light.sort()).toEqual(Object.keys(LIGHT).sort());
  });

  it('name the five motion curves', () => {
    const got = props(':root');
    for (const [k, v] of Object.entries(CURVES)) expect(got.get(k), k).toBe(v);
  });

  it('include no token that nothing defines', () => {
    const defined = new Set([...props(':root').keys(), ...props(":root[data-theme='light']").keys()]);
    // Set inline by the components, per element. --h is a film's hue,
    // which the sheet's wash is drawn in; --pin-lift is how far down the
    // header lying over the map pushes the pinned year labels; the
    // --trailer- ones are an open trailer player's measurements, which
    // trailer.ts works out; --syn-tr is the panel synopsis's transition,
    // timed for whatever is changing it (synopsis.ts). --tone is a
    // person's colour (personColour.ts), and on the Daily a person's or a
    // wrong guess's warmth (daily.ts's toneStyle).
    const inline = new Set([
      '--tone',
      '--swatch-r',
      '--i',
      '--at',
      '--lines',
      '--poster-fill',
      '--poster-w',
      '--rail-w',
      '--h',
      '--pin-lift',
      '--trailer-w',
      '--trailer-vh',
      '--trailer-gap',
      '--trailer-well',
      '--trailer-origin',
      '--syn-tr',
      // The visual viewport's top and height, which the Daily sets on the
      // app's box so the guess bar rides above an on-screen keyboard.
      '--vv-top',
      '--vv-h',
      // The glow round the Daily's title card: today's poster colour,
      // lightened, worked out in script (daily.ts's posterGlow).
      '--glow',
      // Where the Movies sheet's one-time pointer puts its caret, over
      // the card it points at (dailyMovies.ts's coachAt).
      '--caret',
    ]);
    // Set by the very rule that reads it: the sheet's wash is an oklch()
    // made from --h, which has to live in a custom property to ship as
    // written, and has one value per theme. A person's face is the same
    // drawing at three sizes: .cd-face and its parts read the size, the
    // photo's inset, the director's corner and the fade, which the face's
    // own rules set (.cd-face-chip and the rest). Name Drop's hidden card
    // is lit in white whatever the theme, as a poster is, and its sheen,
    // edge, "?" and stand-in title's shadow are set on .cd-daily, with the
    // bands of light that cross the title screen's card and Play.
    const local = new Set([
      '--wash',
      '--face',
      '--face-inset',
      '--face-r',
      '--face-fade',
      '--nd-sheen',
      '--nd-edge',
      '--nd-q',
      '--nd-glow',
      '--nd-card-shine',
      '--nd-play-shine',
    ]);
    const used = new Set([...css.matchAll(/var\((--[\w-]+)/g)].map((m) => m[1]));
    const missing = [...used].filter((v) => !defined.has(v) && !inline.has(v) && !local.has(v));
    expect(missing).toEqual([]);
  });
});

/** Every ordinary declaration that writes an oklch() colour itself. */
function bareOklch(sheet: string): string[] {
  return rules(sheet).flatMap((r) =>
    r.decls.filter(([k, v]) => !k.startsWith('--') && /oklch\(/.test(v)).map(([k]) => `${r.selector} ${k}`),
  );
}

describe('the stylesheet', () => {
  it('is read whole', () => {
    // Every other test here would pass on an empty string.
    expect(rules(css).length).toBeGreaterThan(200);
  });

  it('writes oklch() only inside custom properties', () => {
    // The build rewrites an oklch() in an ordinary declaration as hex,
    // and a gradient between hex stops is mixed in sRGB, not OKLab. A
    // custom property's value ships as written; color-mix(in oklch, …)
    // is fine, because it is not an oklch() colour.
    expect(bareOklch(css)).toEqual([]);
    expect(
      bareOklch(`:root { --a: oklch(0.5 0.2 295); }
        .b { color: color-mix(in oklch, var(--a) 50%, transparent); }
        @media (max-width: 1px) { .c { background: linear-gradient(oklch(0.4 0.1 20), red); } }`),
    ).toEqual(['.c background']);
  });

  it('invents no colour outside custom properties, as its header promises', () => {
    // A shadow or fill written out in a rule drifts from the token it
    // copies when that token is retuned, and the theme never reaches it.
    // A mask's stops are not drawn — a mask reads only their alpha — so
    // the chip row's #000 is not a colour.
    const literal = /rgba?\(|hsla?\(|#[0-9a-f]{3,8}\b/i;
    const invented = rules(css).flatMap((r) =>
      r.decls
        .filter(([k, v]) => !k.startsWith('--') && !/mask-image$/.test(k) && literal.test(v))
        .map(([k]) => `${r.selector.replace(/\s+/g, ' ')} ${k}`),
    );
    expect(invented).toEqual([]);
  });

  it('draws in Figtree and Young Serif only', () => {
    expect(css).not.toMatch(/Fraunces|Work Sans/);
    const families = rules(css).flatMap((r) => r.decls.filter(([k]) => k === 'font-family').map(([, v]) => v));
    for (const f of families) expect(f).toMatch(/^'(Figtree|Young Serif)', (sans-serif|serif)$/);
  });

  it('never asks Young Serif for a weight it does not have', () => {
    // It ships in 400 alone. A heading or a <strong> left to its default
    // would get a bold the browser smears together from the regular.
    for (const r of rules(css)) {
      if (!r.decls.some(([k, v]) => k === 'font-family' && v.includes('Young Serif'))) continue;
      expect(r.decls.find(([k]) => k === 'font-weight')?.[1], r.selector).toBe('400');
    }
  });

  it('uses only the Figtree weights the page loads', () => {
    const weights = new Set(
      rules(css).flatMap((r) => r.decls.filter(([k]) => k === 'font-weight').map(([, v]) => v)),
    );
    for (const w of weights) expect(['400', '500', '600', '700']).toContain(w);
  });
});
