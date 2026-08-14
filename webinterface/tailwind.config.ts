import type { Config } from 'tailwindcss';

export default {
  // Story 046. Without this key Tailwind defaults to `media`, so every `dark:`
  // utility followed the OS while PrimeVue followed its own `.dark-mode`
  // selector — an OS-dark user got Tailwind's dark surfaces behind light
  // PrimeVue components, and the in-app toggle could not have moved either half.
  // Both now hang off the same class; see `utils/theme.ts`.
  darkMode: ['selector', '.dark-mode'],

  content: [
    './components/**/*.{js,vue,ts}',
    './layouts/**/*.vue',
    './pages/**/*.vue',
    './plugins/**/*.{js,ts}',
    './app.vue',
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['"Open Sans"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
      },
      colors: {
        primary: {
          50: '#eff6ff',
          100: '#dbeafe',
          200: '#bfdbfe',
          300: '#93c5fd',
          400: '#60a5fa',
          500: '#3b82f6',
          600: '#2563eb',
          700: '#1d4ed8',
          800: '#1e40af',
          900: '#1e3a8a',
          950: '#172554',
        },
      },
      borderRadius: {
        'xl': '16px',
        '2xl': '24px',
        '3xl': '32px',
      },
    },
  },
  plugins: [],
} satisfies Config;
