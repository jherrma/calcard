export interface Calendar {
  id: string;
  name: string;
  description?: string;
  color: string;
  owner_id: string;
  shared?: boolean;
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
