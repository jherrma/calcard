import type {
  PublicAccessStatus,
  Share,
  ShareListResponse,
  SharePermission,
  ShareResourceType,
  SharedResource,
} from '~/types/sharing';
import type { Calendar } from '~/types/calendar';
import type { AddressBook } from '~/types/contacts';
import { useAuthStore } from '~/stores/auth';
import { useCalendarStore } from '~/stores/calendars';
import { useContactsStore } from '~/stores/contacts';

/**
 * Base path of a resource's share collection (story 043). Calendars and address
 * books use the identical sub-resource layout, and BOTH are keyed on the
 * resource UUID (#52) — passing a numeric id here yields a 404.
 */
export function shareCollectionUrl(type: ShareResourceType, uuid: string): string {
  const segment = type === 'calendar' ? 'calendars' : 'addressbooks';
  return `/api/v1/${segment}/${encodeURIComponent(uuid)}/shares`;
}

/** Path of the calendar public-link sub-resource. Calendars only — address books have no public mode. */
export function publicAccessUrl(calendarUuid: string): string {
  return `/api/v1/calendars/${encodeURIComponent(calendarUuid)}/public`;
}

export const PERMISSION_OPTIONS: { label: string; value: SharePermission }[] = [
  { label: 'Can view', value: 'read' },
  { label: 'Can edit', value: 'read-write' },
];

export function permissionLabel(permission: string): string {
  return PERMISSION_OPTIONS.find((o) => o.value === permission)?.label ?? permission;
}

/**
 * Pull the human-readable reason out of a failed share call.
 *
 * The share/public handlers answer with `{"error": "user 'x' not found"}` — note
 * the key is `error`, unlike the auth handlers' `message` that the rest of the
 * app reads. Falling through to `Error.message` would surface ofetch's
 * "[POST] /api/v1/…: 400 Bad Request", which tells the user nothing, so the
 * fallback string is preferred over it.
 */
export function shareErrorMessage(e: unknown, fallback: string): string {
  const data = (e as { data?: { error?: string; message?: string } } | null)?.data;
  return data?.error || data?.message || fallback;
}

/** A resource kind's noun, for messages that are shared between both kinds. */
function resourceNoun(type: ShareResourceType): string {
  return type === 'calendar' ? 'calendar' : 'address book';
}

interface SharingState {
  /** Shares of the resource currently open in the share UI. */
  shares: Share[];
  isLoadingShares: boolean;
  isSaving: boolean;
  /**
   * Why the share list could not be loaded. MUST be rendered: an empty
   * `shares` after a failed load is indistinguishable from "nothing is shared",
   * and claiming a resource is private when we simply don't know is a lie the
   * owner may act on.
   */
  sharesError: string | null;
  /** Public-link status of the calendar currently open. null until fetched. */
  publicAccess: PublicAccessStatus | null;
  isLoadingPublic: boolean;
  /**
   * Why the public-link status could not be loaded. Separate from
   * `sharesError` because both panels are mounted at once — a single field
   * would let the public panel's failure surface as the share list's.
   */
  publicError: string | null;
  /**
   * Monotonic request tokens. The store holds exactly ONE resource's shares /
   * public status, so a response that arrives after the UI moved on to another
   * resource (or after a mutation wrote a fresher value) must be dropped rather
   * than applied. useApi's retry-once-on-401 makes that reordering window wide:
   * a request that hits an expired access token costs a refresh round-trip
   * before it is re-issued, so a later request can easily resolve first.
   */
  sharesSeq: number;
  publicSeq: number;
}

/**
 * One store for both resource kinds. Read actions swallow failures into
 * `sharesError` / `publicError` (a 404 here just means "you are not the owner",
 * which the UI renders as a blocked state); mutations THROW so the caller can
 * toast the server's reason and roll its optimistic UI back.
 */
export const useSharingStore = defineStore('sharing', {
  state: (): SharingState => ({
    shares: [],
    isLoadingShares: false,
    isSaving: false,
    sharesError: null,
    publicAccess: null,
    isLoadingPublic: false,
    publicError: null,
    sharesSeq: 0,
    publicSeq: 0,
  }),

  getters: {
    /**
     * Resources OTHER users shared with you, from both list endpoints. There is
     * no /shared-with-me endpoint — `shared === true` on a listed resource is
     * the signal, and it also means the share endpoints would 404, so nothing
     * here may offer share management.
     */
    sharedWithMe(): SharedResource[] {
      const calendars = useCalendarStore().calendars as Calendar[];
      const addressBooks = useContactsStore().addressBooks as AddressBook[];

      const rows: SharedResource[] = [];
      for (const cal of calendars) {
        if (!cal.shared) continue;
        rows.push({
          type: 'calendar',
          uuid: cal.uuid,
          name: cal.name,
          color: cal.color,
          permission: cal.permission || 'read',
          ownerName: cal.owner?.display_name || 'another user',
        });
      }
      for (const ab of addressBooks) {
        if (!ab.shared) continue;
        rows.push({
          type: 'addressbook',
          uuid: ab.UUID,
          name: ab.Name,
          permission: ab.permission || 'read',
          ownerName: ab.owner?.display_name || 'another user',
        });
      }
      return rows;
    },
  },

  actions: {
    /** Drop the previous resource's data so a reopened dialog never flashes stale state. */
    reset() {
      this.resetShares();
      this.invalidatePublic();
      this.publicAccess = null;
      this.publicError = null;
    },

    /**
     * Share-list half of `reset()`. Kept separate because the share panel and
     * the public-link panel are siblings that each own one half of this store —
     * a full reset from one would wipe the other's freshly fetched state.
     */
    resetShares() {
      // Invalidate anything still in flight: without this, the previous
      // resource's response would land afterwards and repopulate the list we
      // just cleared, under the header of a DIFFERENT resource.
      this.invalidateShares();
      this.shares = [];
      this.sharesError = null;
      this.isSaving = false;
    },

    /**
     * Discard an in-flight share fetch. Mutations call it because their own
     * response is newer than a GET issued before them — letting that GET land
     * would resurrect the pre-mutation list. Clearing the loading flag here is
     * required: `fetchShares` refuses to clear a flag it no longer owns, so a
     * superseded request would otherwise leave the spinner up forever.
     */
    invalidateShares() {
      this.sharesSeq++;
      this.isLoadingShares = false;
    },

    /** `invalidateShares` for the public-link half. */
    invalidatePublic() {
      this.publicSeq++;
      this.isLoadingPublic = false;
    },

    async fetchShares(type: ShareResourceType, uuid: string) {
      const api = useApi();
      const seq = ++this.sharesSeq;
      this.isLoadingShares = true;
      this.sharesError = null;
      try {
        const response = await api<ShareListResponse>(shareCollectionUrl(type, uuid));
        if (seq !== this.sharesSeq) return; // superseded — another resource owns the state now
        this.shares = response.shares || [];
      } catch (e: unknown) {
        if (seq !== this.sharesSeq) return;
        // Includes the 404 a non-owner gets. Blank list + error is the honest
        // state — the UI must render the error INSTEAD of the empty state.
        this.shares = [];
        this.sharesError = shareErrorMessage(e, 'Failed to load sharing settings');
      } finally {
        // Only the newest request may clear the flag; a superseded one would
        // otherwise hide the spinner of the request that replaced it.
        if (seq === this.sharesSeq) this.isLoadingShares = false;
      }
    },

    /**
     * Invite by email OR username — the backend's `user_identifier` accepts
     * either (there is no user-search endpoint to autocomplete against).
     * Sharing with yourself is rejected server-side too; catching it here keeps
     * the round-trip and gives a message that names the actual problem.
     */
    async createShare(
      type: ShareResourceType,
      uuid: string,
      identifier: string,
      permission: SharePermission,
    ): Promise<Share> {
      const trimmed = identifier.trim();
      if (!trimmed) {
        throw new Error('Enter an email address or username');
      }

      const self = useAuthStore().user;
      const needle = trimmed.toLowerCase();
      if (self && (self.email.toLowerCase() === needle || self.username.toLowerCase() === needle)) {
        throw new Error(`You cannot share a ${resourceNoun(type)} with yourself`);
      }

      if (this.shares.some((s: Share) => s.shared_with.email.toLowerCase() === needle
        || s.shared_with.username.toLowerCase() === needle)) {
        throw new Error(`This ${resourceNoun(type)} is already shared with ${trimmed}`);
      }

      const api = useApi();
      this.invalidateShares();
      this.isSaving = true;
      try {
        const share = await api<Share>(shareCollectionUrl(type, uuid), {
          method: 'POST',
          body: { user_identifier: trimmed, permission },
        });
        this.shares = [...this.shares, share];
        return share;
      } catch (e: unknown) {
        throw new Error(shareErrorMessage(e, `Failed to share this ${resourceNoun(type)}`));
      } finally {
        this.isSaving = false;
      }
    },

    async updateShare(
      type: ShareResourceType,
      uuid: string,
      shareId: string,
      permission: SharePermission,
    ): Promise<Share> {
      const api = useApi();
      this.invalidateShares();
      this.isSaving = true;
      try {
        const updated = await api<Share>(`${shareCollectionUrl(type, uuid)}/${encodeURIComponent(shareId)}`, {
          method: 'PATCH',
          body: { permission },
        });
        // Replace from the response rather than trusting the requested value —
        // the server is the authority on what the grant now is.
        this.shares = this.shares.map((s: Share) => (s.id === shareId ? updated : s));
        return updated;
      } catch (e: unknown) {
        throw new Error(shareErrorMessage(e, 'Failed to update permission'));
      } finally {
        this.isSaving = false;
      }
    },

    async revokeShare(type: ShareResourceType, uuid: string, shareId: string) {
      const api = useApi();
      this.invalidateShares();
      this.isSaving = true;
      try {
        await api(`${shareCollectionUrl(type, uuid)}/${encodeURIComponent(shareId)}`, { method: 'DELETE' });
        this.shares = this.shares.filter((s: Share) => s.id !== shareId);
      } catch (e: unknown) {
        throw new Error(shareErrorMessage(e, 'Failed to remove access'));
      } finally {
        this.isSaving = false;
      }
    },

    /**
     * Revoke every share at once. Revocations run in parallel and are settled
     * independently: one failure must not strand the others, so the successful
     * ids are dropped from state and the count of failures is reported. The
     * first rejection's reason is handed back too — a bare count leaves the user
     * with "1 share could not be removed" and no idea why.
     */
    async revokeAllShares(
      type: ShareResourceType,
      uuid: string,
    ): Promise<{ revoked: number; failed: number; reason: string | null }> {
      const api = useApi();
      const ids = this.shares.map((s: Share) => s.id);
      this.invalidateShares();
      this.isSaving = true;
      try {
        const results = await Promise.allSettled(
          ids.map((id: string) =>
            api(`${shareCollectionUrl(type, uuid)}/${encodeURIComponent(id)}`, { method: 'DELETE' })),
        );
        const failedIds = new Set(
          ids.filter((_: string, i: number) => results[i]?.status === 'rejected'),
        );
        const firstRejection = results.find((r) => r.status === 'rejected');
        const reason = firstRejection
          ? shareErrorMessage(firstRejection.reason, 'the server rejected the request')
          : null;
        this.shares = this.shares.filter((s: Share) => failedIds.has(s.id));
        // Keep the reason on state as well: the rows that survived are exactly
        // the failures, so the panel can explain why they are still listed.
        this.sharesError = reason;
        return { revoked: ids.length - failedIds.size, failed: failedIds.size, reason };
      } finally {
        this.isSaving = false;
      }
    },

    async fetchPublicAccess(calendarUuid: string) {
      const api = useApi();
      const seq = ++this.publicSeq;
      this.isLoadingPublic = true;
      this.publicError = null;
      try {
        const status = await api<PublicAccessStatus>(publicAccessUrl(calendarUuid));
        if (seq !== this.publicSeq) return; // superseded by another calendar or by a mutation
        this.publicAccess = status;
      } catch (e: unknown) {
        if (seq !== this.publicSeq) return;
        // The status endpoint is the ONLY source of the public URL (the token is
        // json:"-" on the domain model), so on failure we genuinely know nothing
        // and the panel must say so instead of implying "not public".
        this.publicAccess = null;
        this.publicError = shareErrorMessage(e, 'Failed to load public access status');
      } finally {
        if (seq === this.publicSeq) this.isLoadingPublic = false;
      }
    },

    /**
     * Enable or disable the public link. POST …/public takes `{ enabled }` and
     * handles BOTH directions — disabling also clears the token server-side, so
     * re-enabling later hands out a brand-new URL.
     */
    async setPublicAccess(calendarUuid: string, enabled: boolean): Promise<PublicAccessStatus> {
      const api = useApi();
      this.invalidatePublic();
      this.isSaving = true;
      try {
        const status = await api<PublicAccessStatus>(publicAccessUrl(calendarUuid), {
          method: 'POST',
          body: { enabled },
        });
        this.publicAccess = status;
        this.publicError = null;
        return status;
      } catch (e: unknown) {
        throw new Error(shareErrorMessage(e, 'Failed to update public access'));
      } finally {
        this.isSaving = false;
      }
    },

    /** Mint a new token. The previous URL stops working immediately — confirm before calling. */
    async regeneratePublicToken(calendarUuid: string): Promise<PublicAccessStatus> {
      const api = useApi();
      this.invalidatePublic();
      this.isSaving = true;
      try {
        const status = await api<PublicAccessStatus>(`${publicAccessUrl(calendarUuid)}/regenerate`, {
          method: 'POST',
        });
        this.publicAccess = status;
        this.publicError = null;
        return status;
      } catch (e: unknown) {
        throw new Error(shareErrorMessage(e, 'Failed to regenerate the public link'));
      } finally {
        this.isSaving = false;
      }
    },
  },
});
