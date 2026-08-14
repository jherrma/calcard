import { palette, updatePrimaryPalette } from '@primeuix/themes';

/**
 * Accent colour (story 046) — the second half of theming, next to `theme.ts`.
 *
 * One hex value drives both colour systems:
 *
 *   - **Tailwind** `primary-*` utilities read `--accent-<shade>` CSS variables,
 *     which hold `R G B` triplets. The triplet form is not decoration: the
 *     config wraps them as `rgb(var(--accent-500) / <alpha-value>)` so opacity
 *     modifiers keep working, and the app really does use them
 *     (`bg-primary-900/20` in the settings nav).
 *   - **PrimeVue** design tokens, via `updatePrimaryPalette()`.
 *
 * Kept out of `theme.ts` deliberately: that file is imported by `nuxt.config.ts`
 * at build time and must stay free of runtime dependencies like this one.
 *
 * Unlike the light/dark choice, the accent is a SERVER preference
 * (`accent_color`, see `stores/preferences.ts`) — it has no reason to differ per
 * device, and nothing needs it before login.
 */

export const ACCENT_SHADES = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950] as const;

export type AccentShade = (typeof ACCENT_SHADES)[number];
export type AccentPalette = Record<AccentShade, string>;

/** Tailwind blue-500. Mirrors `PrefAccentColor`'s default in the Go domain. */
export const DEFAULT_ACCENT_COLOR = '#3b82f6';

/**
 * The default ramp is Tailwind's own blue, spelled out rather than generated.
 *
 * `palette()` produces a perfectly good scale, but not an identical one — its
 * dark end is noticeably softer (blue-700 comes out `#295bac` against
 * Tailwind's `#1d4ed8`). Generating the default would therefore restyle every
 * existing screen for users who never touch the setting, which is not something
 * an opt-in feature should do. Custom colours have no such baseline to preserve
 * and are generated.
 */
export const DEFAULT_ACCENT_PALETTE: AccentPalette = {
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
};

/** Canonical form: `#` + six lowercase hex digits. Matches the server pattern. */
const CANONICAL_HEX = /^#[0-9a-f]{6}$/;
const LOOSE_HEX = /^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/;

export interface AccentPreset {
  name: string;
  value: string;
}

/** Offered as swatches; a custom colour is still any valid hex. */
export const ACCENT_PRESETS: AccentPreset[] = [
  { name: 'Blue', value: DEFAULT_ACCENT_COLOR },
  { name: 'Indigo', value: '#6366f1' },
  { name: 'Purple', value: '#8b5cf6' },
  { name: 'Pink', value: '#ec4899' },
  { name: 'Red', value: '#ef4444' },
  { name: 'Orange', value: '#f97316' },
  { name: 'Amber', value: '#d97706' },
  { name: 'Green', value: '#16a34a' },
  { name: 'Teal', value: '#0d9488' },
  { name: 'Cyan', value: '#0891b2' },
];

/** True only for the exact form the server stores and this module expects. */
export function isAccentColor(value: unknown): value is string {
  return typeof value === 'string' && CANONICAL_HEX.test(value);
}

/**
 * Canonicalises hand-typed input: adds a missing `#`, expands `#abc` shorthand,
 * trims and lowercases. Returns null for anything that is not a hex colour.
 *
 * The server accepts ONLY the canonical form — expanding shorthand here rather
 * than there keeps one spelling in the database while still letting someone
 * type `fff` into the box.
 */
export function normalizeAccentColor(input: string): string | null {
  const trimmed = input.trim();
  if (!LOOSE_HEX.test(trimmed)) return null;

  let hex = (trimmed.startsWith('#') ? trimmed.slice(1) : trimmed).toLowerCase();
  if (hex.length === 3) {
    hex = hex
      .split('')
      .map(c => c + c)
      .join('');
  }
  return `#${hex}`;
}

/** `"#3b82f6"` → `"59 130 246"`, the channel form Tailwind's config expects. */
export function hexToRgbTriplet(hex: string): string {
  const r = Number.parseInt(hex.slice(1, 3), 16);
  const g = Number.parseInt(hex.slice(3, 5), 16);
  const b = Number.parseInt(hex.slice(5, 7), 16);
  return `${r} ${g} ${b}`;
}

/** The 50–950 ramp for an accent: exact Tailwind blue by default, else generated. */
export function accentPalette(hex: string): AccentPalette {
  if (hex === DEFAULT_ACCENT_COLOR) return { ...DEFAULT_ACCENT_PALETTE };
  return palette(hex) as AccentPalette;
}

/**
 * Pushes an accent into both colour systems. Invalid input is ignored rather
 * than applied — this value arrives from a server response and from a text
 * field, and a junk ramp would leave the UI colourless.
 */
export function applyAccentColor(hex: string): void {
  if (typeof document === 'undefined' || !isAccentColor(hex)) return;

  const ramp = accentPalette(hex);
  const root = document.documentElement;
  for (const shade of ACCENT_SHADES) {
    root.style.setProperty(`--accent-${shade}`, hexToRgbTriplet(ramp[shade]));
  }
  updatePrimaryPalette(ramp);
}
