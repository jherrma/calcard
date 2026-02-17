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
        visible: state.visibleCalendarIds.has(cal.id),
      }));
    },

    ownedCalendars(state: CalendarState) {
      return state.calendars.filter((c: Calendar) => !c.shared);
    },

    sharedCalendars(state: CalendarState) {
      return state.calendars.filter((c: Calendar) => c.shared);
    },

    writableCalendars(state: CalendarState) {
      return state.calendars.filter((c: Calendar) => !c.shared);
    },
  },

  actions: {
    async fetchCalendars() {
      const api = useApi();
      try {
        const response = await api<{ calendars: Calendar[] }>('/api/v1/calendars');
        this.calendars = response.calendars || [];

        // Initially show all calendars
        this.visibleCalendarIds = new Set(this.calendars.map((c: Calendar) => String(c.id)));
      } catch (e: unknown) {
        this.error = (e as Error).message || 'Failed to load calendars';
      }
    },

    async fetchEvents(start: Date, end: Date) {
      this.isLoading = true;
      this.error = null;

      try {
        const api = useApi();
        const allEvents: CalendarEvent[] = [];

        // Fetch events for all calendars so toggling visibility doesn't require refetching
        for (const calId of this.calendars.map((c: Calendar) => c.id)) {
          try {
            const response = await api<{ events: CalendarEvent[] }>(
              `/api/v1/calendars/${calId}/events?start=${start.toISOString()}&end=${end.toISOString()}`
            );
            if (response.events) {
              allEvents.push(...response.events);
            }
          } catch (e) {
            // Continue with other calendars if one fails
            console.warn(`Failed to load events for calendar ${calId}`, e);
          }
        }

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

      const response = await api<CalendarEvent>(`/api/v1/calendars/${calendarId}/events`, {
        method: 'POST',
        body,
      });

      this.events.push(response);
      return response;
    },

    async getEvent(calendarId: string, eventId: string) {
      const api = useApi();
      return await api<CalendarEvent>(`/api/v1/calendars/${calendarId}/events/${eventId}`);
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

      let url = `/api/v1/calendars/${calendarId}/events/${eventId}`;
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

    async deleteEvent(calendarId: string, eventId: string, scope?: string, recurrenceId?: string) {
      const api = useApi();

      let url = `/api/v1/calendars/${calendarId}/events/${eventId}`;
      const params = new URLSearchParams();
      if (scope) params.set('scope', scope);
      if (recurrenceId) params.set('recurrence_id', recurrenceId);
      if (params.toString()) url += `?${params.toString()}`;

      await api(url, { method: 'DELETE' });

      // Remove from local state
      this.events = this.events.filter((e: CalendarEvent) => e.id !== eventId);
    },

    async updateEventTime(eventId: string, calendarId: string, start: Date, end: Date) {
      const api = useApi();
      await api(`/api/v1/calendars/${calendarId}/events/${eventId}`, {
        method: 'PATCH',
        body: {
          start: toRFC3339(start),
          end: toRFC3339(end),
          timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        },
      });

      // Update local state
      const event = this.events.find((e: CalendarEvent) => e.id === eventId);
      if (event) {
        event.start = toRFC3339(start);
        event.end = toRFC3339(end);
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
