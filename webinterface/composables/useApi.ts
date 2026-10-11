import { useAuthStore } from "~/stores/auth";

// When the "too many requests" toast may next be shown (epoch ms). Module-level
// on purpose: every store action builds its own useApi() instance, and a page
// load fires several requests in parallel, so a per-instance flag would raise
// one toast per request in exactly the situation the toast is about.
let rateLimitNoticeUntil = 0;

/** Minimum gap between two rate-limit toasts, whatever Retry-After says. */
const RATE_LIMIT_NOTICE_MIN_GAP_MS = 5_000;

/** Specs only: forget that a rate-limit toast was shown. */
export function resetRateLimitNotice() {
  rateLimitNoticeUntil = 0;
}

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

export const useApi = () => {
  const config = useRuntimeConfig();
  const authStore = useAuthStore();
  const nuxtApp = useNuxtApp();

  const api = $fetch.create({
    baseURL: (config.public.apiBaseUrl as string) || "",
    // Auto-retry exactly once, and ONLY on a 401, so a request that fails with
    // an expired access token is re-issued after onResponseError refreshes the
    // token. ofetch runs onRequest again on the retry, which re-reads
    // authStore.accessToken and attaches the fresh Bearer header. Scoping to
    // [401] keeps 5xx behaviour unchanged; retry is decremented per attempt so
    // it can never loop. 429 is handled by the wrapper below, not here.
    retry: 1,
    retryStatusCodes: [401],
    async onRequest({ options }) {
      if (authStore.accessToken) {
        options.headers = new Headers(options.headers as HeadersInit);
        options.headers.set('Authorization', `Bearer ${authStore.accessToken}`);
      }
    },
    async onResponse({ response }) {
      // Unwrap the backend's { status: "ok", data: ... } response wrapper
      if (response._data?.status === "ok" && response._data?.data !== undefined) {
        response._data = response._data.data;
      }
    },
    async onResponseError({ request, response, options }) {
      // Never refresh when the failing call IS the refresh endpoint — that
      // re-entrancy is what caused the infinite refresh loop (H15).
      const url = typeof request === "string" ? request : (request as Request).url;
      if (
        response.status === 401 &&
        authStore.isAuthenticated &&
        !url.includes("/auth/refresh")
      ) {
        // Single-flight refresh. This hook is awaited before ofetch decides to
        // retry, so the new token is in the store (and picked up by onRequest)
        // before the request is re-issued.
        await authStore.refreshToken();
        // refreshToken() never rejects: on failure it clears auth and redirects
        // to login. If we have no token afterwards the refresh failed, so cancel
        // the pending retry to avoid a pointless second 401.
        if (!authStore.accessToken) {
          options.retry = 0;
        }
      } else {
        // Don't auto-retry 401s we can't recover from (e.g. the refresh call
        // itself, or an unauthenticated request); let the error surface.
        options.retry = 0;
      }
    },
  });

  const notifyRateLimited = (message: string, seconds: number | null) => {
    const now = Date.now();
    if (now < rateLimitNoticeUntil) return;
    rateLimitNoticeUntil = now + Math.max((seconds ?? 0) * 1000, RATE_LIMIT_NOTICE_MIN_GAP_MS);
    try {
      // Store actions call useApi() outside component setup, where PrimeVue's
      // inject()-based useToast() would throw; the app context makes it work.
      nuxtApp.vueApp.runWithContext(() => useAppToast().warn(message, "Too many requests"));
    } catch {
      // No toast service: the rewritten error below still carries the message.
    }
  };

  // HTTP 429 handling. The server's limiters answer with Retry-After and a JSON
  // body; left alone, callers surfaced ofetch's '[GET] "…": 429 Too Many
  // Requests' or the body's machine code ("rate_limit_exceeded") to the user.
  //  - GET/HEAD with a short Retry-After are re-issued after waiting, at most
  //    RATE_LIMIT_MAX_RETRIES times. Writes are never repeated automatically.
  //  - Everything else raises ONE toast per burst and rejects with an error
  //    whose message, data.message and data.error are all the friendly text,
  //    so every existing `e.message` / `e.data?.message` reader shows it.
  const request = async (input: Parameters<typeof api>[0], options?: Parameters<typeof api>[1]) => {
    for (let attempt = 0; ; attempt++) {
      try {
        return await api(input, options);
      } catch (e) {
        if (!isRateLimitError(e)) throw e;
        const seconds = retryAfterFromError(e);
        const method = options?.method ?? (input instanceof Request ? input.method : "GET");
        if (
          isIdempotentMethod(method) &&
          attempt < RATE_LIMIT_MAX_RETRIES &&
          seconds !== null &&
          seconds <= RATE_LIMIT_MAX_WAIT_SECONDS
        ) {
          // Retry-After: 0 means "now"; still pause a beat rather than hammer.
          await sleep(Math.max(seconds, 1) * 1000);
          continue;
        }
        const message = rateLimitMessage(seconds);
        notifyRateLimited(message, seconds);
        throw friendlyRateLimitError(e, message, seconds);
      }
    }
  };

  // Keep the $Fetch surface (raw/create) so the return type is unchanged. Only
  // the call itself gets 429 handling; nothing in the app uses .raw.
  return Object.assign(request, {
    raw: api.raw,
    create: api.create,
  }) as unknown as typeof api;
};

/** Rewrites a 429 FetchError in place so every reader sees the friendly text. */
function friendlyRateLimitError(e: unknown, message: string, seconds: number | null) {
  const err = e as Error & { response?: { _data?: unknown } };
  err.message = message;
  if (err.response) {
    const body = err.response._data;
    err.response._data = {
      ...(body && typeof body === "object" ? body : {}),
      error: message,
      message,
      retry_after: seconds,
    };
  }
  return err;
}
