/**
 * The one narrow-screen rule of the task dialogs and the undo toast: from a
 * viewport of 640px down they are the screen's width minus 1rem each side.
 * (ShareDialog keeps its own 95vw; the event dialogs have no breakpoint.)
 * Auto-imported.
 */
const NARROW_BREAKPOINT = '640px';
const NARROW_WIDTH = 'calc(100vw - 2rem)';

/** `:breakpoints` of a PrimeVue Dialog. */
export const DIALOG_BREAKPOINTS = { [NARROW_BREAKPOINT]: NARROW_WIDTH };

/** `:breakpoints` of a PrimeVue Toast (its entries are style objects). */
export const TOAST_BREAKPOINTS = { [NARROW_BREAKPOINT]: { width: NARROW_WIDTH } };
