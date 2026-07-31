import type { Calendar, CalendarEvent, EventFormData } from '~/types/calendar';

// Format a Date as RFC 3339 with the local timezone offset (e.g. 2026-02-09T11:00:00+01:00).
// Unlike toISOString() which converts to UTC, this preserves the user's local time so
// the backend can attach the correct IANA timezone via time.In(loc).
export function toRFC3339(d: Date): string {
  const pad = (n: number) => n.toString().padStart(2, '0');
  const offset = -d.getTimezoneOffset();
  const sign = offset >= 0 ? '+' : '-';
  const absOffset = Math.abs(offset);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` +
    `${sign}${pad(Math.floor(absOffset / 60))}:${pad(absOffset % 60)}`;
}

interface CalendarState {
  calendars: Calendar[];
  events: CalendarEvent[];
  visibleCalendarIds: Set<string>;
  isLoading: boolean;
  error: string | null;
  currentView: 'dayGridMonth' | 'timeGridWeek' | 'timeGridDay';
  currentDate: Date;
}

export const useCalendarStore = defineStore('calendars', {
  state: (): CalendarState => ({
    calendars: [],
    events: [],
    visibleCalendarIds: new Set(),
    isLoading: false,
    error: null,
    currentView: 'dayGridMonth',
    currentDate: new Date(),
  }),

  getters: {
    visibleEvents(state: CalendarState) {
      return state.events.filter((e: CalendarEvent) => state.visibleCalendarIds.has(String(e.calendar_id)));
    },

    calendarOptions(state: CalendarState) {
      return state.calendars.map((cal: Calendar) => ({
        ...cal,
        visible: state.visibleCalendarIds.has(String(cal.id)),
      }));
    },

    ownedCalendars(state: CalendarState) {
      return state.calendars.filter((c: Calendar) => !c.shared);
    },

    sharedCalendars(state: CalendarState) {
      return state.calendars.filter((c: Calendar) => c.shared);
    },

    writableCalendars(state: CalendarState) {
      return state.calendars.filter((c: Calendar) => !c.shared || c.permission === 'read-write');
    },
  },

  actions: {
    async fetchCalendars() {
      const api = useApi();
      try {
        // A REFETCH must not clobber the sidebar filter (story 043 review): the
        // share dialog refetches the list after every share mutation, and the
        // old "select all" reset silently re-checked calendars the user had
        // hidden — their events flooded back into the view mid-dialog. So: first
        // load still shows everything, a refetch keeps each already-known
        // calendar's state and defaults calendars we have not seen before
        // (e.g. one just shared with us) to visible.
        const knownIds = new Set(this.calendars.map((c: Calendar) => String(c.id)));
        const previouslyVisible = this.visibleCalendarIds;
        const isRefetch = knownIds.size > 0;

        const response = await api<{ calendars: Calendar[] }>('/api/v1/calendars');
        this.calendars = response.calendars || [];

        this.visibleCalendarIds = new Set(
          this.calendars
            .map((c: Calendar) => String(c.id))
            .filter((id: string) => !isRefetch || !knownIds.has(id) || previouslyVisible.has(id)),
        );
      } catch (e: unknown) {
        this.error = (e as Error).message || 'Failed to load calendars';
      }
    },

    async fetchEvents(start: Date, end: Date) {
      this.isLoading = true;
      this.error = null;

      try {
        const api = useApi();

        // Fetch every calendar's events IN PARALLEL (was N sequential round-trips,
        // which visibly lagged month navigation with several calendars). All
        // calendars are fetched — not just visible ones — so toggling visibility
        // doesn't require a refetch. allSettled preserves the previous
        // continue-on-error behaviour: one failing calendar doesn't block others.
        const results = await Promise.allSettled(
          this.calendars.map((c: Calendar) =>
            api<{ events: CalendarEvent[] }>(
              `/api/v1/calendars/${c.uuid}/events?start=${start.toISOString()}&end=${end.toISOString()}`
            )
          )
        );

        const allEvents: CalendarEvent[] = [];
        results.forEach((r, i) => {
          if (r.status === 'fulfilled') {
            if (r.value.events) allEvents.push(...r.value.events);
          } else {
            console.warn(`Failed to load events for calendar ${this.calendars[i]?.id}`, r.reason);
          }
        });

        this.events = allEvents;
      } catch (e: unknown) {
        this.error = (e as Error).message || 'Failed to load events';
      } finally {
        this.isLoading = false;
      }
    },

    toggleCalendarVisibility(calendarId: string) {
      const id = String(calendarId);
      if (this.visibleCalendarIds.has(id)) {
        this.visibleCalendarIds.delete(id);
      } else {
        this.visibleCalendarIds.add(id);
      }
    },

    // Map a calendar's numeric id (what event objects and the sidebar carry) to
    // its UUID, the canonical external identifier the API now expects on
    // /calendars/:id routes (#52). Callers still pass the numeric id; the store
    // resolves it here so there's a single translation point. Falls back to the
    // given value if the calendar isn't loaded (keeps the URL well-formed).
    calendarUuid(id: string | number): string {
      return (
        this.calendars.find((c: Calendar) => String(c.id) === String(id))?.uuid ?? String(id)
      );
    },

    async createEvent(calendarId: string, data: EventFormData) {
      const api = useApi();
      const body: Record<string, unknown> = {
        summary: data.summary,
        description: data.description,
        location: data.location,
        start: toRFC3339(data.start),
        end: toRFC3339(data.end),
        timezone: data.timezone,
        all_day: data.all_day,
      };
      if (data.recurrence) {
        body.recurrence = data.recurrence;
      }

      const response = await api<CalendarEvent>(`/api/v1/calendars/${this.calendarUuid(calendarId)}/events`, {
        method: 'POST',
        body,
      });

      this.events.push(response);
      return response;
    },

    async getEvent(calendarId: string, eventId: string) {
      const api = useApi();
      return await api<CalendarEvent>(`/api/v1/calendars/${this.calendarUuid(calendarId)}/events/${eventId}`);
    },

    /**
     * Resolve ONE occurrence of a (possibly recurring) event on a given day.
     *
     * GET /calendars/:uuid/events/:id returns the stored MASTER event and ignores
     * any recurrence hint — so it cannot answer "the 15 September instance of this
     * weekly series". The list endpoint does expand recurrences server-side, so ask
     * it for the single day the occurrence falls on and pick the matching
     * RECURRENCE-ID. Used by the global-search deep link (story 044), which can
     * point at a date the calendar page has not loaded.
     *
     * Deliberately does NOT touch `events`: the page's loaded range belongs to
     * whatever FullCalendar is showing, and this is a one-off lookup.
     */
    async fetchEventOccurrence(
      calendarId: string,
      eventId: string,
      recurrenceId: string,
      day: Date
    ): Promise<CalendarEvent | null> {
      const api = useApi();
      // Local-midnight bounds ±1 day: an occurrence stored in another timezone can
      // land just outside the local day, and over-fetching one day is cheap.
      const start = new Date(day.getFullYear(), day.getMonth(), day.getDate() - 1);
      const end = new Date(day.getFullYear(), day.getMonth(), day.getDate() + 2);
      const response = await api<{ events: CalendarEvent[] }>(
        `/api/v1/calendars/${this.calendarUuid(calendarId)}/events?start=${start.toISOString()}&end=${end.toISOString()}`
      );
      return (
        (response.events || []).find(
          (e: CalendarEvent) => e.id === eventId && (e.recurrence_id || '') === recurrenceId
        ) ?? null
      );
    },

    async updateEvent(calendarId: string, eventId: string, data: EventFormData, scope?: string, recurrenceId?: string) {
      const api = useApi();
      const body: Record<string, unknown> = {
        summary: data.summary,
        description: data.description,
        location: data.location,
        start: toRFC3339(data.start),
        end: toRFC3339(data.end),
        timezone: data.timezone,
        all_day: data.all_day,
      };
      if (data.recurrence) {
        body.recurrence = data.recurrence;
      }

      let url = `/api/v1/calendars/${this.calendarUuid(calendarId)}/events/${eventId}`;
      const params = new URLSearchParams();
      if (scope) params.set('scope', scope);
      if (recurrenceId) params.set('recurrence_id', recurrenceId);
      if (params.toString()) url += `?${params.toString()}`;

      const response = await api<CalendarEvent>(url, {
        method: 'PATCH',
        body,
      });

      // For recurring mutations, the caller should refetch events
      if (!scope || scope === 'all') {
        const idx = this.events.findIndex((e: CalendarEvent) => e.id === eventId);
        if (idx !== -1) {
          this.events[idx] = response;
        }
      }

      return response;
    },

    async moveEvent(calendarId: string, eventId: string, targetCalendarId: string) {
      const api = useApi();
      // Both the source (path) and target (body) are calendar UUIDs now (#52).
      await api(`/api/v1/calendars/${this.calendarUuid(calendarId)}/events/${eventId}/move`, {
        method: 'POST',
        body: { target_calendar_id: this.calendarUuid(targetCalendarId) },
      });
    },

    async deleteEvent(calendarId: string, eventId: string, scope?: string, recurrenceId?: string) {
      const api = useApi();

      let url = `/api/v1/calendars/${this.calendarUuid(calendarId)}/events/${eventId}`;
      const params = new URLSearchParams();
      if (scope) params.set('scope', scope);
      if (recurrenceId) params.set('recurrence_id', recurrenceId);
      if (params.toString()) url += `?${params.toString()}`;

      await api(url, { method: 'DELETE' });

      // Remove from local state
      this.events = this.events.filter((e: CalendarEvent) => e.id !== eventId);
    },

    async updateEventTime(eventId: string, calendarId: string, start: Date, end: Date, scope?: string, recurrenceId?: string) {
      const api = useApi();

      // Mirror updateEvent: without scope the backend defaults to scope=all and
      // rewrites the whole series' DTSTART/DTEND. For a dragged/resized single
      // occurrence, callers pass scope='this' + recurrence_id so only that
      // occurrence moves (as a RECURRENCE-ID exception).
      let url = `/api/v1/calendars/${this.calendarUuid(calendarId)}/events/${eventId}`;
      const params = new URLSearchParams();
      if (scope) params.set('scope', scope);
      if (recurrenceId) params.set('recurrence_id', recurrenceId);
      if (params.toString()) url += `?${params.toString()}`;

      await api(url, {
        method: 'PATCH',
        body: {
          start: toRFC3339(start),
          end: toRFC3339(end),
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        },
      });

      // For scoped (recurring) mutations the server changes what the series
      // returns, so the caller must refetch the visible range; only patch local
      // state for an unscoped/whole-series update.
      if (!scope || scope === 'all') {
        const event = this.events.find((e: CalendarEvent) => e.id === eventId);
        if (event) {
          event.start = toRFC3339(start);
          event.end = toRFC3339(end);
        }
      }
    },

    async createCalendar(data: { name: string; color: string; timezone: string; description: string }) {
      const api = useApi();
      const response = await api<Calendar>('/api/v1/calendars', {
        method: 'POST',
        body: data,
      });
      this.calendars.push(response);
      this.visibleCalendarIds.add(String(response.id));
      return response;
    },

    async updateCalendar(calendarUuid: string, data: { name?: string; color?: string; timezone?: string; description?: string }) {
      const api = useApi();
      const response = await api<Calendar>(`/api/v1/calendars/${calendarUuid}`, {
        method: 'PATCH',
        body: data,
      });
      const idx = this.calendars.findIndex((c: Calendar) => c.uuid === calendarUuid);
      if (idx >= 0) {
        this.calendars[idx] = response;
      }
      return response;
    },

    async deleteCalendar(calendarUuid: string) {
      const api = useApi();
      const cal = this.calendars.find((c: Calendar) => c.uuid === calendarUuid);
      await api(`/api/v1/calendars/${calendarUuid}`, {
        method: 'DELETE',
        body: { confirmation: 'DELETE' },
      });
      this.calendars = this.calendars.filter((c: Calendar) => c.uuid !== calendarUuid);
      if (cal) {
        this.visibleCalendarIds.delete(String(cal.id));
        this.events = this.events.filter((e: CalendarEvent) => String(e.calendar_id) !== String(cal.id));
      }
    },

    setCurrentView(view: 'dayGridMonth' | 'timeGridWeek' | 'timeGridDay') {
      this.currentView = view;
    },

    setCurrentDate(date: Date) {
      this.currentDate = date;
    },
  },
});
