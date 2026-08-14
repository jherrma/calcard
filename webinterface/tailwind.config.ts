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
      // Story 046: primary-* is driven by the user's accent colour at runtime.
      // The variables hold `R G B` CHANNEL TRIPLETS rather than colours so the
      // `<alpha-value>` placeholder keeps opacity modifiers working — the app
      // uses `bg-primary-900/20` and friends, which a plain `var(--x)` would
      // silently break. Defaults (Tailwind blue) live in assets/css/theme.css,
      // and utils/accent.ts overwrites them.
      colors: {
        primary: Object.fromEntries(
          [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950].map(shade => [
            shade,
            `rgb(var(--accent-${shade}) / <alpha-value>)`,
          ]),
        ),
        // `surface-*` is used ~440 times across the templates but was never
        // defined here, so all of it compiled to nothing. Values come from
        // PrimeVue's Material preset via CSS variables in theme.css (slate in
        // light, zinc in dark) so Tailwind's surfaces and PrimeVue's components
        // cannot drift apart. Triplets again, for `bg-surface-900/50` and friends.
        surface: Object.fromEntries(
          [0, 50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950].map(shade => [
            shade,
            `rgb(var(--surface-${shade}) / <alpha-value>)`,
          ]),
        ),
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
