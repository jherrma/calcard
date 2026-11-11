/**
 * Helpers for failed API calls (#170 task and task-list writes). Pure,
 * auto-imported.
 */

/** What to do after a failure that has no sentence of its own. */
export const RETRY_HINT = 'Check your connection and try again.';

interface FetchErrorLike {
  status?: number;
  statusCode?: number;
  data?: { message?: unknown; error?: unknown } | null;
}

/** The HTTP status of an ofetch error, if it has one. */
export function httpErrorStatus(e: unknown): number | undefined {
  const err = e as FetchErrorLike | null;
  return err?.statusCode ?? err?.status;
}

/**
 * What to tell the user: the server's own message for a 4xx (the 409 on a
 * repeating task, a 400 field error, a 403/404, the friendly 429 text useApi
 * puts there), else the fallback. A 5xx body is the generic
 * "Internal server error", which says neither what failed nor what to do.
 */
export function clientErrorMessage(e: unknown, fallback: string): string {
  const status = httpErrorStatus(e);
  const body = (e as FetchErrorLike | null)?.data;
  const message = body?.message ?? body?.error;
  if (status !== undefined && status >= 400 && status < 500 && typeof message === 'string' && message.trim()) {
    return message;
  }
  return fallback;
}
