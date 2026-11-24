/**
 * Parsing of text typed into a date field of the event form.
 *
 * Why the form parses typed text itself: PrimeVue's DatePicker (4.5.5) never
 * commits a typed value in 24-hour format. With the time shown its parser
 * reads `ampm.toLowerCase()` although no AM/PM is in the text, throws, and
 * swallows the error. The field then shows the typed date while the form still
 * holds the old one.
 *
 * Auto-imported by Nuxt (files under `utils/`) - call these without an import.
 */

/** The `date-format` of the event form's Start and End fields: `YYYY-MM-DD`. */
export const DATE_FIELD_FORMAT = 'yy-mm-dd';

const TYPED_DATE_TIME = /^(\d{4})-(\d{2})-(\d{2})\s+(\d{2}):(\d{2})(?:\s+([ap])m)?$/i;

/**
 * Parses `2026-12-01 09:30`, or `2026-12-01 09:30 PM` on the 12-hour clock,
 * into a local Date. Returns null for anything else.
 *
 * Every part must have its full width, exactly as the field displays it. That
 * is deliberate: once a typed value is complete the picker rewrites the field
 * text, so accepting `2026-12-1 09:30` would rewrite it to `2026-12-01 09:30`
 * while a day is still being corrected with Backspace, and the next digit
 * would land after the time.
 */
export function parseTypedDateTime(text: string, hourFormat: '12' | '24'): Date | null {
  const match = TYPED_DATE_TIME.exec(text.trim());
  if (!match) return null;

  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const minutes = Number(match[5]);
  let hours = Number(match[4]);
  const meridiem = match[6]?.toLowerCase();

  const date = new Date(year, month - 1, day);
  const isRealDate = date.getFullYear() === year && date.getMonth() === month - 1 && date.getDate() === day;
  if (!isRealDate) return null;

  if (meridiem) {
    if (hours > 12) return null;
    hours = (hours % 12) + (meridiem === 'p' ? 12 : 0);
  } else if (hourFormat === '12' || hours > 23) {
    return null;
  }
  if (minutes > 59) return null;

  date.setHours(hours, minutes, 0, 0);
  return date;
}

/** The part of a PrimeVue DatePicker that `keepFocusInTheField` works on. */
export interface DateFieldPicker {
  focused: boolean;
  preventFocus: boolean;
  initFocusableCell: () => void;
}

/**
 * Keeps the focus in the date field for as long as the field is focused.
 *
 * PrimeVue's DatePicker (4.5.5) moves the focus into its popup from a timer it
 * arms after each re-render. A flag is meant to stop that, but it covers only
 * the first timer: when the picker re-renders twice before that runs (the popup
 * opens on the first keystroke, the completed date changes the value), the
 * second one pulls the focus out of the field, and Enter and the keystrokes
 * after it go to the popup. Entering the popup on purpose (a click on a day,
 * Arrow Down) blurs the field first or bypasses this code, so with the field
 * focused the popup must never take the focus: hold the flag for as long as it
 * is.
 */
export function keepFocusInTheField(picker: DateFieldPicker | null | undefined): void {
  if (!picker) return;

  const initFocusableCell = picker.initFocusableCell;

  picker.initFocusableCell = function (this: DateFieldPicker) {
    if (this.focused) this.preventFocus = true;
    initFocusableCell.call(this);
  };
}
