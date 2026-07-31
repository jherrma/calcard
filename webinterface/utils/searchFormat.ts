// Row formatting shared by the global-search palette (components/common/GlobalSearch.vue)
// and the full results page (pages/search.vue) — story 044. Both render the same
// event/contact rows, so the labels live here instead of being duplicated (and
// drifting) between the two.
//
// Auto-imported by Nuxt (files under `utils/`), but the .vue sources import these
// explicitly, matching the sibling components.

import type { CalendarEvent } from '~/types/calendar';
import type { ContactHit } from '~/types/search';

const dayFormatter = new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
const timeFormatter = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });

/** "Tue, 16 Jun, 09:00" — or just the day for an all-day event. */
export function formatEventWhen(event: CalendarEvent): string {
  const start = new Date(event.start);
  if (Number.isNaN(start.getTime())) return '';
  if (event.all_day) return dayFormatter.format(start);
  return `${dayFormatter.format(start)}, ${timeFormatter.format(start)}`;
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

const DAY_MS = 86_400_000;

/** "Today" / "Tomorrow" / the formatted day — compared on LOCAL day boundaries. */
export function eventRelativeLabel(event: CalendarEvent, now: Date = new Date()): string {
  const start = new Date(event.start);
  if (Number.isNaN(start.getTime())) return '';
  const days = Math.round((startOfDay(start) - startOfDay(now)) / DAY_MS);
  if (days === 0) return 'Today';
  if (days === 1) return 'Tomorrow';
  return dayFormatter.format(start);
}

export function isPastEvent(event: CalendarEvent, now: number = Date.now()): boolean {
  const reference = new Date(event.end || event.start);
  if (Number.isNaN(reference.getTime())) return false;
  return reference.getTime() < now;
}

export function searchInitials(name: string | undefined): string {
  const parts = (name || '').split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]!.charAt(0) + parts[parts.length - 1]!.charAt(0)).toUpperCase();
  return (parts[0]?.charAt(0) || '?').toUpperCase();
}

// Same hash-to-palette scheme as the contact list/detail views, so a contact keeps
// one colour everywhere.
const AVATAR_COLORS = [
  '#3b82f6', '#ef4444', '#10b981', '#f59e0b', '#8b5cf6',
  '#ec4899', '#06b6d4', '#f97316', '#6366f1', '#14b8a6',
];

export function searchAvatarColor(seed: string): string {
  let hash = 0;
  for (let i = 0; i < seed.length; i++) {
    hash = seed.charCodeAt(i) + ((hash << 5) - hash);
  }
  return AVATAR_COLORS[Math.abs(hash) % AVATAR_COLORS.length]!;
}

/**
 * `YYYY-MM-DD` in LOCAL time for the /calendar?date= deep link. Using the date
 * parts (not toISOString) keeps an evening event on its own day east of Greenwich.
 */
export function toLocalDateParam(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** "Acme · alice@example.com · Work" — organization, primary email, address book. */
export function contactMetaLine(hit: ContactHit): string {
  const contact = hit.contact;
  const email = contact.emails?.find((e) => e.primary)?.value || contact.emails?.[0]?.value || '';
  return [contact.organization, email, hit.addressBookName].filter(Boolean).join(' · ');
}
