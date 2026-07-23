// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import CalendarPage from './index.vue';
import { useCalendarStore } from '~/stores/calendars';

// SCOPE: the changeView() guard on the calendar page (#26). The view SelectButton
// can deselect and emit `null`; before the fix changeView passed that straight to
// FullCalendar's getApi().changeView(null), which throws and blanks the calendar.
// The guard drops any value that isn't a known view. These tests inject a fake
// FullCalendar api and assert the happy path still switches while null / unknown
// views are no-ops. REVERT PROOF: with the guard removed, the null case calls
// api.changeView(null) and the "not.toHaveBeenCalled" assertion fails.

// useConfirm() (explicit import) needs a ConfirmationService provider a bare mount
// lacks — stub the module so the page's setup runs.
vi.mock('primevue/useconfirm', () => ({
  useConfirm: () => ({ require: vi.fn() }),
}));

// useAppToast() calls PrimeVue's useToast(), which needs a ToastService provider;
// replace it with an inert stub (self-contained factory — mockNuxtImport hoists it).
mockNuxtImport('useAppToast', () => {
  return () => ({
    success: vi.fn(),
    error: vi.fn(),
    warn: vi.fn(),
    info: vi.fn(),
  });
});

interface CalendarApi {
  changeView: ReturnType<typeof vi.fn>;
}
interface PageVm {
  changeView: (view: string | null | undefined) => void;
  calendarRef: { getApi: () => CalendarApi } | undefined;
  calendarOptions: {
    slotMinTime?: string;
    slotMaxTime?: string;
    scrollTime?: string;
    [k: string]: unknown;
  };
}

async function mountPage() {
  const pinia = createTestingPinia({ stubActions: true });
  const wrapper = mount(CalendarPage, {
    shallow: true, // stub FullCalendar + all dialog children
    global: { plugins: [pinia] },
  });
  await nextTick();
  const vm = wrapper.vm as unknown as PageVm;
  // Inject a fake FullCalendar api so the happy path is observable without a real
  // FullCalendar instance (it never mounts under a shallow ClientOnly stub).
  const api: CalendarApi = { changeView: vi.fn() };
  vm.calendarRef = { getApi: () => api };
  return { wrapper, vm, api, store: useCalendarStore() };
}

beforeEach(() => {
  vi.stubGlobal('$fetch', vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('calendar page changeView guard (#26)', () => {
  it('switches to a valid view through the FullCalendar api', async () => {
    const { vm, api, store } = await mountPage();

    vm.changeView('timeGridWeek');

    expect(api.changeView).toHaveBeenCalledWith('timeGridWeek');
    expect(store.setCurrentView).toHaveBeenCalledWith('timeGridWeek');
  });

  it('ignores a null view (SelectButton deselect) instead of crashing FullCalendar', async () => {
    const { vm, api, store } = await mountPage();

    vm.changeView(null);

    // Belt-and-suspenders guard: null never reaches FullCalendar or the store.
    expect(api.changeView).not.toHaveBeenCalled();
    expect(store.setCurrentView).not.toHaveBeenCalled();
  });

  it('ignores an unknown view string', async () => {
    const { vm, api } = await mountPage();

    vm.changeView('listWeek');

    expect(api.changeView).not.toHaveBeenCalled();
  });
});

// SCOPE: the time-axis window on week/day views (#28). The old options clamped
// the axis to 06:00–22:00, hiding events outside those hours with no scroll
// access. The fix shows the full day and only scrolls the viewport to the
// morning. REVERT PROOF: restore slotMinTime '06:00:00' / slotMaxTime '22:00:00'
// and these assertions fail.
describe('calendar time-axis window (#28)', () => {
  it('exposes the full 24h day and scrolls to the morning instead of clamping 06:00–22:00', async () => {
    const { vm } = await mountPage();

    const opts = vm.calendarOptions;
    expect(opts.slotMinTime).toBe('00:00:00');
    expect(opts.slotMaxTime).toBe('24:00:00');
    // Viewport still opens around the morning hours.
    expect(opts.scrollTime).toBe('07:00:00');
  });
});
