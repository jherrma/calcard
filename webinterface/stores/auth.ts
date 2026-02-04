// stores/auth.ts
import { defineStore } from 'pinia';

export const useAuthStore = defineStore('auth', () => {
  const accessToken = ref<string | null>(null);
  const user = ref<any>(null);

  const isAuthenticated = computed(() => !!accessToken.value);

  function setAuth(token: string, userData: any) {
    accessToken.value = token;
    user.value = userData;
  }

  function clearAuth() {
    accessToken.value = null;
    user.value = null;
  }

  return {
    accessToken,
    user,
    isAuthenticated,
    setAuth,
    clearAuth,
  };
});
