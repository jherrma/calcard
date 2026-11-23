import { describe, it, expect } from 'vitest';
import { parseTypedDateTime } from './dateFieldInput';

const local = (year: number, month: number, day: number, hours: number, minutes: number) =>
  new Date(year, month - 1, day, hours, minutes, 0, 0);

describe('parseTypedDateTime', () => {
  it('reads the 24-hour format the field displays', () => {
    expect(parseTypedDateTime('2026-12-01 09:30', '24')).toEqual(local(2026, 12, 1, 9, 30));
    expect(parseTypedDateTime('  2026-12-01 23:59 ', '24')).toEqual(local(2026, 12, 1, 23, 59));
  });

  it('reads AM and PM in any letter case', () => {
    expect(parseTypedDateTime('2026-12-01 09:30 PM', '12')).toEqual(local(2026, 12, 1, 21, 30));
    expect(parseTypedDateTime('2026-12-01 12:05 am', '12')).toEqual(local(2026, 12, 1, 0, 5));
    expect(parseTypedDateTime('2026-12-01 12:05 Pm', '12')).toEqual(local(2026, 12, 1, 12, 5));
    expect(parseTypedDateTime('2026-12-01 09:30 PM', '24')).toEqual(local(2026, 12, 1, 21, 30));
  });

  it('needs AM or PM on the 12-hour clock', () => {
    expect(parseTypedDateTime('2026-12-01 09:30', '12')).toBeNull();
  });

  it('needs every part at full width, as the field displays it', () => {
    expect(parseTypedDateTime('2026-12-1 09:30', '24')).toBeNull();
    expect(parseTypedDateTime('2026-1-01 09:30', '24')).toBeNull();
    expect(parseTypedDateTime('2026-12-01 9:30', '24')).toBeNull();
    expect(parseTypedDateTime('2026-12-01 09:3', '24')).toBeNull();
  });

  it('refuses anything that is not that format', () => {
    const rejected = ['', '   ', 'tomorrow', '01.12.2026 09:30', '2026-12-01', '2026-12-01 09:30:15', '09:30', '1pm'];

    for (const text of rejected) {
      expect(parseTypedDateTime(text, '24'), text).toBeNull();
    }
  });

  it('refuses a date or a time that does not exist', () => {
    expect(parseTypedDateTime('2026-02-30 10:00', '24')).toBeNull();
    expect(parseTypedDateTime('2026-13-01 10:00', '24')).toBeNull();
    expect(parseTypedDateTime('0099-12-01 10:00', '24')).toBeNull();
    expect(parseTypedDateTime('2026-12-01 24:00', '24')).toBeNull();
    expect(parseTypedDateTime('2026-12-01 09:60', '24')).toBeNull();
    expect(parseTypedDateTime('2026-12-01 13:30 PM', '12')).toBeNull();
  });
});
