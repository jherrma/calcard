import { defineStore } from "pinia";
import type { User, LoginResponse, RefreshResponse } from "~/types/auth";

interface AuthState {
  user: User | null;
  accessToken: string | null;
  isAuthenticated: boolean;
  isAdmin: boolean;
  isLoading: boolean;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({
    user: null,
    accessToken: null,
    isAuthenticated: false,
    isAdmin: false,
    isLoading: true,
  }),

  actions: {
    async login(credentials: any) {
      const api = useApi();
      const response = await api<LoginResponse>("/api/v1/auth/login", {
        method: "POST",
        body: credentials,
      });

      this.setAuth(response);
    },

    setAuth(response: LoginResponse) {
      this.accessToken = response.access_token;
      this.user = response.user;
      this.isAuthenticated = true;
      this.isAdmin = response.user.is_admin || false;

      // Store refresh token in cookie
      const refreshCookie = useCookie("refresh_token", {
        httpOnly: false, // Client needs to access it for refresh
        secure: process.env.NODE_ENV === "production",
        sameSite: "strict",
        maxAge: 60 * 60 * 24 * 7, // 7 days
      });
      refreshCookie.value = response.refresh_token;

      // Schedule token refresh
      this.scheduleTokenRefresh(response.expires_in);
    },

    async register(data: any) {
      const api = useApi();
      await api("/api/v1/auth/register", {
        method: "POST",
        body: data,
      });
    },

    async setupAdmin(data: any) {
      const api = useApi();
      await api("/api/v1/auth/setup", {
        method: "POST",
        body: data,
      });
    },

    async logout() {
      const api = useApi();
      try {
        await api("/api/v1/auth/logout", { method: "POST" });
      } finally {
        this.clearAuth();
        navigateTo("/auth/login");
      }
    },

    async refreshToken() {
      const refreshCookie = useCookie("refresh_token");
      if (!refreshCookie.value) {
        this.clearAuth();
        return;
      }

      try {
        const api = useApi();
        const response = await api<RefreshResponse>("/api/v1/auth/refresh", {
          method: "POST",
          body: { refresh_token: refreshCookie.value },
        });

        this.accessToken = response.access_token;
        this.scheduleTokenRefresh(response.expires_in);
      } catch {
        this.clearAuth();
      }
    },

    scheduleTokenRefresh(expiresIn: number) {
      // Refresh 1 minute before expiration
      const refreshTime = (expiresIn - 60) * 1000;
      if (refreshTime > 0) {
        setTimeout(() => this.refreshToken(), refreshTime);
      }
    },

    clearAuth() {
      this.user = null;
      this.accessToken = null;
      this.isAuthenticated = false;
      this.isAdmin = false;
      const refreshCookie = useCookie("refresh_token");
      refreshCookie.value = null;
    },

    async initAuth() {
      this.isLoading = true;
      const refreshCookie = useCookie("refresh_token");
      if (refreshCookie.value) {
        await this.refreshToken();
        if (this.accessToken) {
          await this.fetchUser();
        }
      }
      this.isLoading = false;
    },

    async fetchUser() {
      const api = useApi();
      try {
        this.user = await api<User>("/api/v1/users/me");
        this.isAuthenticated = true;
        this.isAdmin = this.user.is_admin || false;
      } catch {
        this.clearAuth();
      }
    },
  },
});
