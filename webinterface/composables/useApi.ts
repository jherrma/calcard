import { useAuthStore } from "~/stores/auth";

export const useApi = () => {
  const config = useRuntimeConfig();
  const authStore = useAuthStore();

  const api = $fetch.create({
    baseURL: (config.public.apiBaseUrl as string) || "",
    // Auto-retry exactly once, and ONLY on a 401, so a request that fails with
    // an expired access token is re-issued after onResponseError refreshes the
    // token. ofetch runs onRequest again on the retry, which re-reads
    // authStore.accessToken and attaches the fresh Bearer header. Scoping to
    // [401] keeps 5xx behaviour unchanged; retry is decremented per attempt so
    // it can never loop.
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

  return api;
};
