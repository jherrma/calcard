/**
 * Open-source attribution types (story 101).
 *
 * One shape covers both halves of the list: the Go manifest served by
 * `GET /api/v1/about/open-source` and the npm manifest generated into
 * `public/open-source.json` use identical field names on purpose.
 */
export interface OpenSourcePackage {
  name: string;
  version: string;
  /**
   * SPDX-ish identifier, or `UNKNOWN_LICENSE` when it could not be determined.
   * "unknown" means "not detected / not declared", NOT "unlicensed" — never
   * present it as the latter.
   */
  license: string;
  /** Repository or package page. Always an https URL. */
  url: string;
}

export interface OpenSourceManifest {
  /** How the list was produced — shown as provenance in the UI. */
  generator: string;
  /** Caveat text from the generator (best-effort detection). */
  note: string;
  count: number;
  packages: OpenSourcePackage[];
}

/** Sentinel emitted by both generators when the license could not be resolved. */
export const UNKNOWN_LICENSE = 'unknown';
