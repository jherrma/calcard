import { useAuthStore } from "~/stores/auth";

export const useApi = () => {
  const config = useRuntimeConfig();
  const authStore = useAuthStore();

  const api = $fetch.create({
    baseURL: (config.public.apiBaseUrl as string) || "",
    async onRequest({ options }) {
      if (authStore.accessToken) {
        options.headers = {
          ...options.headers,
          Authorization: `Bearer ${authStore.accessToken}`,
        };
      }
    },
    async onResponse({ response }) {
      // Unwrap the backend's { status: "ok", data: ... } response wrapper
      if (response._data?.status === "ok" && response._data?.data !== undefined) {
        response._data = response._data.data;
      }
    },
    async onResponseError({ response }) {
      if (response.status === 401 && authStore.isAuthenticated) {
        // Try to refresh token or logout
        await authStore.refreshToken();
      }
    },
  });

  return api;
};
