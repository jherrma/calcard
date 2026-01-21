# Story 032: Authentication UI

## Title
Implement Login, Registration, and Authentication Flow

## Description
As a user, I want to login, register, and manage my authentication so that I can access my calendars and contacts.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| UM-1.1.1 | Users can create an account with email and password |
| AU-2.1.1 | Users can login with email and password |
| AU-2.1.5 | Users can manually logout |
| AU-2.2.1 | Users can login via Google OAuth |
| AU-2.2.2 | Users can login via Microsoft/Azure AD |

## Acceptance Criteria

### Login Page

- [ ] Route: `/auth/login`
- [ ] Email input field with validation
- [ ] Password input field
- [ ] "Remember me" checkbox (optional)
- [ ] Submit button with loading state
- [ ] Link to registration page
- [ ] Link to forgot password page
- [ ] Error messages for invalid credentials
- [ ] Redirect to calendar after successful login

### OAuth Login Buttons

- [ ] "Continue with Google" button
- [ ] "Continue with Microsoft" button
- [ ] Buttons disabled if provider not configured
- [ ] OAuth flow opens in popup or redirect
- [ ] Handle OAuth callback and token exchange

### Registration Page

- [ ] Route: `/auth/register`
- [ ] Email input with validation
- [ ] Username input with validation (3-100 chars, alphanumeric)
- [ ] Display name input (optional)
- [ ] Password input with strength indicator
- [ ] Password confirmation input
- [ ] Submit button with loading state
- [ ] Link to login page
- [ ] Success message with email verification notice
- [ ] Client-side validation before submit

### Email Verification

- [ ] Route: `/auth/verify?token={token}`
- [ ] Automatically verify on page load
- [ ] Success message with link to login
- [ ] Error message for invalid/expired token

### Forgot Password

- [ ] Route: `/auth/forgot-password`
- [ ] Email input
- [ ] Submit button
- [ ] Success message (always shown to prevent enumeration)

### Reset Password

- [ ] Route: `/auth/reset-password?token={token}`
- [ ] New password input with strength indicator
- [ ] Password confirmation input
- [ ] Submit button
- [ ] Success message with link to login
- [ ] Error for invalid/expired token

### Authentication State

- [ ] Auth store manages tokens and user state
- [ ] Access token stored in memory
- [ ] Refresh token stored in httpOnly cookie (or secure storage)
- [ ] Auto-refresh token before expiration
- [ ] Logout clears all auth state
- [ ] Auth middleware protects routes

## Technical Notes

### Auth Store
```typescript
// stores/auth.ts
import { defineStore } from 'pinia';
import type { User, LoginRequest, RegisterRequest } from '~/types';

interface AuthState {
  user: User | null;
  accessToken: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    user: null,
    accessToken: null,
    isAuthenticated: false,
    isLoading: true,
  }),

  actions: {
    async login(credentials: LoginRequest) {
      const api = useApi();
      const response = await api.post<LoginResponse>('/api/v1/auth/login', credentials);

      this.accessToken = response.access_token;
      this.user = response.user;
      this.isAuthenticated = true;

      // Store refresh token in cookie
      const refreshCookie = useCookie('refresh_token', {
        httpOnly: false, // Client needs to send it
        secure: true,
        sameSite: 'strict',
        maxAge: 60 * 60 * 24 * 7, // 7 days
      });
      refreshCookie.value = response.refresh_token;

      // Schedule token refresh
      this.scheduleTokenRefresh(response.expires_in);
    },

    async register(data: RegisterRequest) {
      const api = useApi();
      await api.post('/api/v1/auth/register', data);
    },

    async logout() {
      const api = useApi();
      try {
        await api.post('/api/v1/auth/logout', {});
      } finally {
        this.clearAuth();
        navigateTo('/auth/login');
      }
    },

    async refreshToken() {
      const refreshCookie = useCookie('refresh_token');
      if (!refreshCookie.value) {
        this.clearAuth();
        return;
      }

      try {
        const response = await $fetch<RefreshResponse>('/api/v1/auth/refresh', {
          method: 'POST',
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
      setTimeout(() => this.refreshToken(), refreshTime);
    },

    clearAuth() {
      this.user = null;
      this.accessToken = null;
      this.isAuthenticated = false;
      const refreshCookie = useCookie('refresh_token');
      refreshCookie.value = null;
    },

    async initAuth() {
      this.isLoading = true;
      await this.refreshToken();
      if (this.accessToken) {
        await this.fetchUser();
      }
      this.isLoading = false;
    },

    async fetchUser() {
      const api = useApi();
      this.user = await api.get<User>('/api/v1/users/me');
      this.isAuthenticated = true;
    },
  },
});
```

### Login Page Component
```vue
<!-- pages/auth/login.vue -->
<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50 py-12 px-4">
    <div class="max-w-md w-full space-y-8">
      <div>
        <h2 class="mt-6 text-center text-3xl font-bold text-gray-900">
          Sign in to your account
        </h2>
      </div>

      <form class="mt-8 space-y-6" @submit.prevent="handleLogin">
        <div class="space-y-4">
          <div>
            <label for="email" class="block text-sm font-medium text-gray-700">
              Email address
            </label>
            <InputText
              id="email"
              v-model="form.email"
              type="email"
              required
              class="w-full"
              :class="{ 'p-invalid': errors.email }"
            />
            <small v-if="errors.email" class="p-error">{{ errors.email }}</small>
          </div>

          <div>
            <label for="password" class="block text-sm font-medium text-gray-700">
              Password
            </label>
            <Password
              id="password"
              v-model="form.password"
              required
              :feedback="false"
              toggle-mask
              class="w-full"
            />
          </div>
        </div>

        <div class="flex items-center justify-between">
          <NuxtLink
            to="/auth/forgot-password"
            class="text-sm text-primary-600 hover:text-primary-500"
          >
            Forgot your password?
          </NuxtLink>
        </div>

        <div>
          <Button
            type="submit"
            label="Sign in"
            :loading="isLoading"
            class="w-full"
          />
        </div>

        <Message v-if="error" severity="error" :closable="false">
          {{ error }}
        </Message>
      </form>

      <!-- OAuth Buttons -->
      <div class="mt-6">
        <div class="relative">
          <div class="absolute inset-0 flex items-center">
            <div class="w-full border-t border-gray-300" />
          </div>
          <div class="relative flex justify-center text-sm">
            <span class="px-2 bg-gray-50 text-gray-500">Or continue with</span>
          </div>
        </div>

        <div class="mt-6 grid grid-cols-2 gap-3">
          <Button
            label="Google"
            icon="pi pi-google"
            severity="secondary"
            outlined
            @click="loginWithOAuth('google')"
          />
          <Button
            label="Microsoft"
            icon="pi pi-microsoft"
            severity="secondary"
            outlined
            @click="loginWithOAuth('microsoft')"
          />
        </div>
      </div>

      <p class="mt-2 text-center text-sm text-gray-600">
        Don't have an account?
        <NuxtLink to="/auth/register" class="text-primary-600 hover:text-primary-500">
          Sign up
        </NuxtLink>
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'auth',
  middleware: 'guest',
});

const authStore = useAuthStore();
const router = useRouter();

const form = reactive({
  email: '',
  password: '',
});

const errors = reactive({
  email: '',
});

const isLoading = ref(false);
const error = ref('');

const handleLogin = async () => {
  error.value = '';
  isLoading.value = true;

  try {
    await authStore.login(form);
    router.push('/calendar');
  } catch (e: any) {
    error.value = e.data?.message || 'Invalid email or password';
  } finally {
    isLoading.value = false;
  }
};

const loginWithOAuth = (provider: string) => {
  const config = useRuntimeConfig();
  window.location.href = `${config.public.apiBaseUrl}/api/v1/auth/oauth/${provider}`;
};
</script>
```

### Auth Middleware
```typescript
// middleware/auth.ts
export default defineNuxtRouteMiddleware(async (to) => {
  const authStore = useAuthStore();

  // Wait for auth initialization
  if (authStore.isLoading) {
    await authStore.initAuth();
  }

  // Protected routes
  if (!authStore.isAuthenticated && !to.path.startsWith('/auth')) {
    return navigateTo('/auth/login');
  }
});
```

### Guest Middleware (for auth pages)
```typescript
// middleware/guest.ts
export default defineNuxtRouteMiddleware(() => {
  const authStore = useAuthStore();

  if (authStore.isAuthenticated) {
    return navigateTo('/calendar');
  }
});
```

### Password Strength Indicator
```vue
<!-- components/auth/PasswordStrength.vue -->
<template>
  <div class="mt-1">
    <div class="flex gap-1">
      <div
        v-for="i in 4"
        :key="i"
        class="h-1 flex-1 rounded"
        :class="i <= strength ? strengthColors[strength] : 'bg-gray-200'"
      />
    </div>
    <p class="text-xs mt-1" :class="strengthTextColors[strength]">
      {{ strengthLabels[strength] }}
    </p>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  password: string;
}>();

const strengthLabels = ['', 'Weak', 'Fair', 'Good', 'Strong'];
const strengthColors = ['', 'bg-red-500', 'bg-orange-500', 'bg-yellow-500', 'bg-green-500'];
const strengthTextColors = ['', 'text-red-500', 'text-orange-500', 'text-yellow-500', 'text-green-500'];

const strength = computed(() => {
  const password = props.password;
  if (!password) return 0;

  let score = 0;
  if (password.length >= 8) score++;
  if (/[a-z]/.test(password) && /[A-Z]/.test(password)) score++;
  if (/\d/.test(password)) score++;
  if (/[^a-zA-Z0-9]/.test(password)) score++;

  return score;
});
</script>
```

### Auth Layout
```vue
<!-- layouts/auth.vue -->
<template>
  <div class="min-h-screen bg-gray-50">
    <slot />
  </div>
</template>
```

## OAuth Callback Handling

```vue
<!-- pages/auth/oauth/callback.vue -->
<template>
  <div class="min-h-screen flex items-center justify-center">
    <div v-if="error" class="text-center">
      <h2 class="text-xl font-semibold text-red-600">Authentication Failed</h2>
      <p class="mt-2 text-gray-600">{{ error }}</p>
      <NuxtLink to="/auth/login" class="mt-4 inline-block text-primary-600">
        Back to Login
      </NuxtLink>
    </div>
    <div v-else class="text-center">
      <ProgressSpinner />
      <p class="mt-4 text-gray-600">Completing sign in...</p>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'auth',
});

const route = useRoute();
const authStore = useAuthStore();
const error = ref('');

onMounted(async () => {
  const { access_token, refresh_token, error: oauthError } = route.query;

  if (oauthError) {
    error.value = oauthError as string;
    return;
  }

  if (access_token && refresh_token) {
    authStore.accessToken = access_token as string;
    const refreshCookie = useCookie('refresh_token');
    refreshCookie.value = refresh_token as string;

    await authStore.fetchUser();
    navigateTo('/calendar');
  } else {
    error.value = 'Invalid authentication response';
  }
});
</script>
```

## Definition of Done

- [ ] Login page with email/password form
- [ ] Login form validation and error display
- [ ] OAuth login buttons (Google, Microsoft)
- [ ] Registration page with all fields
- [ ] Password strength indicator on registration
- [ ] Email verification page
- [ ] Forgot password page
- [ ] Reset password page
- [ ] Auth store manages tokens and user state
- [ ] Token refresh before expiration
- [ ] Auth middleware protects routes
- [ ] Guest middleware redirects authenticated users
- [ ] Logout functionality
- [ ] Responsive design for all auth pages
