// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import {
  PERMISSION_OPTIONS,
  permissionLabel,
  publicAccessUrl,
  shareCollectionUrl,
  shareErrorMessage,
  useSharingStore,
} from './sharing';
import { useAuthStore } from './auth';
import { useCalendarStore } from './calendars';
import { useContactsStore } from './contacts';
import type { PublicAccessStatus, Share } from '~/types/sharing';
import type { Calendar } from '~/types/calendar';
import type { AddressBook } from '~/types/contacts';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

function share(id: string, email: string, permission = 'read'): Share {
  return {
    id,
    shared_with: {
      id: `u-${id}`,
      username: email.split('@')[0]!,
      display_name: `User ${id}`,
      email,
    },
    permission,
    created_at: '2026-01-01T00:00:00Z',
  };
}

/** An ofetch-shaped rejection: the readable reason lives on `data`, not on `message`. */
function fetchError(body: Record<string, string>, message = '[POST] "/api/v1/x": 400 Bad Request') {
  return Object.assign(new Error(message), { data: body });
}

function calendar(overrides: Partial<Calendar> = {}): Calendar {
  return {
    id: '1',
    uuid: 'cal-uuid-1',
    path: 'work',
    name: 'Work',
    color: '#ff0000',
    owner_id: '1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function book(overrides: Partial<AddressBook> = {}): AddressBook {
  return {
    ID: 1,
    UUID: 'ab-uuid-1',
    UserID: 1,
    Path: 'default',
    Name: 'Colleagues',
    Description: '',
    CreatedAt: '2026-01-01T00:00:00Z',
    UpdatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
});

describe('URL helpers', () => {
  it('maps each resource type to its own collection, both keyed on the UUID (#52)', () => {
    expect(shareCollectionUrl('calendar', 'abc-123')).toBe('/api/v1/calendars/abc-123/shares');
    expect(shareCollectionUrl('addressbook', 'abc-123')).toBe('/api/v1/addressbooks/abc-123/shares');
    expect(publicAccessUrl('abc-123')).toBe('/api/v1/calendars/abc-123/public');
  });

  it('percent-encodes the identifier so a stray slash cannot escape the path', () => {
    expect(shareCollectionUrl('calendar', 'a/b')).toBe('/api/v1/calendars/a%2Fb/shares');
  });

  it('labels permissions from the single option list the UI renders', () => {
    expect(PERMISSION_OPTIONS.map((o) => o.value)).toEqual(['read', 'read-write']);
    expect(permissionLabel('read')).toBe('Can view');
    expect(permissionLabel('read-write')).toBe('Can edit');
    // Anything the backend might add stays readable rather than becoming blank.
    expect(permissionLabel('owner')).toBe('owner');
  });
});

describe('shareErrorMessage', () => {
  it('prefers the share handlers\' `error` key over ofetch\'s useless Error.message', () => {
    const e = fetchError({ error: "user 'nobody@example.com' not found" });
    expect(shareErrorMessage(e, 'fallback')).toBe("user 'nobody@example.com' not found");
  });

  it('falls back to `message` (the auth handlers\' key) then to the caller fallback', () => {
    expect(shareErrorMessage(fetchError({ message: 'boom' }), 'fallback')).toBe('boom');
    expect(shareErrorMessage(new Error('[POST] 500'), 'fallback')).toBe('fallback');
    expect(shareErrorMessage(null, 'fallback')).toBe('fallback');
  });
});

describe('fetchShares', () => {
  it('unwraps the { shares } envelope for a calendar', async () => {
    apiMock.mockResolvedValueOnce({ shares: [share('s1', 'a@example.com')] });
    const store = useSharingStore();

    await store.fetchShares('calendar', 'cal-uuid-1');

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/cal-uuid-1/shares');
    expect(store.shares).toHaveLength(1);
    expect(store.isLoadingShares).toBe(false);
    expect(store.sharesError).toBeNull();
  });

  it('hits the addressbooks collection for an address book', async () => {
    apiMock.mockResolvedValueOnce({ shares: [] });
    const store = useSharingStore();

    await store.fetchShares('addressbook', 'ab-uuid-1');

    expect(apiMock).toHaveBeenCalledWith('/api/v1/addressbooks/ab-uuid-1/shares');
    expect(store.shares).toEqual([]);
  });

  it('clears the list and records the reason when the call fails (e.g. the 404 a non-owner gets)', async () => {
    apiMock.mockRejectedValueOnce(fetchError({ error: 'not_found' }));
    const store = useSharingStore();
    store.shares = [share('stale', 'old@example.com')];

    await store.fetchShares('calendar', 'cal-uuid-1');

    expect(store.shares).toEqual([]);
    expect(store.sharesError).toBe('not_found');
    expect(store.isLoadingShares).toBe(false);
  });
});

describe('createShare', () => {
  it('posts user_identifier + permission and appends the created share', async () => {
    const created = share('s1', 'friend@example.com', 'read-write');
    apiMock.mockResolvedValueOnce(created);
    const store = useSharingStore();

    const result = await store.createShare('calendar', 'cal-uuid-1', '  friend@example.com  ', 'read-write');

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/cal-uuid-1/shares', {
      method: 'POST',
      // Trimmed — a pasted address with trailing whitespace must still resolve.
      body: { user_identifier: 'friend@example.com', permission: 'read-write' },
    });
    expect(result).toBe(created);
    expect(store.shares).toEqual([created]);
    expect(store.isSaving).toBe(false);
  });

  it('rejects an empty identifier without calling the API', async () => {
    const store = useSharingStore();
    await expect(store.createShare('calendar', 'cal-uuid-1', '   ', 'read'))
      .rejects.toThrow('Enter an email address or username');
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('refuses self-sharing by email or username, case-insensitively', async () => {
    const auth = useAuthStore();
    auth.user = {
      id: 'me',
      email: 'Me@Example.com',
      username: 'me',
      is_admin: false,
      created_at: '2026-01-01T00:00:00Z',
    };
    const store = useSharingStore();

    await expect(store.createShare('calendar', 'cal-uuid-1', 'me@example.com', 'read'))
      .rejects.toThrow('You cannot share a calendar with yourself');
    await expect(store.createShare('addressbook', 'ab-uuid-1', 'ME', 'read'))
      .rejects.toThrow('You cannot share a address book with yourself');
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('refuses a duplicate invite locally — the backend 400s on it anyway', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'friend@example.com')];

    await expect(store.createShare('calendar', 'cal-uuid-1', 'FRIEND@example.com', 'read'))
      .rejects.toThrow('already shared with');
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('rethrows the backend reason so the caller can toast something useful', async () => {
    apiMock.mockRejectedValueOnce(fetchError({ error: "user 'ghost' not found" }));
    const store = useSharingStore();

    await expect(store.createShare('calendar', 'cal-uuid-1', 'ghost', 'read'))
      .rejects.toThrow("user 'ghost' not found");
    expect(store.shares).toEqual([]);
    expect(store.isSaving).toBe(false);
  });
});

describe('updateShare', () => {
  it('patches the share and replaces it in place from the response', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'a@example.com', 'read-write'), share('s2', 'b@example.com', 'read')];
    apiMock.mockResolvedValueOnce(share('s1', 'a@example.com', 'read'));

    await store.updateShare('addressbook', 'ab-uuid-1', 's1', 'read');

    expect(apiMock).toHaveBeenCalledWith('/api/v1/addressbooks/ab-uuid-1/shares/s1', {
      method: 'PATCH',
      body: { permission: 'read' },
    });
    expect(store.shares.map((s) => [s.id, s.permission])).toEqual([['s1', 'read'], ['s2', 'read']]);
  });

  it('leaves the list untouched and rethrows on failure', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'a@example.com', 'read-write')];
    apiMock.mockRejectedValueOnce(fetchError({ error: 'invalid permission' }));

    await expect(store.updateShare('calendar', 'cal-uuid-1', 's1', 'read'))
      .rejects.toThrow('invalid permission');
    expect(store.shares[0]!.permission).toBe('read-write');
  });
});

describe('revokeShare', () => {
  it('deletes the share and drops it from state', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'a@example.com'), share('s2', 'b@example.com')];
    apiMock.mockResolvedValueOnce(undefined);

    await store.revokeShare('calendar', 'cal-uuid-1', 's1');

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/cal-uuid-1/shares/s1', { method: 'DELETE' });
    expect(store.shares.map((s) => s.id)).toEqual(['s2']);
  });

  it('keeps the share when the delete fails', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'a@example.com')];
    apiMock.mockRejectedValueOnce(fetchError({ error: 'share not found' }));

    await expect(store.revokeShare('calendar', 'cal-uuid-1', 's1')).rejects.toThrow('share not found');
    expect(store.shares).toHaveLength(1);
  });
});

describe('revokeAllShares', () => {
  it('removes every share and reports the count', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'a@example.com'), share('s2', 'b@example.com')];
    apiMock.mockResolvedValue(undefined);

    await expect(store.revokeAllShares('calendar', 'cal-uuid-1')).resolves.toEqual({ revoked: 2, failed: 0, reason: null });
    expect(store.shares).toEqual([]);
  });

  it('keeps only the shares whose delete failed — one failure must not strand the rest', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'a@example.com'), share('s2', 'b@example.com'), share('s3', 'c@example.com')];
    apiMock
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(fetchError({ error: 'boom' }))
      .mockResolvedValueOnce(undefined);

    await expect(store.revokeAllShares('calendar', 'cal-uuid-1')).resolves.toEqual({ revoked: 2, failed: 1, reason: 'boom' });
    expect(store.shares.map((s) => s.id)).toEqual(['s2']);
    expect(store.isSaving).toBe(false);
  });
});

describe('public link', () => {
  it('fetches the status — the only place the public URL is obtainable', async () => {
    apiMock.mockResolvedValueOnce({ enabled: true, public_url: 'https://x/public/calendar/tok.ics' });
    const store = useSharingStore();

    await store.fetchPublicAccess('cal-uuid-1');

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/cal-uuid-1/public');
    expect(store.publicAccess?.public_url).toBe('https://x/public/calendar/tok.ics');
    expect(store.isLoadingPublic).toBe(false);
  });

  it('disables via the same POST with enabled:false — there is no DELETE route', async () => {
    apiMock.mockResolvedValueOnce({ enabled: false, public_url: null });
    const store = useSharingStore();

    await store.setPublicAccess('cal-uuid-1', false);

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/cal-uuid-1/public', {
      method: 'POST',
      body: { enabled: false },
    });
    expect(store.publicAccess).toEqual({ enabled: false, public_url: null });
  });

  it('stores the regenerated token and does not touch state when regeneration fails', async () => {
    const store = useSharingStore();
    apiMock.mockResolvedValueOnce({ enabled: true, public_url: 'https://x/public/calendar/new.ics' });

    await store.regeneratePublicToken('cal-uuid-1');
    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendars/cal-uuid-1/public/regenerate', { method: 'POST' });
    expect(store.publicAccess?.public_url).toContain('new.ics');

    apiMock.mockRejectedValueOnce(fetchError({ error: 'public access is not enabled' }));
    await expect(store.regeneratePublicToken('cal-uuid-1')).rejects.toThrow('public access is not enabled');
    expect(store.publicAccess?.public_url).toContain('new.ics');
  });
});

describe('sharedWithMe', () => {
  it('derives rows from both list endpoints and skips resources you own', () => {
    useCalendarStore().calendars = [
      calendar({ uuid: 'own', name: 'Mine' }),
      calendar({
        uuid: 'shared-cal',
        name: 'Team',
        shared: true,
        permission: 'read-write',
        owner: { id: '9', display_name: 'Dana' },
      }),
    ];
    useContactsStore().addressBooks = [
      book({ UUID: 'own-ab', Name: 'My contacts' }),
      book({
        UUID: 'shared-ab',
        Name: 'Sales',
        shared: true,
        permission: 'read',
        owner: { id: '9', display_name: 'Dana' },
      }),
    ];

    const rows = useSharingStore().sharedWithMe;

    expect(rows).toEqual([
      {
        type: 'calendar',
        uuid: 'shared-cal',
        name: 'Team',
        color: '#ff0000',
        permission: 'read-write',
        ownerName: 'Dana',
      },
      {
        type: 'addressbook',
        uuid: 'shared-ab',
        name: 'Sales',
        permission: 'read',
        ownerName: 'Dana',
      },
    ]);
  });

  it('falls back to read-only and an anonymous owner when the payload omits them', () => {
    useCalendarStore().calendars = [calendar({ uuid: 'c', shared: true })];
    useContactsStore().addressBooks = [];

    const [row] = useSharingStore().sharedWithMe;

    expect(row?.permission).toBe('read');
    expect(row?.ownerName).toBe('another user');
  });

  it('is empty when nothing is shared', () => {
    useCalendarStore().calendars = [calendar()];
    useContactsStore().addressBooks = [book()];
    expect(useSharingStore().sharedWithMe).toEqual([]);
  });
});

describe('reset', () => {
  it('resetShares leaves the public-link half alone (the two panels are siblings)', async () => {
    const store = useSharingStore();
    store.shares = [share('s1', 'a@example.com')];
    store.publicAccess = { enabled: true, public_url: 'https://x/public/calendar/tok.ics' };

    store.resetShares();
    expect(store.shares).toEqual([]);
    expect(store.publicAccess).not.toBeNull();

    store.reset();
    expect(store.publicAccess).toBeNull();
  });
});

describe('out-of-order responses (request-sequence guard)', () => {
  /** A promise whose settlement this test controls. */
  function deferred<T>() {
    let resolve!: (v: T) => void;
    let reject!: (e: unknown) => void;
    const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
    return { promise, resolve, reject };
  }

  it('drops a superseded share response instead of listing resource A under resource B', async () => {
    const store = useSharingStore();
    // Calendar A's GET stalls — exactly what useApi's retry-once-on-401 does: it
    // awaits a token refresh before re-issuing the request.
    const slowA = deferred<{ shares: Share[] }>();
    apiMock.mockReturnValueOnce(slowA.promise);
    const pendingA = store.fetchShares('calendar', 'cal-a');

    // The dialog closes and reopens on calendar B, which answers immediately.
    store.resetShares();
    apiMock.mockResolvedValueOnce({ shares: [share('b1', 'b@example.com')] });
    await store.fetchShares('calendar', 'cal-b');

    // A finally lands. Without the guard it would overwrite B's list, telling the
    // owner that two people have access to B who in fact have access to A.
    slowA.resolve({ shares: [share('a1', 'a@example.com'), share('a2', 'a2@example.com')] });
    await pendingA;

    expect(store.shares.map((s) => s.id)).toEqual(['b1']);
    expect(store.isLoadingShares).toBe(false);
    expect(store.sharesError).toBeNull();
  });

  it('drops a superseded FAILURE too — B must not be blamed for A\'s error', async () => {
    const store = useSharingStore();
    const slowA = deferred<{ shares: Share[] }>();
    apiMock.mockReturnValueOnce(slowA.promise);
    const pendingA = store.fetchShares('calendar', 'cal-a');

    store.resetShares();
    apiMock.mockResolvedValueOnce({ shares: [share('b1', 'b@example.com')] });
    await store.fetchShares('calendar', 'cal-b');

    slowA.reject(fetchError({ error: 'calendar not found' }));
    await pendingA;

    expect(store.shares.map((s) => s.id)).toEqual(['b1']);
    expect(store.sharesError).toBeNull();
  });

  it('lets a mutation win over a GET that was issued before it', async () => {
    const store = useSharingStore();
    const slowList = deferred<{ shares: Share[] }>();
    apiMock.mockReturnValueOnce(slowList.promise);
    const pendingList = store.fetchShares('calendar', 'cal-a');

    const created = share('new', 'friend@example.com');
    apiMock.mockResolvedValueOnce(created);
    await store.createShare('calendar', 'cal-a', 'friend@example.com', 'read');

    // The pre-mutation list must not resurrect and hide the share just created.
    slowList.resolve({ shares: [] });
    await pendingList;

    expect(store.shares.map((s) => s.id)).toEqual(['new']);
    expect(store.isLoadingShares).toBe(false);
  });

  it('keeps the two halves\' errors apart — a public failure is not a share failure', async () => {
    const store = useSharingStore();
    apiMock.mockRejectedValueOnce(fetchError({ error: 'public status unavailable' }));

    await store.fetchPublicAccess('cal-uuid-1');

    expect(store.publicError).toBe('public status unavailable');
    // Both panels are mounted at once; a shared field would make the share list
    // render the public panel's error.
    expect(store.sharesError).toBeNull();
    expect(store.publicAccess).toBeNull();
  });

  it('drops a superseded public-status response after the calendar was toggled', async () => {
    const store = useSharingStore();
    const slowStatus = deferred<PublicAccessStatus>();
    apiMock.mockReturnValueOnce(slowStatus.promise);
    const pendingStatus = store.fetchPublicAccess('cal-uuid-1');

    apiMock.mockResolvedValueOnce({ enabled: true, public_url: 'https://x/public/calendar/new.ics' });
    await store.setPublicAccess('cal-uuid-1', true);

    slowStatus.resolve({ enabled: false, public_url: null });
    await pendingStatus;

    expect(store.publicAccess).toEqual({ enabled: true, public_url: 'https://x/public/calendar/new.ics' });
    expect(store.isLoadingPublic).toBe(false);
  });
});
