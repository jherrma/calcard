export interface Calendar {
  id: string;
  uuid: string;
  path: string;
  name: string;
  description?: string;
  color: string;
  timezone?: string;
  owner_id: string;
  shared?: boolean;
  permission?: string;
  /**
   * True when this calendar mirrors a remote iCalendar feed (story 100). The
   * server refuses every write to it — from its owner as much as from a
   * sharee — so the UI must not offer one.
   */
  subscribed?: boolean;
  public_enabled?: boolean;
  public_url?: string;
  owner?: {
    id: string;
    display_name: string;
  };
  created_at: string;
  updated_at: string;
}

export interface RecurrenceRule {
  frequency: string;
  interval: number;
  by_day?: string[];
  by_month_day?: number[];
  by_month?: number[];
  until?: string;
  count?: number;
}

export interface CalendarEvent {
  id: string;
  calendar_id: number;
  uid: string;
  summary: string;
  description?: string;
  location?: string;
  start: string;
  end: string;
  all_day: boolean;
  is_recurring: boolean;
  recurrence_id?: string;
  recurrence?: RecurrenceRule;
}

export interface EventFormData {
  summary: string;
  description: string;
  location: string;
  calendar_id: string;
  all_day: boolean;
  start: Date;
  end: Date;
  timezone: string;
  recurrence?: RecurrenceRule;
}

export interface EventsQuery {
  start: string;
  end: string;
  calendar_ids?: string[];
}

/**
 * A subscription to a remote iCalendar feed (story 100). The server flattens
 * the subscription and the calendar mirroring it into one object, because they
 * are one thing to the user: "a calendar I subscribed to".
 */
export interface CalendarSubscription {
  id: string;
  /** UUID of the read-only calendar this feed is mirrored into. */
  calendar_id: string;
  name: string;
  description: string;
  color: string;
  url: string;
  /** A Go duration string from the allowed set: 15m, 30m, 1h, 6h, 12h, 24h. */
  refresh_interval: string;
  status: 'pending' | 'synced' | 'error' | 'disabled';
  enabled: boolean;
  last_synced_at: string | null;
  /** Null when auto-sync is off — there is no next refresh to report. */
  next_sync_at: string | null;
  last_error: string;
  error_count: number;
  event_count: number;
  created_at: string;
}

export interface CalendarSubscriptionListResponse {
  subscriptions: CalendarSubscription[];
}

/** POST /calendar-subscriptions/:id/refresh — the subscription plus what changed. */
export interface CalendarSubscriptionRefreshResponse extends CalendarSubscription {
  synced: boolean;
  created: number;
  updated: number;
  deleted: number;
  skipped: number;
}
