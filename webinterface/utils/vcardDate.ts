/**
 * Normalize a vCard (RFC 6350) date string into ISO extended form `YYYY-MM-DD`.
 *
 * vCard dates arrive in two shapes:
 *   - basic form    `YYYYMMDD`   (no separators, e.g. `19850412`) — RFC 6350 default
 *   - extended form `YYYY-MM-DD` (e.g. `1985-04-12`)
 *
 * Both are collapsed to the extended form the `<DatePicker>` model expects.
 * Already-valid extended values pass through unchanged. Empty, whitespace,
 * undefined, or unrecognized input returns an empty string so callers never
 * build an `Invalid Date` (which would later serialize to `NaN-NaN-NaN`).
 *
 * Parsing is lenient and never throws: anything that doesn't start with a
 * recognizable year/month/day run yields `''`.
 */
export function normalizeVCardDate(input: string | null | undefined): string {
  if (!input) return '';
  const value = input.trim();
  if (!value) return '';

  // Anchored at the start; the separators are optional so both `YYYYMMDD` and
  // `YYYY-MM-DD` (and any trailing time component) are accepted. Trailing
  // characters after the date are ignored.
  const match = /^(\d{4})-?(\d{2})-?(\d{2})/.exec(value);
  if (!match) return '';

  const year = match[1]!;
  const month = match[2]!;
  const day = match[3]!;
  const iso = `${year}-${month}-${day}`;

  // Reject well-shaped but non-existent calendar dates (e.g. `19851345` →
  // `1985-13-45`, or Feb 30) so callers never build an `Invalid Date` that
  // later serializes to `NaN-NaN-NaN`. `Date` rolls out-of-range parts over, so
  // we require the parsed components to round-trip exactly.
  const probe = new Date(`${iso}T00:00:00Z`);
  if (
    Number.isNaN(probe.getTime()) ||
    probe.getUTCFullYear() !== Number(year) ||
    probe.getUTCMonth() + 1 !== Number(month) ||
    probe.getUTCDate() !== Number(day)
  ) {
    return '';
  }
  return iso;
}
