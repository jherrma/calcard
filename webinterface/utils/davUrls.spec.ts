import { describe, it, expect } from 'vitest';
import { davHomeUrl, davOrigin } from './davUrls';

// The server serves /dav/{account username}/calendars/ and .../addressbooks/
// (server/internal/adapter/webdav). A client configured with any other shape
// fails with no hint why, so the exact strings are pinned here.

describe('davOrigin', () => {
  it('uses the configured API origin when there is one', () => {
    expect(davOrigin('https://dav.example.org', 'https://app.example.org')).toBe('https://dav.example.org');
  });

  it("falls back to the page's origin in the single-container build", () => {
    // apiBaseUrl is '' there (#99): the Go server serves the SPA itself.
    expect(davOrigin('', 'https://cal.example.org')).toBe('https://cal.example.org');
    expect(davOrigin(undefined, 'http://192.0.2.10:8080')).toBe('http://192.0.2.10:8080');
  });

  it('drops trailing slashes so no path gets a double slash', () => {
    expect(davOrigin('https://dav.example.org/', 'x')).toBe('https://dav.example.org');
    expect(davOrigin('', 'https://cal.example.org//')).toBe('https://cal.example.org');
  });
});

describe('davHomeUrl', () => {
  const origin = 'https://cal.example.org';

  it('builds the calendar home with its trailing slash', () => {
    expect(davHomeUrl(origin, 'aBcDeFgHiJkLmNoP', 'caldav')).toBe(
      'https://cal.example.org/dav/aBcDeFgHiJkLmNoP/calendars/',
    );
  });

  it('builds the address-book home with its trailing slash', () => {
    expect(davHomeUrl(origin, 'aBcDeFgHiJkLmNoP', 'carddav')).toBe(
      'https://cal.example.org/dav/aBcDeFgHiJkLmNoP/addressbooks/',
    );
  });

  it('keeps the username exactly as stored', () => {
    // REVERT PROOF: the DAV handler compares the segment case-sensitively, so
    // lower-casing a generated mixed-case username yields a 404 home.
    expect(davHomeUrl(origin, 'MixedCase', 'caldav')).toContain('/dav/MixedCase/');
  });

  it('percent-encodes a username that would otherwise break the path', () => {
    // Usernames are URL-safe today; this only guards the URL's shape.
    expect(davHomeUrl(origin, 'a/b c', 'carddav')).toBe('https://cal.example.org/dav/a%2Fb%20c/addressbooks/');
  });
});
