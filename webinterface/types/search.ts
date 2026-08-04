import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';

/**
 * A single matching event occurrence, denormalised with the calendar metadata the
 * result row needs (colour + name). `GET /api/v1/search` (#156) returns exactly
 * this, so nothing has to be joined against locally loaded state — the hit is
 * self-sufficient for rendering and for deep-linking into the calendar view.
 *
 * A recurring series appears ONCE, represented by the occurrence the server
 * picked (the next one, or the last one for a finished series), carrying that
 * occurrence's `recurrence_id`.
 */
export interface EventHit {
  /** Unique per rendered occurrence: expanded instances share `event.id`. */
  key: string;
  event: CalendarEvent;
  /** Numeric calendar id (as string) — what the deep link and colour lookup use. */
  calendarId: string;
  calendarName: string;
  calendarColor: string;
}

/** A matching contact plus its address book, needed for the `?ab=` route param. */
export interface ContactHit {
  contact: Contact;
  /** Numeric address book id as a string (`Contact.addressbook_id`). */
  addressBookId: string;
  addressBookName: string;
}

export interface SearchResults {
  events: EventHit[];
  contacts: ContactHit[];
  calendars: Calendar[];
  addressBooks: AddressBook[];
  /**
   * Per category: true when the server had more matches than it returned. The
   * server caps each group (see `max_limit` in the response), so a count of N
   * with `hasMore` means "N+" — rendering a bare N would claim a total the
   * server never promised.
   */
  hasMore: {
    events: boolean;
    contacts: boolean;
    calendars: boolean;
    addressBooks: boolean;
  };
}

// ---------------------------------------------------------------------------
// Wire shapes of GET /api/v1/search (#156). Raw JSON — no { status, data }
// envelope, like its sibling /contacts/search.
// ---------------------------------------------------------------------------

export interface SearchApiGroup<T> {
  items: T[];
  count: number;
  has_more: boolean;
  /**
   * False when the category was excluded via `types`. An empty group with
   * `searched: false` means "not looked at", NOT "nothing matched".
   */
  searched: boolean;
}

export interface SearchApiEventItem {
  event: CalendarEvent;
  calendar_uuid: string;
  calendar_name: string;
  calendar_color: string;
}

export interface SearchApiContactItem {
  contact: Contact;
  addressbook_uuid: string;
  addressbook_name: string;
}

export interface SearchApiResponse {
  query: string;
  types: string[];
  /** Items per group the server applied, and the cap it would clamp to. */
  limit: number;
  offset: number;
  max_limit: number;
  events: SearchApiGroup<SearchApiEventItem>;
  contacts: SearchApiGroup<SearchApiContactItem>;
  calendars: SearchApiGroup<Calendar>;
  addressbooks: SearchApiGroup<AddressBook>;
}
