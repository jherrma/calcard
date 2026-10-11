// Pure helpers for handling HTTP 429 Too Many Requests in useApi().
// Auto-imported (utils/), so composables and specs can call them directly.

/** How often an idempotent request is re-issued after a 429 before giving up. */
export const RATE_LIMIT_MAX_RETRIES = 2;

/**
 * Longest Retry-After (seconds) worth waiting out silently. Anything longer
 * would leave the user looking at a spinner, so it surfaces as a message.
 */
export const RATE_LIMIT_MAX_WAIT_SECONDS = 15;

/** Methods that are safe to re-issue: they cannot create or change anything. */
const IDEMPOTENT_METHODS = new Set(['GET', 'HEAD']);

export function isIdempotentMethod(method: string | undefined): boolean {
  return IDEMPOTENT_METHODS.has((method || 'GET').toUpperCase());
}

/**
 * Parses a Retry-After header into whole seconds from `now`.
 *
 * RFC 9110 allows delay-seconds ("120") or an HTTP-date. Returns null when the
 * header is missing or unparseable: "we don't know when" is not the same as
 * "retry now" (0), and must not be treated as such.
 */
export function parseRetryAfter(
  header: string | null | undefined,
  now: number = Date.now(),
): number | null {
  if (header == null) return null;
  const value = header.trim();
  if (value === '') return null;
  if (/^\d+$/.test(value)) return Number(value);
  // Only an HTTP-date contains letters; this keeps "1.5" or "-3" from being
  // read as a date by the lenient Date.parse.
  if (!/[a-z]/i.test(value)) return null;
  const date = Date.parse(value);
  if (Number.isNaN(date)) return null;
  return Math.max(0, Math.ceil((date - now) / 1000));
}

/** The user-facing text for a 429 — never the server's raw error payload. */
export function rateLimitMessage(seconds: number | null): string {
  if (seconds == null || seconds <= 0) {
    return 'Too many requests, please try again in a moment.';
  }
  return `Too many requests, please try again in ${seconds} second${seconds === 1 ? '' : 's'}.`;
}

/** The shape of an ofetch FetchError, as far as 429 handling needs it. */
interface RateLimitErrorLike {
  status?: number;
  statusCode?: number;
  data?: unknown;
  response?: { headers?: Headers };
}

/** True for an HTTP 429 rejection from $fetch / useApi(). */
export function isRateLimitError(e: unknown): boolean {
  if (!e || typeof e !== 'object') return false;
  const err = e as RateLimitErrorLike;
  return (err.status ?? err.statusCode) === 429;
}

/**
 * Seconds until the server accepts requests again, or null if it did not say.
 *
 * Prefers the Retry-After header and falls back to the `retry_after` field the
 * global limiter also puts in its body: a browser hides Retry-After from a
 * CROSS-origin caller unless the server lists it in Access-Control-Expose-Headers.
 */
export function retryAfterFromError(e: unknown, now: number = Date.now()): number | null {
  const err = (e ?? {}) as RateLimitErrorLike;
  const fromHeader = parseRetryAfter(err.response?.headers?.get?.('retry-after'), now);
  if (fromHeader !== null) return fromHeader;
  const body = err.data;
  const fromBody =
    body && typeof body === 'object' ? (body as { retry_after?: unknown }).retry_after : undefined;
  if (typeof fromBody === 'number' && Number.isFinite(fromBody) && fromBody >= 0) {
    return Math.ceil(fromBody);
  }
  return null;
}
