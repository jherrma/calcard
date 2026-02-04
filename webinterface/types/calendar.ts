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

export interface CalendarEvent {
  id: string;
  calendar_id: string;
  uid: string;
  summary: string;
  description?: string;
  location?: string;
  start: string;
  end: string;
  all_day: boolean;
  recurrence_rule?: string;
  created_at: string;
  updated_at: string;
}

export interface EventsQuery {
  start: string;
  end: string;
  calendar_ids?: string[];
}
