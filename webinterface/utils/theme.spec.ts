// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { installMemoryStorage, installThrowingStorage } from '~/test/support/storage';
import {
  DARK_MODE_CLASS,
  DEFAULT_THEME_MODE,
  THEME_MODES,
  THEME_OPTIONS,
  THEME_STORAGE_KEY,
  applyResolvedTheme,
  isThemeMode,
  prefersDarkScheme,
  readStoredThemeMode,
  resolveTheme,
  writeStoredThemeMode,
} from './theme';

// SCOPE: the pure/DOM-poking half of story 046's theming. The reactive wiring is
// covered in composables/useTheme.spec.ts.
//
// The two behaviours worth pinning here are the ones a user would experience as
// a broken app rather than a wrong colour: a junk stored value must degrade to
// the default instead of leaving the document unstyled, and a localStorage that
// THROWS on access (Safari private mode, blocked cookies — it does not merely
// return null) must not take the theme down with it.

describe('theme constants', () => {
  it('offers exactly the three documented modes, system last', () => {
    expect([...THEME_MODES]).toEqual(['light', 'dark', 'system']);
    expect(DEFAULT_THEME_MODE).toBe('system');
  });

  it('keeps THEME_OPTIONS aligned with THEME_MODES', () => {
    // The toggle and the settings page both render THEME_OPTIONS, so a mode
    // added to one list and not the other would simply be unreachable in the UI.
    expect(THEME_OPTIONS.map(o => o.value)).toEqual([...THEME_MODES]);
    for (const option of THEME_OPTIONS) {
      expect(option.label).toBeTruthy();
      expect(option.icon).toMatch(/^pi pi-/);
      expect(option.hint).toBeTruthy();
    }
  });
});

describe('the dark-mode selector', () => {
  // The bug this guards against shipped silently for the whole life of the repo:
  // tailwind.config.ts had no `darkMode` key, so Tailwind defaulted to `media`
  // and its 550-odd `dark:` utilities followed the OS while PrimeVue followed
  // `.dark-mode`. An OS-dark user got dark Tailwind surfaces behind light
  // PrimeVue components, and no in-app toggle could have moved both halves.
  // Nothing else in the suite would notice it happening again.

  it('is what Tailwind is configured for', async () => {
    const config = (await import('~/tailwind.config')).default;
    expect(config.darkMode).toEqual(['selector', `.${DARK_MODE_CLASS}`]);
  });

  it('has a surface scale at all, wired to CSS variables', async () => {
    // `surface-*` is used ~440 times in the templates but was never a defined
    // Tailwind colour, so every one of those utilities compiled to NOTHING —
    // three quarters of the app's dark styling did not exist. Nothing else in
    // the suite would notice that happening again, because a missing colour
    // produces no error, just no CSS.
    const colors = (await import('~/tailwind.config')).default.theme?.extend?.colors as
      | Record<string, Record<string, string>>
      | undefined;

    for (const shade of [0, 50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950]) {
      expect(colors?.surface?.[shade], `surface-${shade}`)
        .toBe(`rgb(var(--surface-${shade}) / <alpha-value>)`);
    }
    for (const shade of [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950]) {
      // `<alpha-value>` is not cosmetic: bg-primary-900/20 and bg-surface-900/50
      // are both in use, and a bare var() would silently drop the opacity.
      expect(colors?.primary?.[shade], `primary-${shade}`)
        .toBe(`rgb(var(--accent-${shade}) / <alpha-value>)`);
    }
  });

  it('defines every one of those variables in theme.css', async () => {
    const { readFileSync } = await import('node:fs');
    const { join } = await import('node:path');
    const css = readFileSync(join(process.cwd(), 'assets/css/theme.css'), 'utf8');

    // A variable the config references but the stylesheet never defines is the
    // same failure as before — a utility that resolves to nothing.
    for (const shade of [0, 50, 500, 950]) {
      expect(css, `--surface-${shade}`).toContain(`--surface-${shade}:`);
    }
    for (const shade of [50, 500, 950]) {
      expect(css, `--accent-${shade}`).toContain(`--accent-${shade}:`);
    }
    // Surfaces swap neutral family with the theme (PrimeVue does the same).
    expect(css).toContain(`html.${DARK_MODE_CLASS}`);
  });

  it('is what PrimeVue and the FullCalendar overrides are written against', async () => {
    // Read as text rather than imported: nuxt.config.ts cannot be evaluated
    // here, and the CSS carries no exports. Vitest runs from webinterface/, and
    // `import.meta.url` is not a file: URL under the nuxt environment.
    const { readFileSync } = await import('node:fs');
    const { join } = await import('node:path');
    const root = process.cwd();

    expect(readFileSync(join(root, 'nuxt.config.ts'), 'utf8'))
      .toContain(`darkModeSelector: '.${DARK_MODE_CLASS}'`);
    // These used to target a bare `.dark`, which nothing in the app ever set, so
    // the dark calendar grid had never once rendered.
    expect(readFileSync(join(root, 'assets/css/fullcalendar.css'), 'utf8'))
      .toContain(`.${DARK_MODE_CLASS} .fc`);
  });
});

describe('isThemeMode', () => {
  it.each(['light', 'dark', 'system'])('accepts %s', (mode) => {
    expect(isThemeMode(mode)).toBe(true);
  });

  it.each([['Dark'], ['darkmode'], [''], [null], [undefined], [0], [{ mode: 'dark' }]])(
    'rejects %o',
    (value) => {
      expect(isThemeMode(value)).toBe(false);
    },
  );
});

describe('resolveTheme', () => {
  it('returns an explicit mode unchanged, whatever the OS says', () => {
    expect(resolveTheme('light', true)).toBe('light');
    expect(resolveTheme('dark', false)).toBe('dark');
  });

  it('defers to the OS under system', () => {
    expect(resolveTheme('system', true)).toBe('dark');
    expect(resolveTheme('system', false)).toBe('light');
  });
});

describe('prefersDarkScheme', () => {
  const original = Object.getOwnPropertyDescriptor(window, 'matchMedia');

  afterEach(() => {
    if (original) Object.defineProperty(window, 'matchMedia', original);
  });

  it('reports what matchMedia reports', () => {
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn(() => ({ matches: true })),
    });
    expect(prefersDarkScheme()).toBe(true);
  });

  it('assumes light when matchMedia is missing rather than throwing', () => {
    Object.defineProperty(window, 'matchMedia', { configurable: true, value: undefined });
    expect(prefersDarkScheme()).toBe(false);
  });
});

describe('readStoredThemeMode', () => {
  let storage: ReturnType<typeof installMemoryStorage>;

  beforeEach(() => {
    storage = installMemoryStorage();
  });

  afterEach(() => {
    storage.restore();
  });

  it('returns a valid stored mode', () => {
    storage.data.set(THEME_STORAGE_KEY, 'dark');
    expect(readStoredThemeMode()).toBe('dark');
  });

  it('falls back to the default when nothing is stored', () => {
    expect(readStoredThemeMode()).toBe(DEFAULT_THEME_MODE);
  });

  it('falls back to the default for a junk value', () => {
    // The key is user-writable (devtools, an older build). Trusting it would put
    // an unknown string on <html> and leave the app in neither theme.
    storage.data.set(THEME_STORAGE_KEY, 'midnight');
    expect(readStoredThemeMode()).toBe(DEFAULT_THEME_MODE);
  });

  it('falls back to the default when localStorage access throws', () => {
    const restore = installThrowingStorage();
    try {
      expect(readStoredThemeMode()).toBe(DEFAULT_THEME_MODE);
    } finally {
      restore();
    }
  });
});

describe('writeStoredThemeMode', () => {
  let storage: ReturnType<typeof installMemoryStorage>;

  beforeEach(() => {
    storage = installMemoryStorage();
  });

  afterEach(() => {
    storage.restore();
  });

  it('persists the mode', () => {
    writeStoredThemeMode('light');
    expect(storage.data.get(THEME_STORAGE_KEY)).toBe('light');
  });

  it('swallows a storage failure instead of throwing out of a click handler', () => {
    const restore = installThrowingStorage('QuotaExceededError');
    try {
      expect(() => writeStoredThemeMode('dark')).not.toThrow();
    } finally {
      restore();
    }
  });
});

describe('applyResolvedTheme', () => {
  afterEach(() => {
    document.documentElement.classList.remove(DARK_MODE_CLASS);
    document.documentElement.style.colorScheme = '';
  });

  it('adds the class and the colour scheme for dark', () => {
    applyResolvedTheme('dark');
    expect(document.documentElement.classList.contains(DARK_MODE_CLASS)).toBe(true);
    // color-scheme is what makes the browser's own chrome (scrollbars, native
    // controls, the canvas) follow — it is not decoration.
    expect(document.documentElement.style.colorScheme).toBe('dark');
  });

  it('removes the class again for light', () => {
    applyResolvedTheme('dark');
    applyResolvedTheme('light');
    expect(document.documentElement.classList.contains(DARK_MODE_CLASS)).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe('light');
  });

  it('is idempotent', () => {
    applyResolvedTheme('dark');
    applyResolvedTheme('dark');
    expect(document.documentElement.className.split(/\s+/).filter(c => c === DARK_MODE_CLASS))
      .toHaveLength(1);
  });
});
