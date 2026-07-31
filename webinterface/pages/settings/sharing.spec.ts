// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import SharingSettingsPage from './sharing.vue';
import { useCalendarStore } from '~/stores/calendars';
import { useContactsStore } from '~/stores/contacts';
import type { Calendar } from '~/types/calendar';
import type { AddressBook } from '~/types/contacts';

// SCOPE: the "Shared with me" overview (story 043). Its two list actions swallow
// their failures into their own store's `error` field, so a broken backend and
// "nothing is shared with you" produce the SAME empty `rows`. This page must
// distinguish them: an affirmative "nothing has been shared with you" on a
// failed load is a false statement with no retry affordance.

interface PageVm {
  load: () => Promise<void>;
  loadError: string | null;
  isLoading: boolean;
}

function calendar(overrides: Partial<Calendar> = {}): Calendar {
  return {
    id: '1',
    uuid: 'cal-uuid-1',
    path: 'work',
    name: 'Work',
    color: '#ff0000',
    owner_id: '9',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function book(overrides: Partial<AddressBook> = {}): AddressBook {
  return {
    ID: 1,
    UUID: 'ab-uuid-1',
    UserID: 9,
    Path: 'default',
    Name: 'Sales',
    Description: '',
    CreatedAt: '2026-01-01T00:00:00Z',
    UpdatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

/**
 * Stores must be primed BEFORE mount: the page loads in onMounted, and the
 * failure signal is a store field the stubbed action would normally set.
 */
function setup() {
  createTestingPinia({ stubActions: true });
  return { calendarStore: useCalendarStore(), contactsStore: useContactsStore() };
}

async function mountPage() {
  const wrapper = mount(SharingSettingsPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await nextTick();
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

beforeEach(() => {
  vi.stubGlobal('$fetch', vi.fn());
});

describe('load failure', () => {
  it('reports the error instead of claiming nothing is shared', async () => {
    const { calendarStore } = setup();
    vi.mocked(calendarStore.fetchCalendars).mockImplementation(async () => {
      calendarStore.error = 'Failed to load calendars';
    });

    const { wrapper, vm } = await mountPage();

    expect(vm.loadError).toBe('Failed to load calendars');
    expect(wrapper.text()).toContain('Failed to load calendars');
    // REVERT PROOF: without the loadError branch the empty state renders and
    // tells the user nothing has been shared with them, on no evidence.
    expect(wrapper.text()).not.toContain('Nothing has been shared with you yet');
  });

  it('picks up a failure of the address-book list too, and clears it on a successful retry', async () => {
    const { calendarStore, contactsStore } = setup();
    vi.mocked(contactsStore.fetchAddressBooks).mockImplementation(async () => {
      contactsStore.error = 'Failed to load address books';
    });

    const { vm } = await mountPage();
    expect(vm.loadError).toBe('Failed to load address books');

    // Retry: the backend is healthy again.
    vi.mocked(contactsStore.fetchAddressBooks).mockImplementation(async () => {});
    await vm.load();

    expect(vm.loadError).toBeNull();
    expect(calendarStore.fetchCalendars).toHaveBeenCalledTimes(2);
    expect(contactsStore.fetchAddressBooks).toHaveBeenCalledTimes(2);
  });

  it('still renders what DID load when only one of the two lists failed', async () => {
    const { calendarStore, contactsStore } = setup();
    calendarStore.calendars = [
      calendar({ uuid: 'shared-cal', name: 'Team', shared: true, owner: { id: '9', display_name: 'Dana' } }),
    ];
    vi.mocked(contactsStore.fetchAddressBooks).mockImplementation(async () => {
      contactsStore.error = 'Failed to load address books';
    });

    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain('Failed to load address books');
    expect(wrapper.text()).toContain('Team');
  });
});

describe('successful load', () => {
  it('lists only resources shared WITH the user, with owner and permission', async () => {
    const { calendarStore, contactsStore } = setup();
    calendarStore.calendars = [
      calendar({ uuid: 'own', name: 'Mine' }),
      calendar({
        uuid: 'shared-cal',
        name: 'Team',
        shared: true,
        permission: 'read-write',
        owner: { id: '9', display_name: 'Dana' },
      }),
    ];
    contactsStore.addressBooks = [
      book({ UUID: 'shared-ab', Name: 'Leads', shared: true, permission: 'read', owner: { id: '9', display_name: 'Dana' } }),
    ];

    const { wrapper, vm } = await mountPage();

    expect(vm.loadError).toBeNull();
    expect(wrapper.text()).toContain('Team');
    expect(wrapper.text()).toContain('shared by Dana');
    expect(wrapper.text()).toContain('Leads');
    // Owned resources never appear here — the whole point of the page.
    expect(wrapper.text()).not.toContain('Mine');
    expect(wrapper.html()).toContain('Can edit');
    expect(wrapper.html()).toContain('View only');
  });

  it('shows the empty state only when the load genuinely succeeded', async () => {
    setup();
    const { wrapper, vm } = await mountPage();

    expect(vm.loadError).toBeNull();
    expect(wrapper.text()).toContain('Nothing has been shared with you yet');
  });
});
