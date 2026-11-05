/**
 * Alarm types (#169). They mirror `GET /api/v1/alarms/upcoming` and
 * `POST /api/v1/alarms/acknowledge` (spec §7.1, §7.2; Go DTOs in
 * `server/internal/adapter/http/dto/alarm_dto.go`). Both answer RAW JSON: no
 * `{ status, data }` envelope. The acknowledge answer is a 204 without a body.
 */

/** One reminder that fires within the polled window. */
export interface UpcomingAlarm {
  /** 32 lowercase hex characters: a hash that already contains `trigger_at`. All a client sends back to acknowledge. */
  key: string;
  /** The computed instant, UTC (`Z`). For scheduling only. */
  trigger_at: string;
  uid: string;
  /** Canonical recurrence key, or null for a non-series alarm. Passed back verbatim in the calendar deep link. */
  recurrence_id: string | null;
  /** Informational only: the lowest VALARM index among those sharing this key. */
  alarm_index: number;
  /** Lower-case; `display` for a VALARM without ACTION. */
  action: string;
  description: string | null;
  object_type: 'event' | 'task';
  /** Event or task UUID. */
  object_id: string;
  /** Numeric calendar id (the `?cal=` deep link). */
  calendar_id: number;
  calendar_uuid: string;
  calendar_name: string;
  calendar_color: string;
  summary: string;
  location: string | null;
  all_day: boolean;
  /** Event occurrence start/end (UTC), or `start_date` (`YYYY-MM-DD`) for an all-day one. */
  start: string | null;
  start_date: string | null;
  end: string | null;
  /** Task due (UTC), or `due_date` (`YYYY-MM-DD`) for an all-day one. */
  due: string | null;
  due_date: string | null;
}

export interface UpcomingAlarmsResponse {
  alarms: UpcomingAlarm[];
  count: number;
  from: string;
  until: string;
  /** The server clock, for the skew estimate. */
  server_time: string;
  /** More alarms existed than were returned (at most 500). */
  truncated: boolean;
}

/** Body of `POST /api/v1/alarms/acknowledge`: 1..500 keys, no times. */
export interface AcknowledgeAlarmsBody {
  keys: string[];
}
