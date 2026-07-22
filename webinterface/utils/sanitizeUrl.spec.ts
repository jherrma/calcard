import { describe, it, expect } from 'vitest';
import { sanitizeUrl, INERT_URL } from './sanitizeUrl';

// SCOPE: sanitizeUrl (#27). Contact URL fields come from synced vCard data and
// are rendered into an anchor `href`, so a value like `javascript:alert(1)`
// would execute on click (stored XSS). sanitizeUrl allowlists navigation-safe
// schemes + relative refs and collapses everything else to an inert `#`.
//
// REVERT PROOF: the `javascript:` / `data:` / `vbscript:` expectations below
// assert the inert value. If the sanitizer is removed (returns `raw`) or its
// allowlist is inverted, those assertions fail — see the dedicated block at the
// end that pins the exact regression.

describe('sanitizeUrl', () => {
  describe('passes safe URLs through unchanged (trimmed)', () => {
    it.each([
      'https://example.com',
      'https://example.org/path?x=1#frag',
      'http://example.org',
      'mailto:a@b.c',
      'tel:+1',
      'tel:+1-555-123-4567',
      '/relative/path',
      '//scheme-relative/host',
      '#frag',
      '?q=search+term',
      'HTTPS://EXAMPLE.COM', // scheme case is irrelevant to allowlisting
    ])('keeps %s', (input) => {
      expect(sanitizeUrl(input)).toBe(input);
    });

    it('trims surrounding whitespace from an otherwise-safe URL', () => {
      expect(sanitizeUrl('   https://example.com   ')).toBe('https://example.com');
    });

    it('preserves legitimate percent-encoding in a safe URL', () => {
      const url = 'https://example.com/path%20with%20space?q=a%26b';
      expect(sanitizeUrl(url)).toBe(url);
    });
  });

  describe('neutralizes dangerous / unknown schemes to the inert value', () => {
    it.each([
      ['javascript:alert(1)', 'lowercase javascript'],
      ['JavaScript:alert(1)', 'mixed-case javascript'],
      ['JAVASCRIPT:alert(1)', 'uppercase javascript'],
      ['  javascript:alert(1)', 'leading whitespace'],
      ['\t\n javascript:alert(1)', 'leading control chars'],
      ['java\tscript:alert(1)', 'embedded tab in scheme'],
      ['java\nscript:alert(1)', 'embedded newline in scheme'],
      ['data:text/html,<script>alert(1)</script>', 'data URI'],
      ['vbscript:msgbox(1)', 'vbscript'],
      ['file:///etc/passwd', 'file'],
      ['blob:https://evil/x', 'blob'],
      ['unknownscheme:whatever', 'unknown scheme'],
    ])('neutralizes %s (%s)', (input) => {
      expect(sanitizeUrl(input)).toBe(INERT_URL);
    });

    it('neutralizes HTML-entity-obfuscated javascript (numeric ref)', () => {
      expect(sanitizeUrl('&#106;avascript:alert(1)')).toBe(INERT_URL);
    });

    it('neutralizes HTML-entity-obfuscated javascript (named &Tab; inside scheme)', () => {
      expect(sanitizeUrl('java&Tab;script:alert(1)')).toBe(INERT_URL);
    });

    it('neutralizes percent-encoded scheme colon (javascript%3A…)', () => {
      expect(sanitizeUrl('javascript%3Aalert(1)')).toBe(INERT_URL);
    });
  });

  describe('handles empty / non-string input defensively', () => {
    it.each(['', '   ', '\t\n'])('returns the inert value for blank input %j', (input) => {
      expect(sanitizeUrl(input)).toBe(INERT_URL);
    });

    it('returns the inert value for non-string input', () => {
      // @ts-expect-error — exercising the runtime guard against non-string callers.
      expect(sanitizeUrl(null)).toBe(INERT_URL);
      // @ts-expect-error — exercising the runtime guard against non-string callers.
      expect(sanitizeUrl(undefined)).toBe(INERT_URL);
    });
  });

  // --- Explicit revert-proof anchor -------------------------------------------
  // This is the assertion that distinguishes the fix from its absence. Reverting
  // the fix (sanitizeUrl returning `raw`, or SAFE_SCHEMES inverted so the
  // allowlist becomes a blocklist) makes this expectation receive
  // "javascript:alert(document.cookie)" instead of "#", and the test FAILS.
  it('REVERT PROOF: a javascript: href is rendered inert, not passed through', () => {
    const payload = 'javascript:alert(document.cookie)';
    expect(sanitizeUrl(payload)).toBe(INERT_URL);
    expect(sanitizeUrl(payload)).not.toBe(payload);
  });
});
