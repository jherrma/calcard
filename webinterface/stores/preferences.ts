import type { PreferencesResponse, TimeFormat } from '~/types/settings';

// Keys, defaults and allowed values mirror server/internal/domain/user/preference.go.
// The backend rejects anything outside those sets with 400, so the two lists must
// stay in sync (story 103).
export const PREF_DEFAULT_EVENT_DURATION = 'default_event_duration';
export const PREF_DEFAULT_ALL_DAY = 'default_all_day';
export const PREF_TIME_FORMAT = 'time_format';
export const PREF_ACCENT_COLOR = 'accent_color';

export const DEFAULT_PREFERENCES: Record<string, string> = {
  [PREF_DEFAULT_EVENT_DURATION]: '60',
  [PREF_DEFAULT_ALL_DAY]: 'false',
  [PREF_TIME_FORMAT]: '24h',
  [PREF_ACCENT_COLOR]: DEFAULT_ACCENT_COLOR,
};

/** Durations offered by the settings UI, in minutes. */
export const EVENT_DURATION_OPTIONS: { label: string; value: number }[] = [
  { label: '15 minutes', value: 15 },
  { label: '30 minutes', value: 30 },
  { label: '45 minutes', value: 45 },
  { label: '1 hour', value: 60 },
  { label: '1.5 hours', value: 90 },
  { label: '2 hours', value: 120 },
  { label: '3 hours', value: 180 },
  { label: '4 hours', value: 240 },
  { label: '8 hours', value: 480 },
];

const ALLOWED_DURATIONS = EVENT_DURATION_OPTIONS.map(o => o.value);
const FALLBACK_DURATION = 60;

interface PreferencesState {
  preferences: Record<string, string>;
  isLoaded: boolean;
  isLoading: boolean;
  error: string | null;
}

// Single-flight guard for ensureLoaded(). The calendar page and the settings page
// can both ask on the same tick; without this, two GETs would race to fill the
// same state. Module scope (not state) because a Promise has no business being
// reactive; it is always cleared in the chain's finally.
let inflight: Promise<void> | null = null;

// Bumped by reset() (i.e. on logout). fetchPreferences captures it before its
// await and refuses to write state if it changed meanwhile, so a GET issued for
// the PREVIOUS session can never land in the next one's store.
let generation = 0;

export const usePreferencesStore = defineStore('preferences', {
  state: (): PreferencesState => ({
    // Start from the defaults so anything reading a getter before the fetch
    // resolves (e.g. an EventForm opened very early) still gets sane values.
    preferences: { ...DEFAULT_PREFERENCES },
    isLoaded: false,
    isLoading: false,
    error: null,
  }),

  getters: {
    defaultEventDuration(state: PreferencesState): number {
      const parsed = Number.parseInt(state.preferences[PREF_DEFAULT_EVENT_DURATION] ?? '', 10);
      // Fall back rather than trust: a value written by an older build (or edited
      // straight in the DB) must not turn into a nonsense event length.
      return ALLOWED_DURATIONS.includes(parsed) ? parsed : FALLBACK_DURATION;
    },

    defaultAllDay(state: PreferencesState): boolean {
      return state.preferences[PREF_DEFAULT_ALL_DAY] === 'true';
    },

    timeFormat(state: PreferencesState): TimeFormat {
      return state.preferences[PREF_TIME_FORMAT] === '12h' ? '12h' : '24h';
    },

    /**
     * The UI accent (story 046). Falls back rather than trusts, for the same
     * reason `defaultEventDuration` does: the value is interpolated straight
     * into CSS custom properties, and a row written by an older build — or by
     * hand — must not be able to leave the app without a primary colour.
     */
    accentColor(state: PreferencesState): string {
      const stored = state.preferences[PREF_ACCENT_COLOR];
      return isAccentColor(stored) ? stored : DEFAULT_ACCENT_COLOR;
    },
  },

  actions: {
    async fetchPreferences() {
      const api = useApi();
      const gen = generation;
      this.isLoading = true;
      this.error = null;
      try {
        const data = await api<PreferencesResponse>('/api/v1/users/me/preferences');
        // A reset() (logout) while this was in flight means the response belongs
        // to a session that no longer exists — drop it rather than seed the next
        // user's store with it.
        if (gen !== generation) return;
        // Merge onto the defaults: the server already fills every known key, but
        // a client that is ahead of the server must not end up with holes.
        this.preferences = { ...DEFAULT_PREFERENCES, ...(data?.preferences ?? {}) };
        this.isLoaded = true;
      } catch (e: any) {
        // Still rethrows for a stale generation (the caller asked, so it gets an
        // answer), but the state it would have written is no longer ours.
        if (gen === generation) {
          this.error = e?.data?.message || 'Failed to load preferences';
        }
        throw e;
      } finally {
        if (gen === generation) this.isLoading = false;
      }
    },

    /**
     * Load preferences once per session. Never rejects: preferences are a
     * convenience, so a failed load leaves the defaults in place (with `error`
     * recording why) instead of breaking the calendar page's mount.
     */
    async ensureLoaded() {
      if (this.isLoaded) return;
      if (!inflight) {
        const own: Promise<void> = this.fetchPreferences()
          .catch(() => {})
          .finally(() => {
            // Only clear our own handle: a reset() may already have dropped it
            // and a later caller installed a fresh one for the new session.
            if (inflight === own) inflight = null;
          });
        inflight = own;
      }
      await inflight;
    },

    /**
     * Drop the cached map and the once-per-session latch. Called from
     * authStore.clearAuth(): logout and login are both client-side navigations,
     * so nothing ever reloads the page and Pinia state would otherwise survive
     * into the NEXT user's session — who would then be shown (and, because
     * isDirty compares against the store, be unable to correct) the previous
     * user's settings on /settings/calendar (story 103 review).
     */
    reset() {
      generation++;
      inflight = null;
      this.preferences = { ...DEFAULT_PREFERENCES };
      this.isLoaded = false;
      this.isLoading = false;
      this.error = null;
    },

    async updatePreferences(prefs: Record<string, string>) {
      const api = useApi();
      const data = await api<PreferencesResponse>('/api/v1/users/me/preferences', {
        method: 'PATCH',
        body: { preferences: prefs },
      });
      // The PATCH response is the full map, so adopt it wholesale rather than
      // patching local state — that keeps a rejected-but-retried key honest.
      this.preferences = { ...DEFAULT_PREFERENCES, ...(data?.preferences ?? {}) };
      this.isLoaded = true;
    },
  },
});
