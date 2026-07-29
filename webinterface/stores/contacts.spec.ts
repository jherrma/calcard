// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { createTestingPinia } from '@pinia/testing';
import { useContactsStore } from './contacts';
import type { AddressBook, Contact } from '~/types/contacts';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

function book(id: number, name = `Book ${id}`): AddressBook {
  return {
    ID: id,
    UUID: `uuid-${id}`,
    UserID: 1,
    Path: `/ab/${id}`,
    Name: name,
    Description: '',
    CreatedAt: '2026-01-01T00:00:00Z',
    UpdatedAt: '2026-01-01T00:00:00Z',
  };
}

// Build `n` contact stubs starting at index `start`. addressbook_id is a numeric
// string, matching the backend's fmt.Sprintf("%d", id) shape.
function contacts(start: number, n: number, abId = 1): Contact[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `c${start + i}`,
    addressbook_id: String(abId),
    uid: `uid-${start + i}`,
    formatted_name: `Person ${start + i}`,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }));
}

// A page envelope as returned RAW (not unwrapped) by the contacts endpoint.
function page(cts: Contact[], total: number, offset: number) {
  return { Contacts: cts, Total: total, Limit: 200, Offset: offset };
}

beforeEach(() => {
  createTestingPinia({ stubActions: false });
  apiMock.mockReset();
});

describe('fetchContacts paging loop', () => {
  it('aggregates every page and terminates on the short final page (page.length < limit)', async () => {
    // Total 500 with limit 200 → pages of 200 / 200 / 100 (short page breaks).
    apiMock
      .mockResolvedValueOnce(page(contacts(0, 200), 500, 0))
      .mockResolvedValueOnce(page(contacts(200, 200), 500, 200))
      .mockResolvedValueOnce(page(contacts(400, 100), 500, 400));

    const store = useContactsStore();
    store.addressBooks = [book(1)];

    await store.fetchContacts();

    expect(store.contacts).toHaveLength(500);
    expect(apiMock).toHaveBeenCalledTimes(3);
    // Confirm the offsets walked 0 → 200 → 400.
    const urls = apiMock.mock.calls.map((c) => c[0] as string);
    expect(urls[0]).toContain('offset=0');
    expect(urls[1]).toContain('offset=200');
    expect(urls[2]).toContain('offset=400');
    expect(urls.every((u) => u.includes('limit=200'))).toBe(true);
  });

  it('terminates on offset >= Total even when the final page is exactly `limit` long', async () => {
    // Total 400, two full pages of 200. Without the `offset >= Total` guard a full
    // final page would loop forever — assert exactly 2 calls, no third.
    apiMock
      .mockResolvedValueOnce(page(contacts(0, 200), 400, 0))
      .mockResolvedValueOnce(page(contacts(200, 200), 400, 200));

    const store = useContactsStore();
    store.addressBooks = [book(1)];

    await store.fetchContacts();

    expect(store.contacts).toHaveLength(400);
    expect(apiMock).toHaveBeenCalledTimes(2);
  });

  it('fetches all address books concurrently, not one book at a time (#22)', async () => {
    const store = useContactsStore();
    store.addressBooks = [book(1), book(2), book(3)];

    // Never-resolving promises: a serial loop would issue only book 1's first
    // page and await it, so exactly ONE call would be in flight here. The
    // parallel version dispatches all three books' first pages up front.
    const resolvers: Array<(v: ReturnType<typeof page>) => void> = [];
    apiMock.mockImplementation(() => new Promise((res) => { resolvers.push(res); }));

    const p = store.fetchContacts();
    expect(apiMock).toHaveBeenCalledTimes(3);

    // Each book returns a single empty (short) page → its paging loop ends.
    resolvers.forEach((res) => res(page([], 0, 0)));
    await p;
    expect(store.contacts).toHaveLength(0);
  });

  it('warns and continues when one book rejects, still loading the others', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    // Book 1 fails outright; book 2 returns a single short page.
    apiMock
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce(page(contacts(0, 2, 2), 2, 0));

    const store = useContactsStore();
    store.addressBooks = [book(1, 'Broken'), book(2, 'Good')];

    await store.fetchContacts();

    // Only the healthy book's contacts survived; the failure did not abort the loop.
    expect(store.contacts).toHaveLength(2);
    expect(store.contacts.every((c) => c.addressbook_id === '2')).toBe(true);
    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0]![0]).toContain('Broken');
    // A per-book failure is swallowed, so the store-level error stays clear.
    expect(store.error).toBeNull();

    warnSpy.mockRestore();
  });
});

// Build a Contact whose first-name (default sort key) is `givenName`.
function named(id: string, givenName: string, abId = 1): Contact {
  return {
    id,
    addressbook_id: String(abId),
    uid: `uid-${id}`,
    given_name: givenName,
    formatted_name: givenName,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

describe('groupedContacts letter bucketing', () => {
  it('folds accented/umlaut first letters onto their base Latin letter instead of #', () => {
    const store = useContactsStore();
    store.addressBooks = [book(1)];
    store.selectedAddressBookIds = new Set([1]);
    store.sortBy = 'first_name';
    store.contacts = [
      named('a', 'Ärzte'),
      named('b', 'Élan'),
      named('c', 'Öztürk'),
      named('d', 'Zöe'),
      named('e', '9Lives'),
      named('f', 'Bob'),
    ];

    const groups = store.groupedContacts;
    // Locate the bucket key that a given contact landed in.
    const keyOf = (givenName: string) =>
      [...groups.entries()].find(([, cts]) => cts.some((c) => c.given_name === givenName))?.[0];

    // Diacritics fold to the base Latin letter (this is what the pre-fix code got wrong).
    expect(keyOf('Ärzte')).toBe('A');
    expect(keyOf('Élan')).toBe('E');
    expect(keyOf('Öztürk')).toBe('O');
    expect(keyOf('Zöe')).toBe('Z');
    // Plain A–Z is unchanged and genuinely non-Latin (a leading digit) still buckets to '#'.
    expect(keyOf('Bob')).toBe('B');
    expect(keyOf('9Lives')).toBe('#');

    // Only the digit-led name is in '#'; no accented name leaked in.
    expect(groups.get('#')?.map((c) => c.given_name)).toEqual(['9Lives']);
    // availableLetters stays coherent with the folded keys (sorted).
    expect(store.availableLetters).toEqual(['#', 'A', 'B', 'E', 'O', 'Z']);
  });
});

describe('address book permissions (#53)', () => {
  // A shared book, as GET /api/v1/addressbooks now returns it: the raw GORM
  // fields stay PascalCase while the sharing metadata is snake_case.
  function sharedBook(id: number, permission: string, name = `Shared ${id}`): AddressBook {
    return { ...book(id, name), shared: true, permission, owner: { id: 'u-9', display_name: 'Alice' } };
  }

  it('writableAddressBooks keeps owned books and read-write shares, drops read-only shares', () => {
    const store = useContactsStore();
    store.addressBooks = [
      book(1, 'Mine'),
      sharedBook(2, 'read'),
      sharedBook(3, 'read-write'),
    ];

    const writable = store.writableAddressBooks.map((ab) => ab.ID);
    expect(writable).toEqual([1, 3]);
  });

  it('owned/shared getters partition the list', () => {
    const store = useContactsStore();
    store.addressBooks = [book(1), sharedBook(2, 'read'), sharedBook(3, 'read-write')];

    expect(store.ownedAddressBooks.map((ab) => ab.ID)).toEqual([1]);
    expect(store.sharedAddressBooks.map((ab) => ab.ID)).toEqual([2, 3]);
  });

  it('canWriteAddressBook gates per book, accepting the numeric-string id contacts carry', () => {
    const store = useContactsStore();
    store.addressBooks = [book(1), sharedBook(2, 'read'), sharedBook(3, 'read-write')];

    // Own book: writable. Contacts carry addressbook_id as a numeric STRING,
    // so both forms must resolve.
    expect(store.canWriteAddressBook(1)).toBe(true);
    expect(store.canWriteAddressBook('1')).toBe(true);

    // Read-only share: not writable — this is the gate every edit control uses.
    expect(store.canWriteAddressBook(2)).toBe(false);
    expect(store.canWriteAddressBook('2')).toBe(false);

    // Read-write share: writable. Before #53 the REST API refused these, so the
    // UI hid the controls; now both agree.
    expect(store.canWriteAddressBook(3)).toBe(true);

    // Unknown id: assume writable rather than disabling the UI on a book that
    // simply hasn't loaded yet — the API remains the real gate.
    expect(store.canWriteAddressBook(999)).toBe(true);
  });

  it('treats a shared book with a missing permission as read-only', () => {
    const store = useContactsStore();
    // Defensive: an older backend (or a truncated payload) omits `permission`.
    // Failing closed keeps us from offering writes that would 403.
    store.addressBooks = [{ ...book(4), shared: true }];

    expect(store.canWriteAddressBook(4)).toBe(false);
    expect(store.writableAddressBooks).toHaveLength(0);
  });
});
