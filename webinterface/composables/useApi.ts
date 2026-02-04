// composables/useApi.ts
import { useAuthStore } from '~/stores/auth';

export const useApi = () => {
  const config = useRuntimeConfig();
  const authStore = useAuthStore();

  const baseURL = config.public.apiBaseUrl;

  const request = async <T>(
    endpoint: string,
    options: any = {}
  ): Promise<T> => {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...options.headers,
    };

    if (authStore.accessToken) {
      headers['Authorization'] = `Bearer ${authStore.accessToken}`;
    }

    try {
      const response = await $fetch<T>(endpoint, {
        baseURL,
        ...options,
        headers,
      });
      return response;
    } catch (error: any) {
      // Handle global errors here (e.g., redirect to login on 401)
      if (error.status === 401) {
        authStore.clearAuth();
        navigateTo('/auth/login');
      }
      throw error;
    }
  };

  return {
    get: <T>(endpoint: string, options?: any) => request<T>(endpoint, { ...options, method: 'GET' }),
    post: <T>(endpoint: string, body?: any, options?: any) => 
      request<T>(endpoint, { ...options, method: 'POST', body }),
    patch: <T>(endpoint: string, body?: any, options?: any) => 
      request<T>(endpoint, { ...options, method: 'PATCH', body }),
    delete: <T>(endpoint: string, options?: any) => 
      request<T>(endpoint, { ...options, method: 'DELETE' }),
  };
};
