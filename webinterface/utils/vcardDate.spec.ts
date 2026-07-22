import { describe, it, expect } from 'vitest';
import { normalizeVCardDate } from './vcardDate';

// SCOPE: normalizeVCardDate — the pure helper that feeds ContactForm.vue's
// birthday DatePicker model (issue #24). A DAV-created contact can carry a
// compact RFC 6350 basic-form BDAY `19850412` (no dashes). Before the fix the
// form did `new Date('19850412T00:00:00')` → Invalid Date, and on save the
// formatter emitted `NaN-NaN-NaN`, corrupting the BDAY. These tests pin the
// normalization contract that prevents that.

describe('normalizeVCardDate', () => {
  it('converts the compact basic form YYYYMMDD to extended YYYY-MM-DD (issue #24)', () => {
    // Revert proof: pre-fix, `19850412` was fed straight into new Date(...),
    // producing an Invalid Date → NaN. This assertion fails without the helper.
    expect(normalizeVCardDate('19850412')).toBe('1985-04-12');
  });

  it('passes an already-valid extended form YYYY-MM-DD through unchanged', () => {
    expect(normalizeVCardDate('1985-04-12')).toBe('1985-04-12');
  });

  it('produces a value that builds a real (non-NaN) Date for the compact form', () => {
    const iso = normalizeVCardDate('19850412');
    const d = new Date(iso + 'T00:00:00');
    expect(Number.isNaN(d.getTime())).toBe(false);
    expect(d.getFullYear()).toBe(1985);
    expect(d.getMonth()).toBe(3); // April (0-indexed)
    expect(d.getDate()).toBe(12);
  });

  it('returns an empty string for empty, undefined, null, or whitespace input (no NaN)', () => {
    expect(normalizeVCardDate('')).toBe('');
    expect(normalizeVCardDate(undefined)).toBe('');
    expect(normalizeVCardDate(null)).toBe('');
    expect(normalizeVCardDate('   ')).toBe('');
  });

  it('returns an empty string (and does not throw) for an obviously bad value', () => {
    expect(() => normalizeVCardDate('not-a-date')).not.toThrow();
    expect(normalizeVCardDate('not-a-date')).toBe('');
    expect(normalizeVCardDate('garbage')).toBe('');
  });

  it('tolerates a trailing time component, keeping only the date part', () => {
    expect(normalizeVCardDate('1985-04-12T00:00:00')).toBe('1985-04-12');
  });
});
