import type { User, LoginResponse, RefreshResponse } from "~/types/auth";

// Base refresh-token cookie attributes. A useCookie() handle serializes WRITES
// with its OWN options (options are not remembered per cookie name), so every
// writer must pass these — an options-less write would silently downgrade the
// cookie to a session cookie without Secure/SameSite.
const REFRESH_COOKIE_BASE = {
  httpOnly: false, // client JS must read it to perform the refresh
  secure: process.env.NODE_ENV === "production",
  sameSite: "strict",
} as const;

const REFRESH_MAX_AGE = 60 * 60 * 24 * 7; // 7 days

// Cookie options for the user's "Remember me" choice. When remembered, the
// refresh token persists for 7 days; otherwise maxAge is omitted so the browser
// treats it as a SESSION cookie (dropped on browser close) — that is what makes
// "Remember me = off" actually log the user out once they quit the browser (#19).
function refreshCookieOptions(remember: boolean) {
  return remember
    ? { ...REFRESH_COOKIE_BASE, maxAge: REFRESH_MAX_AGE }
    : { ...REFRESH_COOKIE_BASE };
}

// Companion cookie recording the "Remember me" choice so a token ROTATION
// (performRefresh, ~1 min before every access-token expiry) re-applies the SAME
// lifetime. Without it the persistent write-back from #75 would silently upgrade
// a session cookie back to a 7-day one, defeating #19. It shares the refresh
// token's lifetime, so the two always expire together.
const REMEMBER_COOKIE = "remember_me";

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
    async login(credentials: { email: string; password: string; remember?: boolean }) {
      const api = useApi();
      const response = await api<LoginResponse>("/api/v1/auth/login", {
        method: "POST",
        // Only email/password reach the backend; "remember" is a client-only
        // cookie-lifetime preference (#19).
        body: { email: credentials.email, password: credentials.password },
      });

      this.setAuth(response, credentials.remember ?? false);
    },

    // remember defaults to true so persistent callers (e.g. the OAuth callback)
    // keep the 7-day cookie; the email/password login passes the checkbox value.
    setAuth(response: LoginResponse, remember = true) {
      this.accessToken = response.access_token;
      this.user = response.user;
      this.isAuthenticated = true;
      this.isAdmin = response.user.is_admin || false;

      const options = refreshCookieOptions(remember);
      // Record the choice alongside the token (same lifetime) so later rotations
      // preserve it — see REMEMBER_COOKIE.
      useCookie(REMEMBER_COOKIE, options).value = remember ? "1" : "0";

      // Store refresh token in cookie.
      const refreshCookie = useCookie("refresh_token", options);
      refreshCookie.value = response.refresh_token;

      // Schedule token refresh.
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
      // Send the refresh token so the server can revoke it. Without it the
      // token stays valid for its full lifetime after "logout" (see #7).
      const refreshCookie = useCookie("refresh_token");
      try {
        if (refreshCookie.value) {
          await api("/api/v1/auth/logout", {
            method: "POST",
            body: { refresh_token: refreshCookie.value },
          });
        }
      } finally {
        this.clearAuth();
        navigateTo("/auth/login");
      }
    },

    async refreshToken() {
      // Per-tab single-flight: concurrent 401s in THIS tab share one refresh.
      if (this.refreshPromise) return this.refreshPromise;

      const refreshCookie = useCookie("refresh_token");
      if (!refreshCookie.value) {
        this.clearAuth();
        navigateTo("/auth/login");
        return;
      }

      // performRefresh does the actual token exchange. It re-reads the cookie
      // at call time so that, when run under the cross-tab lock, it always
      // presents the freshest token (another tab may have just rotated it).
      const performRefresh = async () => {
        // Preserve the login-time "Remember me" choice across rotations: read it
        // from its companion cookie (absent → default persistent, matching the
        // pre-#19 behaviour and any token issued before this shipped). The options
        // must match setAuth's, or the write-back below would change the cookie's
        // lifetime/attributes out from under the user.
        const remember = useCookie(REMEMBER_COOKIE).value !== "0";
        const options = refreshCookieOptions(remember);
        // Fresh useCookie() call re-parses document.cookie → picks up a rotation
        // performed by another tab while we were queued behind the lock.
        const cookie = useCookie("refresh_token", options);
        const presented = cookie.value;
        if (!presented) {
          this.clearAuth();
          navigateTo("/auth/login");
          return;
        }
        try {
          const api = useApi();
          const response = await api<RefreshResponse>("/api/v1/auth/refresh", {
            method: "POST",
            body: { refresh_token: presented },
            // The refresh request must never be auto-retried (it must not trigger
            // another refresh); the onResponseError guard skips it, and this
            // disables ofetch's own retry for it too.
            retry: false,
          });

          this.accessToken = response.access_token;
          // The backend ROTATES the refresh token on every use: persist the new
          // one over the presented (now-revoked) one, or the next refresh would
          // present a dead token and log the user out (#75).
          cookie.value = response.refresh_token;
          this.scheduleTokenRefresh(response.expires_at);
        } catch {
          // Refresh failed (revoked/expired): drop auth and go to login instead
          // of looping on more refresh attempts.
          this.clearAuth();
          navigateTo("/auth/login");
        }
      };

      this.refreshPromise = (async () => {
        try {
          // Cross-tab serialization via the Web Locks API: only one tab runs a
          // refresh at a time, so tabs can't race and rotate each other's tokens
          // into a revoked state. Degrade gracefully where locks are unavailable
          // (older browsers, SSR) by refreshing directly.
          const locks =
            typeof navigator !== "undefined" ? navigator.locks : undefined;
          if (locks && typeof locks.request === "function") {
            await locks.request("caldav-auth-refresh", performRefresh);
          } else {
            await performRefresh();
          }
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
      // Clear the companion choice cookie too, so a later login starts fresh.
      useCookie(REMEMBER_COOKIE).value = null;
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
