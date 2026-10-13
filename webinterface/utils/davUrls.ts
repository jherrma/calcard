// The URLs a CalDAV/CardDAV client is configured with. Pure helpers, shared by
// the settings pages that show them (components/settings/DavUrlBox.vue).
//
// The server's DAV tree is /dav/{account username}/calendars/ and
// /dav/{account username}/addressbooks/ (server/internal/adapter/webdav). The
// segment is the ACCOUNT username, also when the client logs in with a
// dedicated DAV credential, whose own username is only a login name. The
// handler compares it exactly, so it must be shown as stored, never
// lower-cased.

export type DavProtocol = 'caldav' | 'carddav';

/**
 * The origin DAV clients talk to: the API origin, or this page's own origin in
 * the single-container build, where apiBaseUrl is empty (#99). Trailing slashes
 * are dropped so the paths below can be appended as they are.
 */
export function davOrigin(apiBaseUrl: string | undefined | null, pageOrigin: string): string {
  return (apiBaseUrl || pageOrigin).replace(/\/+$/, '');
}

/** The calendar or address-book home of an account, with its trailing slash. */
export function davHomeUrl(origin: string, username: string, protocol: DavProtocol): string {
  const home = protocol === 'caldav' ? 'calendars' : 'addressbooks';
  return `${origin}/dav/${encodeURIComponent(username)}/${home}/`;
}
