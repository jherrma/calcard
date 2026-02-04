import type { Calendar, CalendarEvent } from '~/types/calendar';

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
      return state.events.filter((e: CalendarEvent) => state.visibleCalendarIds.has(e.calendar_id));
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
  },

  actions: {
    async fetchCalendars() {
      const api = useApi();
      try {
        const response = await api<{ calendars: Calendar[] }>('/api/v1/calendars');
        this.calendars = response.calendars || [];

        // Initially show all calendars
        this.visibleCalendarIds = new Set(this.calendars.map((c: Calendar) => c.id));
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

        // Fetch events for each visible calendar
        for (const calId of this.visibleCalendarIds) {
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
      if (this.visibleCalendarIds.has(calendarId)) {
        this.visibleCalendarIds.delete(calendarId);
      } else {
        this.visibleCalendarIds.add(calendarId);
      }
    },

    async updateEventTime(eventId: string, calendarId: string, start: Date, end: Date) {
      const api = useApi();
      await api(`/api/v1/calendars/${calendarId}/events/${eventId}`, {
        method: 'PATCH',
        body: {
          start: start.toISOString(),
          end: end.toISOString(),
        },
      });

      // Update local state
      const event = this.events.find((e: CalendarEvent) => e.id === eventId);
      if (event) {
        event.start = start.toISOString();
        event.end = end.toISOString();
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
