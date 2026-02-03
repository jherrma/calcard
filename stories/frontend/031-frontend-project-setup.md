# Story 031: Frontend Project Setup

## Title
Initialize Vue 3 + Nuxt 3 Frontend Project

## Description
As a developer, I want a well-structured frontend project with Vue 3 and Nuxt 3 so that I can build the web interface for the CalDAV/CardDAV server.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| UI-7.1.1 | Web UI is responsive (mobile, tablet, desktop) |
| UI-7.1.2 | Web UI works in Chrome, Firefox, Safari, Edge |

## Acceptance Criteria

### Project Initialization

- [ ] Nuxt 3 project created with TypeScript support
- [ ] Project structure follows Nuxt 3 conventions
- [ ] ESLint and Prettier configured
- [ ] Git hooks for linting (husky + lint-staged)

### Dependencies

- [ ] Vue 3 (included with Nuxt 3)
- [ ] Pinia for state management
- [ ] PrimeVue for UI components
- [ ] @fullcalendar/vue3 for calendar (installed, configured in later story)
- [ ] @vueuse/nuxt for composables
- [ ] Tailwind CSS for styling

### Project Structure

- [ ] `/pages` - Route pages
- [ ] `/components` - Reusable components
- [ ] `/composables` - Vue composables
- [ ] `/stores` - Pinia stores
- [ ] `/types` - TypeScript type definitions
- [ ] `/utils` - Utility functions
- [ ] `/assets` - Static assets (CSS, images)
- [ ] `/plugins` - Nuxt plugins

### Configuration

- [ ] API base URL configurable via environment variable
- [ ] Runtime config for public/private settings
- [ ] SSR disabled (SPA mode) or properly configured for auth
- [ ] Proper meta tags and favicon

### Development Environment

- [ ] `npm run dev` starts development server
- [ ] Hot module replacement works
- [ ] TypeScript type checking
- [ ] Source maps enabled

### Build & Deployment

- [ ] `npm run build` creates production build
- [ ] Static site generation or SSR output
- [ ] Docker support for frontend (or combined with backend)

## Technical Notes

### Project Initialization
```bash
npx nuxi@latest init frontend
cd frontend
npm install
```

### Package.json Dependencies
```json
{
  "dependencies": {
    "@fullcalendar/core": "^6.1.10",
    "@fullcalendar/daygrid": "^6.1.10",
    "@fullcalendar/interaction": "^6.1.10",
    "@fullcalendar/timegrid": "^6.1.10",
    "@fullcalendar/vue3": "^6.1.10",
    "@pinia/nuxt": "^0.5.1",
    "@primevue/nuxt-module": "^4.0.0",
    "@vueuse/nuxt": "^10.7.2",
    "primevue": "^4.0.0",
    "primeicons": "^7.0.0"
  },
  "devDependencies": {
    "@nuxt/devtools": "latest",
    "@nuxtjs/tailwindcss": "^6.11.0",
    "@types/node": "^20.10.0",
    "typescript": "^5.3.0",
    "eslint": "^8.56.0",
    "@nuxtjs/eslint-config-typescript": "^12.1.0",
    "prettier": "^3.2.0",
    "husky": "^8.0.0",
    "lint-staged": "^15.2.0"
  }
}
```

### nuxt.config.ts
```typescript
export default defineNuxtConfig({
  devtools: { enabled: true },

  ssr: false, // SPA mode for simpler auth handling

  modules: [
    '@pinia/nuxt',
    '@primevue/nuxt-module',
    '@nuxtjs/tailwindcss',
    '@vueuse/nuxt',
  ],

  primevue: {
    options: {
      theme: {
        preset: 'Aura',
      },
    },
    components: {
      include: ['Button', 'InputText', 'Dialog', 'Toast', 'Menu', 'Avatar'],
    },
  },

  tailwindcss: {
    cssPath: '~/assets/css/tailwind.css',
  },

  runtimeConfig: {
    public: {
      apiBaseUrl: process.env.NUXT_PUBLIC_API_BASE_URL || 'http://localhost:8080',
    },
  },

  app: {
    head: {
      title: 'CalDAV Server',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'description', content: 'CalDAV/CardDAV Server Web Interface' },
      ],
      link: [
        { rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' },
      ],
    },
  },

  typescript: {
    strict: true,
    typeCheck: true,
  },

  compatibilityDate: '2024-01-01',
});
```

### Directory Structure
```
frontend/
├── nuxt.config.ts
├── package.json
├── tsconfig.json
├── tailwind.config.ts
├── .eslintrc.cjs
├── .prettierrc
│
├── assets/
│   └── css/
│       └── tailwind.css
│
├── components/
│   ├── common/
│   │   ├── AppHeader.vue
│   │   ├── AppSidebar.vue
│   │   ├── LoadingSpinner.vue
│   │   └── ErrorMessage.vue
│   ├── calendar/
│   ├── contacts/
│   └── auth/
│
├── composables/
│   ├── useApi.ts
│   ├── useAuth.ts
│   └── useToast.ts
│
├── layouts/
│   ├── default.vue
│   └── auth.vue
│
├── middleware/
│   └── auth.ts
│
├── pages/
│   ├── index.vue
│   ├── auth/
│   │   ├── login.vue
│   │   └── register.vue
│   ├── calendar/
│   │   └── index.vue
│   ├── contacts/
│   │   └── index.vue
│   └── settings/
│       └── index.vue
│
├── plugins/
│   ├── api.ts
│   └── primevue.ts
│
├── stores/
│   ├── auth.ts
│   ├── calendars.ts
│   └── contacts.ts
│
├── types/
│   ├── api.ts
│   ├── calendar.ts
│   ├── contact.ts
│   └── user.ts
│
├── utils/
│   ├── date.ts
│   └── validation.ts
│
└── public/
    └── favicon.ico
```

### API Composable
```typescript
// composables/useApi.ts
export const useApi = () => {
  const config = useRuntimeConfig();
  const authStore = useAuthStore();

  const baseURL = config.public.apiBaseUrl;

  const fetch = async <T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> => {
    const headers: HeadersInit = {
      'Content-Type': 'application/json',
      ...options.headers,
    };

    if (authStore.accessToken) {
      headers['Authorization'] = `Bearer ${authStore.accessToken}`;
    }

    const response = await $fetch<T>(`${baseURL}${endpoint}`, {
      ...options,
      headers,
    });

    return response;
  };

  return {
    get: <T>(endpoint: string) => fetch<T>(endpoint, { method: 'GET' }),
    post: <T>(endpoint: string, body: unknown) =>
      fetch<T>(endpoint, { method: 'POST', body: JSON.stringify(body) }),
    patch: <T>(endpoint: string, body: unknown) =>
      fetch<T>(endpoint, { method: 'PATCH', body: JSON.stringify(body) }),
    delete: <T>(endpoint: string) => fetch<T>(endpoint, { method: 'DELETE' }),
  };
};
```

### Tailwind Configuration
```typescript
// tailwind.config.ts
import type { Config } from 'tailwindcss';

export default {
  content: [
    './components/**/*.{js,vue,ts}',
    './layouts/**/*.vue',
    './pages/**/*.vue',
    './plugins/**/*.{js,ts}',
    './app.vue',
  ],
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#f0f9ff',
          100: '#e0f2fe',
          500: '#3788d8',
          600: '#2563eb',
          700: '#1d4ed8',
        },
      },
    },
  },
  plugins: [],
} satisfies Config;
```

### TypeScript Types
```typescript
// types/api.ts
export interface ApiResponse<T> {
  data: T;
  message?: string;
}

export interface ApiError {
  error: string;
  message: string;
  details?: ValidationError[];
}

export interface ValidationError {
  field: string;
  message: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}
```

### ESLint Configuration
```javascript
// .eslintrc.cjs
module.exports = {
  root: true,
  extends: ['@nuxtjs/eslint-config-typescript', 'prettier'],
  rules: {
    'vue/multi-word-component-names': 'off',
    '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
  },
};
```

## Environment Variables

```bash
# .env.example
NUXT_PUBLIC_API_BASE_URL=http://localhost:8080
```

## Definition of Done

- [ ] Nuxt 3 project initialized with TypeScript
- [ ] All dependencies installed and configured
- [ ] Project structure created with all directories
- [ ] Tailwind CSS working with custom theme
- [ ] PrimeVue components available
- [ ] Pinia stores set up
- [ ] API composable created
- [ ] ESLint and Prettier configured
- [ ] Development server runs without errors
- [ ] Production build succeeds
- [ ] README with setup instructions
