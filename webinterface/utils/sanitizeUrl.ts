// sanitizeUrl — neutralize dangerous URL schemes before binding a value to an
// anchor `href`. Contact URL/email/phone/address fields originate from synced
// vCard data, which is fully attacker-controlled: a contact whose URL is
// `javascript:alert(document.cookie)` would execute on click (stored XSS).
//
// The function ALLOWLISTS a small set of navigation-safe schemes plus relative
// and scheme-relative references, and collapses everything else to an inert
// value. It is deliberately paranoid about obfuscation the browser would later
// undo when resolving the href: leading/embedded control characters and
// whitespace (browsers strip TAB/LF/CR from URLs entirely), HTML entities, and
// percent-encoding are all normalized away *before* the scheme is inspected.
//
// Auto-imported by Nuxt (files under `utils/`), so call `sanitizeUrl(...)`
// directly in .vue sources without an explicit import.

/** Value returned for anything that is not provably safe to navigate to. */
export const INERT_URL = '#';

/** Schemes we are willing to emit into an `href`. Compared lowercase, with the trailing colon. */
const SAFE_SCHEMES: ReadonlySet<string> = new Set(['http:', 'https:', 'mailto:', 'tel:']);

// Named HTML entities that could smuggle a scheme delimiter or scheme letters
// past a naive check. Numeric references are handled generically below.
const NAMED_ENTITIES: Readonly<Record<string, string>> = {
  colon: ':',
  tab: '\t',
  newline: '\n',
  sol: '/',
};

function fromCodePointSafe(code: number): string {
  if (!Number.isInteger(code) || code < 0 || code > 0x10ffff) return '';
  try {
    return String.fromCodePoint(code);
  } catch {
    return '';
  }
}

/** Decode the numeric and dangerous named HTML entities that could hide a scheme. */
function decodeEntities(input: string): string {
  return input
    .replace(/&#x([0-9a-f]+);?/gi, (_m, hex: string) => fromCodePointSafe(parseInt(hex, 16)))
    .replace(/&#(\d+);?/g, (_m, dec: string) => fromCodePointSafe(parseInt(dec, 10)))
    .replace(/&([a-z]+);?/gi, (m, name: string) => NAMED_ENTITIES[name.toLowerCase()] ?? m);
}

/** Best-effort percent-decode; malformed sequences are left untouched. */
function percentDecode(input: string): string {
  if (!input.includes('%')) return input;
  try {
    return decodeURIComponent(input);
  } catch {
    return input;
  }
}

// Code-point ranges the URL parser trims or ignores when reading the scheme:
// C0 controls + space, DEL through the C1 range up to NBSP, soft hyphen, and
// assorted Unicode whitespace / zero-width marks. Built at runtime from numeric
// ranges so no raw control byte ever lives in the source file.
const CONTROL_AND_WHITESPACE_RE = ((): RegExp => {
  const ranges: ReadonlyArray<readonly [number, number]> = [
    [0x00, 0x20], // C0 controls + space
    [0x7f, 0xa0], // DEL, C1 controls, NBSP
    [0xad, 0xad], // soft hyphen
    [0x1680, 0x1680],
    [0x180e, 0x180e],
    [0x2000, 0x200f],
    [0x2028, 0x2029],
    [0x202f, 0x202f],
    [0x205f, 0x205f],
    [0x3000, 0x3000],
    [0xfeff, 0xfeff], // BOM / zero-width no-break space
  ];
  const cls = ranges
    .map(([lo, hi]) => {
      const from = String.fromCharCode(lo);
      return lo === hi ? from : `${from}-${String.fromCharCode(hi)}`;
    })
    .join('');
  return new RegExp(`[${cls}]`, 'g');
})();

/**
 * Return a value safe to bind to an anchor `href`.
 *
 * Safe inputs are returned trimmed but otherwise unchanged (so legitimate
 * percent-encoding / query strings survive intact); anything else becomes
 * {@link INERT_URL}.
 */
export function sanitizeUrl(raw: string): string {
  if (typeof raw !== 'string') return INERT_URL;

  const trimmed = raw.trim();
  if (trimmed === '') return INERT_URL;

  // Build a normalized probe purely for scheme detection. We undo the layers of
  // obfuscation a browser would resolve, then strip control chars / whitespace
  // (which the URL parser discards) and lowercase for a case-insensitive match.
  const probe = percentDecode(decodeEntities(trimmed))
    .replace(CONTROL_AND_WHITESPACE_RE, '')
    .toLowerCase();

  // A scheme is `[a-z][a-z0-9+.-]*` immediately followed by ':'. Because none of
  // `/ ? #` are valid scheme characters, a colon appearing after them (e.g. a
  // relative path segment `foo/bar:baz`) is correctly NOT treated as a scheme.
  const schemeMatch = /^([a-z][a-z0-9+.-]*):/.exec(probe);
  if (schemeMatch) {
    const scheme = `${schemeMatch[1]}:`;
    return SAFE_SCHEMES.has(scheme) ? trimmed : INERT_URL;
  }

  // No scheme → relative (`/path`), scheme-relative (`//host`), query (`?q`) or
  // fragment (`#frag`). None of these can trigger script execution.
  return trimmed;
}
