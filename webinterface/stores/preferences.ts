import type { PreferencesResponse, TimeFormat } from '~/types/settings';

// Keys, defaults and allowed values mirror server/internal/domain/user/preference.go.
// The backend rejects anything outside those sets with 400, so the two lists must
// stay in sync (story 103).
export const PREF_DEFAULT_EVENT_DURATION = 'default_event_duration';
export const PREF_DEFAULT_ALL_DAY = 'default_all_day';
export const PREF_TIME_FORMAT = 'time_format';

export const DEFAULT_PREFERENCES: Record<string, string> = {
  [PREF_DEFAULT_EVENT_DURATION]: '60',
  [PREF_DEFAULT_ALL_DAY]: 'false',
  [PREF_TIME_FORMAT]: '24h',
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
  },

  actions: {
    async fetchPreferences() {
      const api = useApi();
      this.isLoading = true;
      this.error = null;
      try {
        const data = await api<PreferencesResponse>('/api/v1/users/me/preferences');
        // Merge onto the defaults: the server already fills every known key, but
        // a client that is ahead of the server must not end up with holes.
        this.preferences = { ...DEFAULT_PREFERENCES, ...(data?.preferences ?? {}) };
        this.isLoaded = true;
      } catch (e: any) {
        this.error = e?.data?.message || 'Failed to load preferences';
        throw e;
      } finally {
        this.isLoading = false;
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
        inflight = this.fetchPreferences()
          .catch(() => {})
          .finally(() => {
            inflight = null;
          });
      }
      await inflight;
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
