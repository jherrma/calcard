import type { User, LoginResponse, RefreshResponse } from "~/types/auth";

interface AuthState {
  user: User | null;
  accessToken: string | null;
  isAuthenticated: boolean;
  isAdmin: boolean;
  isLoading: boolean;
  // Single-flight guard so concurrent 401s share one refresh request (H15).
  refreshPromise: Promise<void> | null;
  // Handle for the scheduled refresh timer so it can be cancelled.
  refreshTimer: ReturnType<typeof setTimeout> | null;
}

export const useAuthStore = defineStore("auth", {
  state: (): AuthState => ({
    user: null,
    accessToken: null,
    isAuthenticated: false,
    isAdmin: false,
    isLoading: true,
    refreshPromise: null,
    refreshTimer: null,
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
      this.scheduleTokenRefresh(response.expires_at);
    },

    async register(data: any) {
      const api = useApi();
      return await api<any>("/api/v1/auth/register", {
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
      // If a refresh is already in flight, await the same one (single-flight).
      if (this.refreshPromise) return this.refreshPromise;

      const refreshCookie = useCookie("refresh_token");
      if (!refreshCookie.value) {
        this.clearAuth();
        navigateTo("/auth/login");
        return;
      }

      this.refreshPromise = (async () => {
        try {
          const api = useApi();
          const response = await api<RefreshResponse>("/api/v1/auth/refresh", {
            method: "POST",
            body: { refresh_token: refreshCookie.value },
            // The refresh request must never be auto-retried (it must not trigger
            // another refresh); the onResponseError guard skips it, and this
            // disables ofetch's own retry for it too.
            retry: false,
          });

          this.accessToken = response.access_token;
          this.scheduleTokenRefresh(response.expires_at);
        } catch {
          // Refresh failed (revoked/expired): drop auth and go to login instead
          // of looping on more refresh attempts.
          this.clearAuth();
          navigateTo("/auth/login");
        } finally {
          this.refreshPromise = null;
        }
      })();

      return this.refreshPromise;
    },

    scheduleTokenRefresh(expiresAt: number) {
      // Cancel any previously scheduled refresh so timers don't pile up.
      if (this.refreshTimer) {
        clearTimeout(this.refreshTimer);
        this.refreshTimer = null;
      }
      // Refresh 1 minute before expiration
      const now = Math.floor(Date.now() / 1000);
      const refreshTime = (expiresAt - now - 60) * 1000;
      if (refreshTime > 0) {
        this.refreshTimer = setTimeout(() => this.refreshToken(), refreshTime);
      }
    },

    clearAuth() {
      if (this.refreshTimer) {
        clearTimeout(this.refreshTimer);
        this.refreshTimer = null;
      }
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
