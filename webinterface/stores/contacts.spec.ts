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
