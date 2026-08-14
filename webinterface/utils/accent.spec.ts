// @vitest-environment nuxt
import { describe, it, expect, afterEach } from 'vitest';
import {
  ACCENT_PRESETS,
  ACCENT_SHADES,
  DEFAULT_ACCENT_COLOR,
  DEFAULT_ACCENT_PALETTE,
  accentPalette,
  applyAccentColor,
  hexToRgbTriplet,
  isAccentColor,
  normalizeAccentColor,
} from './accent';

// SCOPE: the accent half of story 046 — parsing what a user types, generating a
// ramp from it, and getting that ramp onto the document in the two forms the two
// colour systems need (CSS channel triplets for Tailwind, hex for PrimeVue).
//
// The load-bearing detail is the TRIPLET form: Tailwind's config wraps these as
// `rgb(var(--accent-500) / <alpha-value>)`, so a value written as `#3b82f6`
// instead of `59 130 246` would silently break every `bg-primary-900/20` in the
// app while leaving the solid colours looking fine.

const readVar = (shade: number) =>
  document.documentElement.style.getPropertyValue(`--accent-${shade}`);

afterEach(() => {
  for (const shade of ACCENT_SHADES) {
    document.documentElement.style.removeProperty(`--accent-${shade}`);
  }
});

describe('isAccentColor', () => {
  it.each(['#3b82f6', '#000000', '#ffffff'])('accepts the canonical form %s', (hex) => {
    expect(isAccentColor(hex)).toBe(true);
  });

  it.each([
    '#3B82F6', // uppercase is not canonical — normalize first
    '#abc', // shorthand is not canonical either
    '3b82f6',
    '#3b82fg',
    'rebeccapurple',
    '',
    null,
    undefined,
    123,
  ])('rejects %o', (value) => {
    expect(isAccentColor(value)).toBe(false);
  });
});

describe('normalizeAccentColor', () => {
  it('passes a canonical value through', () => {
    expect(normalizeAccentColor('#3b82f6')).toBe('#3b82f6');
  });

  it('lowercases, trims and adds a missing hash', () => {
    expect(normalizeAccentColor('  #8B5CF6 ')).toBe('#8b5cf6');
    expect(normalizeAccentColor('8B5CF6')).toBe('#8b5cf6');
  });

  it('expands three-digit shorthand', () => {
    // The server only accepts six digits, so this has to happen client-side or
    // typing "fff" would come back as a 400.
    expect(normalizeAccentColor('#fff')).toBe('#ffffff');
    expect(normalizeAccentColor('abc')).toBe('#aabbcc');
  });

  it.each(['', 'rebeccapurple', '#12345', '#1234567', 'rgb(1,2,3)', '#12g456'])(
    'returns null for %o rather than guessing',
    (input) => {
      expect(normalizeAccentColor(input)).toBeNull();
    },
  );

  it('always produces something isAccentColor accepts', () => {
    for (const input of ['#FFF', 'aabbcc', '  #3B82F6  ', '#8b5cf6']) {
      const out = normalizeAccentColor(input);
      expect(out).not.toBeNull();
      expect(isAccentColor(out)).toBe(true);
    }
  });
});

describe('hexToRgbTriplet', () => {
  it('produces space-separated channels, not a colour', () => {
    // Space-separated because that is what `rgb(var(--x) / <alpha-value>)`
    // expects; commas would break the alpha slash syntax.
    expect(hexToRgbTriplet('#3b82f6')).toBe('59 130 246');
    expect(hexToRgbTriplet('#000000')).toBe('0 0 0');
    expect(hexToRgbTriplet('#ffffff')).toBe('255 255 255');
  });
});

describe('accentPalette', () => {
  it('returns Tailwind blue verbatim for the default', () => {
    // Generating the default would restyle every existing screen for users who
    // never touch the setting: palette() puts blue-700 at #295bac against
    // Tailwind's #1d4ed8.
    expect(accentPalette(DEFAULT_ACCENT_COLOR)).toEqual(DEFAULT_ACCENT_PALETTE);
    expect(accentPalette(DEFAULT_ACCENT_COLOR)[700]).toBe('#1d4ed8');
  });

  it('hands back a copy, not the shared default object', () => {
    const ramp = accentPalette(DEFAULT_ACCENT_COLOR);
    ramp[500] = '#000000';
    expect(DEFAULT_ACCENT_PALETTE[500]).toBe(DEFAULT_ACCENT_COLOR);
  });

  it('generates a full ramp for a custom colour, anchored at 500', () => {
    const ramp = accentPalette('#8b5cf6');
    for (const shade of ACCENT_SHADES) {
      expect(ramp[shade], `shade ${shade}`).toMatch(/^#[0-9a-f]{6}$/i);
    }
    expect(ramp[500]).toBe('#8b5cf6');
  });

  it('ramps monotonically from light to dark', () => {
    const ramp = accentPalette('#16a34a');
    const brightness = (hex: string) =>
      [1, 3, 5].reduce((sum, i) => sum + Number.parseInt(hex.slice(i, i + 2), 16), 0);
    const values = ACCENT_SHADES.map(s => brightness(ramp[s]));
    for (let i = 1; i < values.length; i++) {
      expect(values[i]!, `shade ${ACCENT_SHADES[i]} vs ${ACCENT_SHADES[i - 1]}`)
        .toBeLessThan(values[i - 1]!);
    }
  });
});

describe('ACCENT_PRESETS', () => {
  it('are all canonical, unique, and lead with the default', () => {
    // The swatch row compares preset.value against the applied colour to draw
    // the tick, so a non-canonical preset would render as permanently unselected.
    for (const preset of ACCENT_PRESETS) {
      expect(isAccentColor(preset.value), `${preset.name} ${preset.value}`).toBe(true);
      expect(preset.name).toBeTruthy();
    }
    expect(ACCENT_PRESETS[0]!.value).toBe(DEFAULT_ACCENT_COLOR);
    expect(new Set(ACCENT_PRESETS.map(p => p.value)).size).toBe(ACCENT_PRESETS.length);
  });
});

describe('applyAccentColor', () => {
  it('writes every shade as a triplet on the root element', () => {
    applyAccentColor('#8b5cf6');
    for (const shade of ACCENT_SHADES) {
      expect(readVar(shade), `shade ${shade}`).toMatch(/^\d{1,3} \d{1,3} \d{1,3}$/);
    }
    expect(readVar(500)).toBe('139 92 246');
  });

  it('applies the exact Tailwind blue for the default', () => {
    applyAccentColor(DEFAULT_ACCENT_COLOR);
    expect(readVar(500)).toBe('59 130 246');
    expect(readVar(700)).toBe('29 78 216');
  });

  it('replaces the previous accent rather than layering on it', () => {
    applyAccentColor('#8b5cf6');
    applyAccentColor('#16a34a');
    expect(readVar(500)).toBe('22 163 74');
  });

  it.each(['not-a-colour', '#3b82f', '', '#3b82f6; background: url(x)'])(
    'ignores %o instead of writing a broken ramp',
    (bad) => {
      applyAccentColor(DEFAULT_ACCENT_COLOR);
      applyAccentColor(bad);
      // A junk value must leave the last good ramp standing — the alternative is
      // an app with no primary colour at all.
      expect(readVar(500)).toBe('59 130 246');
    },
  );
});
