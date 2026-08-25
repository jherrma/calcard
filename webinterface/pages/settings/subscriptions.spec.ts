// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import SubscriptionsPage from './subscriptions.vue';
import type { CalendarSubscription } from '~/types/calendar';

// SCOPE: the parts of story 100's settings page that are load-bearing and
// would otherwise be covered by code only — that a failed refresh is reported
// as a failure even though the request returned 200, that a failed LOAD is not
// rendered as "you have no subscriptions", that removal is confirmed first,
// and that the calendar list is refreshed alongside every mutation. Each test
// names the revert it catches.

const { confirmRequire } = vi.hoisted(() => ({ confirmRequire: vi.fn() }));
vi.mock('primevue/useconfirm', () => ({
  useConfirm: () => ({ require: confirmRequire }),
}));

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

const { fetchCalendarsMock } = vi.hoisted(() => ({ fetchCalendarsMock: vi.fn() }));
mockNuxtImport('useCalendarStore', () => () => ({ fetchCalendars: fetchCalendarsMock }));

interface ConfirmOptions {
  header?: string;
  message?: string;
  accept?: () => void | Promise<void>;
}

function lastConfirm(): ConfirmOptions {
  return (confirmRequire.mock.calls.at(-1)?.[0] ?? {}) as ConfirmOptions;
}

interface PageVm {
  subscriptions: CalendarSubscription[];
  loadError: string | null;
  formError: string;
  addForm: { url: string; name: string; refreshInterval: string };
  handleAdd: () => Promise<void>;
  refresh: (sub: CalendarSubscription) => Promise<void>;
  confirmRemove: (sub: CalendarSubscription) => void;
  fetchSubscriptions: () => Promise<void>;
}

function sub(overrides: Partial<CalendarSubscription> = {}): CalendarSubscription {
  return {
    id: 'sub-1',
    calendar_id: 'cal-1',
    name: 'German holidays',
    description: '',
    color: '#3788d8',
    url: 'https://example.com/holidays.ics',
    refresh_interval: '1h',
    status: 'synced',
    enabled: true,
    last_synced_at: '2026-08-25T10:00:00Z',
    next_sync_at: '2026-08-25T11:00:00Z',
    last_error: '',
    error_count: 0,
    event_count: 42,
    created_at: '2026-08-01T00:00:00Z',
    ...overrides,
  };
}

async function mountPage() {
  const wrapper = mount(SubscriptionsPage, { shallow: true, global: { renderStubDefaultSlot: true } });
  await nextTick();
  await nextTick();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

beforeEach(() => {
  vi.clearAllMocks();
  apiMock.mockReset();
  apiMock.mockResolvedValue({ subscriptions: [] });
});

describe('listing', () => {
  it('renders a subscription with its feed, event count and status', async () => {
    apiMock.mockResolvedValue({ subscriptions: [sub()] });

    const { wrapper, vm } = await mountPage();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendar-subscriptions');
    expect(vm.subscriptions).toHaveLength(1);
    expect(wrapper.text()).toContain('German holidays');
    expect(wrapper.text()).toContain('https://example.com/holidays.ics');
    expect(wrapper.text()).toContain('42');
  });

  it('shows the empty state when the account has no subscriptions', async () => {
    const { wrapper } = await mountPage();
    expect(wrapper.text()).toContain('No subscriptions yet');
  });

  it('reports a failed load instead of claiming there are none', async () => {
    apiMock.mockRejectedValue(new Error('boom'));

    const { wrapper, vm } = await mountPage();

    expect(toastSpies.error).toHaveBeenCalledWith('Failed to load calendar subscriptions');
    expect(wrapper.text()).toContain('Failed to load calendar subscriptions');
    // REVERT PROOF: without the loadError branch the empty state claims the
    // account has no subscriptions, on no evidence — and hides the very feed
    // the user came here to fix.
    expect(wrapper.text()).not.toContain('No subscriptions yet');

    apiMock.mockReset();
    apiMock.mockResolvedValue({ subscriptions: [sub()] });
    await vm.fetchSubscriptions();
    expect(vm.loadError).toBeNull();
  });

  it('surfaces the failure reason on a broken subscription', async () => {
    apiMock.mockResolvedValue({
      subscriptions: [sub({ status: 'error', error_count: 2, last_error: 'HTTP 503: Service Unavailable' })],
    });

    const { wrapper } = await mountPage();

    // REVERT PROOF: without the reason the row looks the same as a working
    // one and the user has no way to tell why the calendar stopped updating.
    expect(wrapper.text()).toContain('HTTP 503: Service Unavailable');
  });

  it('explains how to resume a subscription that auto-disabled itself', async () => {
    apiMock.mockResolvedValue({
      subscriptions: [sub({ status: 'disabled', enabled: false, next_sync_at: null, last_error: 'HTTP 404: Not Found' })],
    });

    const { wrapper } = await mountPage();
    expect(wrapper.text()).toContain('Refresh manually to resume');
  });
});

describe('adding', () => {
  it('requires a URL before calling the API', async () => {
    const { vm } = await mountPage();
    apiMock.mockClear();

    vm.addForm.url = '   ';
    await vm.handleAdd();

    expect(vm.formError).toBe('Feed URL is required');
    expect(apiMock).not.toHaveBeenCalled();
  });

  it('posts the feed and refreshes both the list and the calendars', async () => {
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockResolvedValueOnce(sub());
    apiMock.mockResolvedValueOnce({ subscriptions: [sub()] });

    vm.addForm.url = ' https://example.com/holidays.ics ';
    vm.addForm.refreshInterval = '6h';
    await vm.handleAdd();

    expect(apiMock).toHaveBeenNthCalledWith(1, '/api/v1/calendar-subscriptions', {
      method: 'POST',
      body: { url: 'https://example.com/holidays.ics', name: '', refresh_interval: '6h' },
    });
    // REVERT PROOF: a subscription creates a CALENDAR, so without this the
    // sidebar keeps showing the old list until a full page reload.
    expect(fetchCalendarsMock).toHaveBeenCalled();
    expect(vm.subscriptions).toHaveLength(1);
  });

  it('shows the server’s reason rather than a generic failure', async () => {
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockRejectedValueOnce({ data: { message: 'The URL did not return iCalendar data' } });

    vm.addForm.url = 'https://example.com/not-a-feed';
    await vm.handleAdd();

    // The server's message is the only thing that tells the user what is wrong
    // with the URL they pasted.
    expect(vm.formError).toBe('The URL did not return iCalendar data');
  });
});

describe('refreshing', () => {
  it('reports a failed refresh as a failure even though it returned 200', async () => {
    apiMock.mockResolvedValue({ subscriptions: [sub()] });
    const { vm } = await mountPage();

    apiMock.mockReset();
    apiMock.mockResolvedValueOnce({
      ...sub({ status: 'error', last_error: 'HTTP 503: Service Unavailable' }),
      synced: false,
      created: 0,
      updated: 0,
      deleted: 0,
      skipped: 0,
    });
    apiMock.mockResolvedValueOnce({ subscriptions: [sub({ status: 'error' })] });

    await vm.refresh(sub());

    // REVERT PROOF: the endpoint answers 200 when the FEED fails — the request
    // itself worked. Treating that as success would toast "refreshed" over a
    // row that is about to render an error.
    expect(toastSpies.error).toHaveBeenCalledWith('HTTP 503: Service Unavailable');
    expect(toastSpies.success).not.toHaveBeenCalled();
  });

  it('reports a successful refresh and reloads the calendars', async () => {
    apiMock.mockResolvedValue({ subscriptions: [sub()] });
    const { vm } = await mountPage();

    apiMock.mockReset();
    apiMock.mockResolvedValueOnce({ ...sub(), synced: true, created: 1, updated: 0, deleted: 2, skipped: 0 });
    apiMock.mockResolvedValueOnce({ subscriptions: [sub()] });

    await vm.refresh(sub());

    expect(apiMock).toHaveBeenNthCalledWith(1, '/api/v1/calendar-subscriptions/sub-1/refresh', { method: 'POST' });
    expect(toastSpies.success).toHaveBeenCalledWith('"German holidays" refreshed');
    // The refresh changed which events exist, so the grid must reload too.
    expect(fetchCalendarsMock).toHaveBeenCalled();
  });
});

describe('removing', () => {
  it('confirms first, warning that the calendar goes too', async () => {
    apiMock.mockResolvedValue({ subscriptions: [sub()] });
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockResolvedValue(undefined);

    vm.confirmRemove(vm.subscriptions[0]!);

    // REVERT PROOF: removing without confirmation makes deleting a calendar
    // and its 42 events a single mis-click.
    expect(confirmRequire).toHaveBeenCalled();
    expect(lastConfirm().message).toContain('German holidays');
    expect(lastConfirm().message).toContain('42');

    await lastConfirm().accept?.();
    await nextTick();

    expect(apiMock).toHaveBeenCalledWith('/api/v1/calendar-subscriptions/sub-1', { method: 'DELETE' });
    expect(vm.subscriptions).toHaveLength(0);
    expect(fetchCalendarsMock).toHaveBeenCalled();
    expect(toastSpies.success).toHaveBeenCalled();
  });

  it('keeps the row when the delete fails', async () => {
    apiMock.mockResolvedValue({ subscriptions: [sub()] });
    const { vm } = await mountPage();
    apiMock.mockReset();
    apiMock.mockRejectedValue(new Error('nope'));

    vm.confirmRemove(vm.subscriptions[0]!);
    await lastConfirm().accept?.();
    await nextTick();

    // Removing it optimistically would tell the user a still-live feed is gone.
    expect(vm.subscriptions).toHaveLength(1);
    expect(toastSpies.error).toHaveBeenCalledWith('Failed to remove the subscription');
  });
});
