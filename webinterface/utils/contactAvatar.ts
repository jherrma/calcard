/**
 * Initials + a stable placeholder colour for a contact avatar (story 042).
 *
 * Extracted so the dashboard's recent-contacts rows render avatars identically
 * to `components/contacts/ContactListItem.vue`, which still carries its own
 * copy of this logic. Same palette and same hash, so the same person keeps the
 * same colour on both screens.
 *
 * Auto-imported by Nuxt (files under `utils/`).
 */

const AVATAR_COLORS = [
  '#3b82f6', '#ef4444', '#10b981', '#f59e0b', '#8b5cf6',
  '#ec4899', '#06b6d4', '#f97316', '#6366f1', '#14b8a6',
] as const;

/** First + last initial (`Ada Lovelace` → `AL`), `?` when there is no name. */
export function contactInitials(name: string | undefined | null): string {
  const parts = (name || '').split(/\s+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0]!.charAt(0) + parts[parts.length - 1]!.charAt(0)).toUpperCase();
  }
  // charAt (not [0]) because noUncheckedIndexedAccess types string[0] as
  // `string | undefined`; charAt always yields a string.
  return (parts[0]?.charAt(0) || '?').toUpperCase();
}

/** Deterministic palette colour derived from `seed` (a name, falling back to an id). */
export function contactAvatarColor(seed: string | undefined | null): string {
  const value = seed || '';
  let hash = 0;
  for (let i = 0; i < value.length; i++) {
    hash = value.charCodeAt(i) + ((hash << 5) - hash);
  }
  return AVATAR_COLORS[Math.abs(hash) % AVATAR_COLORS.length]!;
}
