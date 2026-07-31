import type { Calendar, CalendarEvent } from '~/types/calendar';
import type { AddressBook, Contact } from '~/types/contacts';

/**
 * A single matching event occurrence, denormalised with the calendar metadata the
 * result row needs (colour + name). The backend has no cross-resource search
 * endpoint (story 044), so these are assembled client-side and carry everything
 * required to render and to deep-link into the calendar view.
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
}
