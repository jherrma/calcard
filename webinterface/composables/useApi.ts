import { useAuthStore } from "~/stores/auth";

export const useApi = () => {
  const config = useRuntimeConfig();
  const authStore = useAuthStore();

  const api = $fetch.create({
    baseURL: (config.public.apiBaseUrl as string) || "",
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
    async onResponseError({ request, response }) {
      // Never refresh when the failing call IS the refresh endpoint — that
      // re-entrancy is what caused the infinite refresh loop (H15).
      const url = typeof request === "string" ? request : (request as Request).url;
      if (
        response.status === 401 &&
        authStore.isAuthenticated &&
        !url.includes("/auth/refresh")
      ) {
        await authStore.refreshToken();
      }
    },
  });

  return api;
};
