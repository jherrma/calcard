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
    async onResponseError({ response }) {
      if (response.status === 401 && authStore.isAuthenticated) {
        // Try to refresh token or logout
        await authStore.refreshToken();
      }
    },
  });

  return api;
};
