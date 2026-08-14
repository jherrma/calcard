# Story 046: Dark Mode & Theming

## Story
**As a** user
**I want to** switch between light and dark themes
**So that** I can use the application comfortably in different lighting conditions and according to my preferences

## Implementation status — COMPLETE (2026-08-14)

All four acceptance groups are met. Two of them were met by fixing bugs nobody knew existed, so the
history matters more than usual here.

### What shipped

**Theme**: `composables/useTheme.ts`, `utils/theme.ts`, `components/common/ThemeToggle.vue`
(in `AppHeader` and the auth layout, so it works logged out), `plugins/theme.client.ts`,
`assets/css/theme.css`, and an inline `app.head` script for flash prevention. Device-local
(`localStorage: calcard-theme`).

**Accent colour**: `utils/accent.ts`, `composables/useAccentColor.ts`, the picker on
`pages/settings/appearance.vue`, and a new **server** preference `accent_color` — the first
pattern-validated key in `server/internal/domain/user/preference.go`.

### Three bugs the story's premise had wrong

The 2026-08-04 audit said dark mode was unreachable because nothing set `.dark-mode`. That was the
smallest of the problems:

1. **`tailwind.config.ts` had no `darkMode` key**, so Tailwind used its `media` default. Every
   `dark:` utility followed the OS while PrimeVue followed a class nobody set — an OS-dark user got
   a *half-dark* app. Now `darkMode: ['selector', '.dark-mode']`.
2. **`surface-*` was never a defined Tailwind colour.** ~440 utilities — three quarters of all
   `dark:` usage — compiled to **nothing**. The surfaces anyone actually saw came from PrimeVue's
   component CSS alone. Now defined against CSS variables carrying PrimeVue's own Material palette
   (slate light / zinc dark), so the two systems cannot disagree.
3. **`assets/css/fullcalendar.css` targeted a bare `.dark`**, which neither strategy ever produced.
   That dark calendar grid had never once rendered.

None of these produce an error — a missing Tailwind colour emits no CSS and no warning. **Check the
BUILT stylesheet, not the templates**, before believing any claim that something is styled.
`utils/theme.spec.ts` now asserts all four systems name the same class and that every CSS variable
the config references is actually defined.

### Contrast

Audited with real WCAG maths over the palette actually in use, not by eye:

- Muted text is `text-surface-500 dark:text-surface-400` (4.8:1 light / 6.9:1 dark). **141 sites**
  were missing the dark half and **11 had the pair inverted** — `text-surface-400
  dark:text-surface-500`, which is the worst available combination at 2.6:1 and 3.7:1.
- `error.vue`'s status code was `text-surface-200 dark:text-surface-800`: 1.2:1 and 1.3:1, i.e.
  invisible rather than de-emphasized. Now surface-500 in both.
- What remains below AA is only where WCAG exempts it: disabled controls and decorative empty-state
  icons (`text-surface-300 dark:text-surface-600`, 17 sites).

### Verification

`pnpm nuxt typecheck`, 448 frontend specs, `go test ./...`, the integration suite, `pnpm build` and
`pnpm generate` all pass; the inline theme script was confirmed present at byte ~400 of the
generated `index.html`, ahead of the stylesheet and the bundle. **Not** verified: a live visual
pass in a browser — the changes were checked against the compiled CSS and the contrast maths.

Note the sketch below uses `.dark` and `[data-theme]`; the app is wired for **`.dark-mode`**.
Follow the code, not the sketch.

## Acceptance Criteria

### Theme Toggle
- [x] Theme toggle button in header/settings — `components/common/ThemeToggle.vue` in `AppHeader` and the auth layout, plus `pages/settings/appearance.vue`
- [x] Three options: Light, Dark, System (auto) — `THEME_OPTIONS` in `utils/theme.ts`, rendered as a `menuitemradio` group
- [x] System option follows OS preference — live, via a `matchMedia` listener held for the app's lifetime
- [x] Theme persists across sessions (localStorage) — key `calcard-theme`; an unreadable or junk value degrades to `system`
- [x] Smooth transition when switching themes — `.theme-switching` for 200ms, under `prefers-reduced-motion`; never on startup
- [x] No flash of wrong theme on page load — inline `app.head` script, verified present at byte ~400 of the generated `index.html`, ahead of both the stylesheet and the bundle

### Dark Mode Styles
- [x] All components properly styled for dark mode — 553 `dark:` utilities plus PrimeVue's dark tokens, both now keyed to `.dark-mode`
- [x] Calendar colors adjusted for dark backgrounds — `assets/css/fullcalendar.css`, which had never rendered before the selector fix
- [x] Form inputs visible and readable — PrimeVue design tokens, plus `color-scheme` for native controls
- [x] Sufficient contrast ratios (WCAG AA) — audited with WCAG maths over the real palette; 141 sites gained the missing dark half of the muted-text pair, 11 had it inverted, and the timegrid hour labels and the `error.vue` status code were fixed. What is still below AA is only WCAG-exempt (disabled controls, decorative empty-state icons)
- [x] Images/icons adapt or have dark variants — icons are PrimeIcons webfont glyphs and inherit `currentColor`. The app ships no raster UI chrome: `public/` holds only a favicon, and every `<img>` in the tree is a user-supplied contact photo
- [x] Scrollbars styled for dark mode — via `color-scheme` on the root, which is also what themes native controls and the canvas. No scrollbar CSS of our own

### Color Customization
- [x] Primary accent color customizable — one hex drives both Tailwind `primary-*` (via `--accent-<shade>` channel triplets, so opacity modifiers survive) and PrimeVue (`updatePrimaryPalette`)
- [x] Calendar colors visible in both themes — per-calendar colours are user-chosen and unaffected by the accent; the grid chrome around them is `fullcalendar.css`, which renders in dark for the first time
- [x] Color picker for accent color in settings — PrimeVue `ColorPicker` plus a hex field on `pages/settings/appearance.vue`, accepting `#abc`, `abc`, `#AABBCC` and expanding/normalizing client-side
- [x] Preset color options available — `ACCENT_PRESETS`, ten swatches as a `radiogroup` with `aria-checked`
- [x] Custom colors persist per user — server preference `accent_color`, pattern-validated and normalized to canonical lowercase hex. Applied optimistically with rollback if the server refuses, and cached device-locally as a paint hint so a reload never repaints

### Visual Consistency
- [x] Consistent shadows and elevation in dark mode — black low-alpha shadows are near-invisible on a dark surface, so `shadow-sm`, `shadow-lg` and PrimeVue's card get deeper alpha under `.dark-mode`
- [x] Border colors appropriate for each theme — `border-surface-200 dark:border-surface-800` throughout, which only started resolving once the surface scale existed
- [x] Focus states visible in both themes — a `:focus-visible` baseline drawn in the accent with an offset gap, so it survives both a dark surface and a light card
- [x] Selection highlights work in both themes — `::selection` in accent-200/surface-900 light and accent-800/surface-50 dark; the browser default had no guaranteed contrast on dark
- [x] Charts and graphs adapt to theme — vacuously: the app renders no charts

## Technical Details

### Theme Composable
```typescript
// composables/useTheme.ts
import { ref, computed, watch, onMounted } from 'vue'
import { useStorage, usePreferredDark } from '@vueuse/core'

export type ThemeMode = 'light' | 'dark' | 'system'

export function useTheme() {
  const prefersDark = usePreferredDark()
  const themeMode = useStorage<ThemeMode>('theme-mode', 'system')
  const accentColor = useStorage<string>('accent-color', '#3B82F6')

  const isDark = computed(() => {
    if (themeMode.value === 'system') {
      return prefersDark.value
    }
    return themeMode.value === 'dark'
  })

  const currentTheme = computed(() => isDark.value ? 'dark' : 'light')

  // Apply theme to document
  function applyTheme() {
    const root = document.documentElement

    if (isDark.value) {
      root.classList.add('dark')
      root.setAttribute('data-theme', 'dark')
    } else {
      root.classList.remove('dark')
      root.setAttribute('data-theme', 'light')
    }

    // Apply accent color
    root.style.setProperty('--primary-color', accentColor.value)
    root.style.setProperty('--primary-50', adjustColor(accentColor.value, 0.95))
    root.style.setProperty('--primary-100', adjustColor(accentColor.value, 0.9))
    root.style.setProperty('--primary-200', adjustColor(accentColor.value, 0.8))
    root.style.setProperty('--primary-300', adjustColor(accentColor.value, 0.6))
    root.style.setProperty('--primary-400', adjustColor(accentColor.value, 0.4))
    root.style.setProperty('--primary-500', accentColor.value)
    root.style.setProperty('--primary-600', adjustColor(accentColor.value, -0.1))
    root.style.setProperty('--primary-700', adjustColor(accentColor.value, -0.2))
    root.style.setProperty('--primary-800', adjustColor(accentColor.value, -0.3))
    root.style.setProperty('--primary-900', adjustColor(accentColor.value, -0.4))
  }

  // Color manipulation helper
  function adjustColor(hex: string, amount: number): string {
    const num = parseInt(hex.replace('#', ''), 16)
    const r = Math.min(255, Math.max(0, (num >> 16) + Math.round(255 * amount)))
    const g = Math.min(255, Math.max(0, ((num >> 8) & 0x00FF) + Math.round(255 * amount)))
    const b = Math.min(255, Math.max(0, (num & 0x0000FF) + Math.round(255 * amount)))
    return `#${(1 << 24 | r << 16 | g << 8 | b).toString(16).slice(1)}`
  }

  function setTheme(mode: ThemeMode) {
    themeMode.value = mode
  }

  function setAccentColor(color: string) {
    accentColor.value = color
    applyTheme()
  }

  function toggleTheme() {
    if (themeMode.value === 'light') {
      themeMode.value = 'dark'
    } else if (themeMode.value === 'dark') {
      themeMode.value = 'system'
    } else {
      themeMode.value = 'light'
    }
  }

  // Watch for changes
  watch([isDark, accentColor], applyTheme, { immediate: true })

  // Also watch system preference changes when in system mode
  watch(prefersDark, () => {
    if (themeMode.value === 'system') {
      applyTheme()
    }
  })

  onMounted(() => {
    applyTheme()
  })

  return {
    themeMode,
    isDark,
    currentTheme,
    accentColor,
    setTheme,
    setAccentColor,
    toggleTheme
  }
}
```

### Theme Toggle Component
```vue
<!-- components/common/ThemeToggle.vue -->
<template>
  <div class="theme-toggle">
    <Button
      :icon="themeIcon"
      text
      rounded
      v-tooltip.bottom="themeTooltip"
      @click="showMenu = !showMenu"
      aria-label="Toggle theme"
    />

    <Menu
      ref="menu"
      :model="themeOptions"
      :popup="true"
      v-model:visible="showMenu"
    >
      <template #item="{ item }">
        <div
          class="theme-option"
          :class="{ active: item.value === themeMode }"
          @click="selectTheme(item.value)"
        >
          <i :class="item.icon"></i>
          <span>{{ item.label }}</span>
          <i v-if="item.value === themeMode" class="pi pi-check"></i>
        </div>
      </template>
    </Menu>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useTheme, type ThemeMode } from '~/composables/useTheme'

const { themeMode, isDark, setTheme } = useTheme()

const showMenu = ref(false)

const themeIcon = computed(() => {
  if (themeMode.value === 'system') return 'pi pi-desktop'
  return isDark.value ? 'pi pi-moon' : 'pi pi-sun'
})

const themeTooltip = computed(() => {
  const modes: Record<ThemeMode, string> = {
    light: 'Light mode',
    dark: 'Dark mode',
    system: 'System preference'
  }
  return modes[themeMode.value]
})

const themeOptions = [
  { label: 'Light', value: 'light', icon: 'pi pi-sun' },
  { label: 'Dark', value: 'dark', icon: 'pi pi-moon' },
  { label: 'System', value: 'system', icon: 'pi pi-desktop' }
]

function selectTheme(mode: ThemeMode) {
  setTheme(mode)
  showMenu.value = false
}
</script>

<style scoped>
.theme-toggle {
  position: relative;
}

.theme-option {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
  cursor: pointer;
  transition: background-color 0.2s;
}

.theme-option:hover {
  background: var(--surface-hover);
}

.theme-option.active {
  color: var(--primary-color);
}

.theme-option i:last-child {
  margin-left: auto;
}
</style>
```

### Theme Settings Component
```vue
<!-- components/settings/ThemeSettings.vue -->
<template>
  <div class="theme-settings">
    <h3>Appearance</h3>

    <div class="setting-group">
      <label>Theme</label>
      <SelectButton
        v-model="themeMode"
        :options="themeOptions"
        optionLabel="label"
        optionValue="value"
        @change="onThemeChange"
      >
        <template #option="{ option }">
          <i :class="option.icon"></i>
          <span>{{ option.label }}</span>
        </template>
      </SelectButton>
    </div>

    <div class="setting-group">
      <label>Accent Color</label>
      <div class="color-options">
        <div
          v-for="color in presetColors"
          :key="color.value"
          class="color-swatch"
          :class="{ active: accentColor === color.value }"
          :style="{ backgroundColor: color.value }"
          :title="color.name"
          @click="setAccentColor(color.value)"
        >
          <i v-if="accentColor === color.value" class="pi pi-check"></i>
        </div>
        <div
          class="color-swatch custom"
          :class="{ active: isCustomColor }"
          @click="showColorPicker = true"
        >
          <i class="pi pi-palette"></i>
        </div>
      </div>
    </div>

    <div class="setting-group">
      <label>Preview</label>
      <div class="theme-preview" :class="{ dark: isDark }">
        <div class="preview-header">
          <div class="preview-dot" style="background: #ef4444"></div>
          <div class="preview-dot" style="background: #f59e0b"></div>
          <div class="preview-dot" style="background: #22c55e"></div>
        </div>
        <div class="preview-content">
          <div class="preview-sidebar">
            <div class="preview-item active"></div>
            <div class="preview-item"></div>
            <div class="preview-item"></div>
          </div>
          <div class="preview-main">
            <div class="preview-card"></div>
            <div class="preview-card"></div>
          </div>
        </div>
      </div>
    </div>

    <!-- Custom Color Picker Dialog -->
    <Dialog
      v-model:visible="showColorPicker"
      header="Choose Accent Color"
      :modal="true"
      :style="{ width: '320px' }"
    >
      <div class="color-picker-content">
        <ColorPicker v-model="customColor" inline />
        <InputText
          v-model="customColor"
          placeholder="#3B82F6"
          class="color-input"
        />
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" @click="showColorPicker = false" />
        <Button label="Apply" @click="applyCustomColor" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useTheme, type ThemeMode } from '~/composables/useTheme'

const {
  themeMode,
  isDark,
  accentColor,
  setTheme,
  setAccentColor
} = useTheme()

const showColorPicker = ref(false)
const customColor = ref(accentColor.value)

const themeOptions = [
  { label: 'Light', value: 'light', icon: 'pi pi-sun' },
  { label: 'Dark', value: 'dark', icon: 'pi pi-moon' },
  { label: 'System', value: 'system', icon: 'pi pi-desktop' }
]

const presetColors = [
  { name: 'Blue', value: '#3B82F6' },
  { name: 'Purple', value: '#8B5CF6' },
  { name: 'Pink', value: '#EC4899' },
  { name: 'Red', value: '#EF4444' },
  { name: 'Orange', value: '#F97316' },
  { name: 'Green', value: '#22C55E' },
  { name: 'Teal', value: '#14B8A6' },
  { name: 'Cyan', value: '#06B6D4' }
]

const isCustomColor = computed(() => {
  return !presetColors.some(c => c.value === accentColor.value)
})

function onThemeChange(event: { value: ThemeMode }) {
  setTheme(event.value)
}

function applyCustomColor() {
  if (customColor.value.match(/^#[0-9A-Fa-f]{6}$/)) {
    setAccentColor(customColor.value)
    showColorPicker.value = false
  }
}
</script>

<style scoped>
.theme-settings {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}

.setting-group {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.setting-group label {
  font-weight: 500;
  color: var(--text-color-secondary);
  font-size: 0.875rem;
}

.color-options {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.color-swatch {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: transform 0.2s, box-shadow 0.2s;
  border: 2px solid transparent;
}

.color-swatch:hover {
  transform: scale(1.1);
}

.color-swatch.active {
  border-color: var(--text-color);
  box-shadow: 0 0 0 2px var(--surface-card);
}

.color-swatch i {
  color: white;
  font-size: 0.875rem;
}

.color-swatch.custom {
  background: conic-gradient(
    red, yellow, lime, aqua, blue, magenta, red
  );
}

.color-swatch.custom i {
  background: var(--surface-card);
  border-radius: 50%;
  padding: 0.25rem;
  color: var(--text-color);
}

.theme-preview {
  background: #ffffff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  overflow: hidden;
  transition: all 0.3s;
}

.theme-preview.dark {
  background: #1f2937;
  border-color: #374151;
}

.preview-header {
  display: flex;
  gap: 0.375rem;
  padding: 0.5rem;
  background: #f3f4f6;
}

.dark .preview-header {
  background: #111827;
}

.preview-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.preview-content {
  display: flex;
  padding: 0.5rem;
  gap: 0.5rem;
  min-height: 100px;
}

.preview-sidebar {
  width: 40px;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.preview-item {
  height: 8px;
  background: #e5e7eb;
  border-radius: 2px;
}

.dark .preview-item {
  background: #374151;
}

.preview-item.active {
  background: var(--primary-color);
}

.preview-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.preview-card {
  flex: 1;
  background: #f9fafb;
  border-radius: 4px;
}

.dark .preview-card {
  background: #374151;
}

.color-picker-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1rem;
}

.color-input {
  width: 100%;
  text-align: center;
  font-family: monospace;
}
</style>
```

### Global Theme Styles
```css
/* assets/css/theme.css */

/* CSS Custom Properties for Light Theme */
:root {
  /* Surface colors */
  --surface-ground: #f8fafc;
  --surface-card: #ffffff;
  --surface-border: #e2e8f0;
  --surface-hover: #f1f5f9;
  --surface-overlay: rgba(0, 0, 0, 0.4);

  /* Text colors */
  --text-color: #1e293b;
  --text-color-secondary: #64748b;

  /* Primary colors (overridden by accent color) */
  --primary-color: #3B82F6;
  --primary-50: #eff6ff;
  --primary-100: #dbeafe;
  --primary-200: #bfdbfe;
  --primary-300: #93c5fd;
  --primary-400: #60a5fa;
  --primary-500: #3b82f6;
  --primary-600: #2563eb;
  --primary-700: #1d4ed8;
  --primary-800: #1e40af;
  --primary-900: #1e3a8a;

  /* Status colors */
  --green-500: #22c55e;
  --yellow-500: #eab308;
  --red-500: #ef4444;

  /* Shadows */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.05);
  --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.1);
  --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.1);

  /* Transitions */
  --transition-colors: color 0.2s, background-color 0.2s, border-color 0.2s;
}

/* CSS Custom Properties for Dark Theme */
:root.dark,
[data-theme="dark"] {
  /* Surface colors */
  --surface-ground: #0f172a;
  --surface-card: #1e293b;
  --surface-border: #334155;
  --surface-hover: #334155;
  --surface-overlay: rgba(0, 0, 0, 0.6);

  /* Text colors */
  --text-color: #f1f5f9;
  --text-color-secondary: #94a3b8;

  /* Adjust primary colors for dark mode visibility */
  --primary-50: #1e3a5f;
  --primary-100: #1e4076;

  /* Status colors (slightly adjusted for dark) */
  --green-500: #4ade80;
  --yellow-500: #facc15;
  --red-500: #f87171;

  /* Shadows (more subtle in dark mode) */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.2);
  --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.3);
  --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.4);
}

/* Smooth theme transitions */
* {
  transition: var(--transition-colors);
}

/* Prevent transition on page load */
.no-transition * {
  transition: none !important;
}

/* Scrollbar styling */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

::-webkit-scrollbar-track {
  background: var(--surface-ground);
}

::-webkit-scrollbar-thumb {
  background: var(--surface-border);
  border-radius: 4px;
}

::-webkit-scrollbar-thumb:hover {
  background: var(--text-color-secondary);
}

.dark ::-webkit-scrollbar-thumb {
  background: var(--surface-border);
}

/* Selection styling */
::selection {
  background: var(--primary-200);
  color: var(--primary-900);
}

.dark ::selection {
  background: var(--primary-700);
  color: var(--primary-100);
}

/* Focus visible styling */
:focus-visible {
  outline: 2px solid var(--primary-color);
  outline-offset: 2px;
}
```

### Nuxt Plugin for Theme Initialization
```typescript
// plugins/theme.client.ts
export default defineNuxtPlugin(() => {
  // Prevent flash of wrong theme
  const savedTheme = localStorage.getItem('theme-mode')
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches

  const shouldBeDark =
    savedTheme === 'dark' ||
    (savedTheme === 'system' && prefersDark) ||
    (!savedTheme && prefersDark)

  if (shouldBeDark) {
    document.documentElement.classList.add('dark')
    document.documentElement.setAttribute('data-theme', 'dark')
  }

  // Remove no-transition class after initial render
  requestAnimationFrame(() => {
    document.documentElement.classList.remove('no-transition')
  })
})
```

### Head Script for Flash Prevention
```html
<!-- In nuxt.config.ts or app.vue -->
<script>
  // Inline script to prevent theme flash
  (function() {
    document.documentElement.classList.add('no-transition');
    var theme = localStorage.getItem('theme-mode');
    var prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    if (theme === 'dark' || (theme === 'system' && prefersDark) || (!theme && prefersDark)) {
      document.documentElement.classList.add('dark');
      document.documentElement.setAttribute('data-theme', 'dark');
    }
  })();
</script>
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 038 (Settings Pages) - theme settings integration
- PrimeVue ColorPicker component
- @vueuse/core for usePreferredDark

## Estimation
- **Complexity:** Medium
- **Components:** 2 components, 1 composable, 1 plugin, CSS

## Notes
- Test all components in both themes before release
- Calendar event colors need special handling for dark mode
- Consider reduced motion preference for transitions
- PrimeVue has built-in dark mode support via theme switching
- Custom accent colors should maintain sufficient contrast
