// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import CalendarPage from './index.vue';
import { useCalendarStore } from '~/stores/calendars';
import type { CalendarEvent } from '~/types/calendar';

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

// Global search deep-links into this page via the route query (story 044), so the
// route is mocked with a mutable object each test can rewrite before mounting.
// Default {} keeps the pre-existing specs on the "no deep link" path.
const { routeStub, navigateToMock } = vi.hoisted(() => ({
  routeStub: { query: {} as Record<string, string> },
  navigateToMock: vi.fn(),
}));
mockNuxtImport('useRoute', () => () => routeStub as never);
mockNuxtImport('navigateTo', () => navigateToMock);

interface CalendarApi {
  changeView: ReturnType<typeof vi.fn>;
  gotoDate: ReturnType<typeof vi.fn>;
}
interface PageVm {
  changeView: (view: string | null | undefined) => void;
  applyDeepLink: () => Promise<void>;
  selectedEvent: CalendarEvent | null;
  showDetailDialog: boolean;
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
  const api: CalendarApi = { changeView: vi.fn(), gotoDate: vi.fn() };
  vm.calendarRef = { getApi: () => api };
  return { wrapper, vm, api, store: useCalendarStore() };
}

beforeEach(() => {
  vi.stubGlobal('$fetch', vi.fn());
  routeStub.query = {};
  navigateToMock.mockReset();
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

// SCOPE: the global-search deep link (story 044). /calendar?date=&event=&cal=
// must jump the view to the date and open that event's detail dialog, then drop
// the params so re-picking the same result (or a reload) behaves sanely.
describe('global search deep link (story 044)', () => {
  function occurrence(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
    return {
      id: 'ev-1',
      calendar_id: 7,
      uid: 'uid-1',
      summary: 'Team Standup',
      start: '2026-06-16T09:00:00+02:00',
      end: '2026-06-16T09:30:00+02:00',
      all_day: false,
      is_recurring: false,
      ...overrides,
    };
  }

  // onMounted runs applyDeepLink once — that IS the first-load path, but it fires
  // before a test can seed store.events, so reset the observers afterwards and let
  // each test assert its own explicit invocation.
  async function mountForDeepLink() {
    const ctx = await mountPage();
    vi.mocked(ctx.store.getEvent).mockReset();
    vi.mocked(ctx.store.fetchEventOccurrence).mockReset();
    vi.mocked(ctx.store.setCurrentDate).mockClear();
    navigateToMock.mockReset();
    return ctx;
  }

  it('does nothing without deep-link params', async () => {
    const { vm, api, store } = await mountForDeepLink();

    await vm.applyDeepLink();

    expect(api.gotoDate).not.toHaveBeenCalled();
    expect(store.getEvent).not.toHaveBeenCalled();
    expect(navigateToMock).not.toHaveBeenCalled();
    expect(vm.showDetailDialog).toBe(false);
  });

  it('jumps to the date, fetches the event and strips the params', async () => {
    routeStub.query = { date: '2026-06-16', event: 'ev-1', cal: '7' };
    const { vm, api, store } = await mountForDeepLink();
    // No `recurrence` param: a plain event is resolved by id, which is what
    // GET /events/:id can answer correctly.
    vi.mocked(store.getEvent).mockResolvedValue(occurrence());

    await vm.applyDeepLink();

    // Parsed as LOCAL midnight — a bare 'YYYY-MM-DD' would be UTC and could land
    // on the 15th west of Greenwich.
    const jumped = api.gotoDate.mock.calls[0]![0] as Date;
    expect([jumped.getFullYear(), jumped.getMonth(), jumped.getDate()]).toEqual([2026, 5, 16]);
    expect(store.setCurrentDate).toHaveBeenCalled();
    // The numeric calendar id from the link; the store maps it to the UUID (#52).
    expect(store.getEvent).toHaveBeenCalledWith('7', 'ev-1');
    expect(vm.showDetailDialog).toBe(true);
    expect(navigateToMock).toHaveBeenCalledWith({ path: '/calendar', query: {} }, { replace: true });
  });

  // REVERT PROOF for the recurring deep link: the target date is normally OUTSIDE
  // the range FullCalendar has fetched (the refetch that gotoDate triggers is async
  // and lands later), so store.events misses and the old code fell through to
  // getEvent — which returns the series MASTER because GET /events/:id does no
  // recurrence expansion. That opened a dialog for 5 January while the grid sat on
  // 15 September. The occurrence is now resolved from the expanded target day.
  it('resolves a recurring occurrence from the target day instead of the series master', async () => {
    routeStub.query = {
      date: '2026-09-15',
      event: 'ev-1',
      cal: '7',
      recurrence: '2026-09-15T07:00:00Z',
    };
    const { vm, store } = await mountForDeepLink();
    // store.events holds the CURRENT month only — the September occurrence is absent.
    store.events = [];
    const resolved = occurrence({
      start: '2026-09-15T09:00:00+02:00',
      end: '2026-09-15T09:30:00+02:00',
      recurrence_id: '2026-09-15T07:00:00Z',
      is_recurring: true,
    });
    vi.mocked(store.fetchEventOccurrence).mockResolvedValue(resolved);

    await vm.applyDeepLink();

    expect(store.fetchEventOccurrence).toHaveBeenCalledWith(
      '7',
      'ev-1',
      '2026-09-15T07:00:00Z',
      expect.any(Date)
    );
    // The series master (getEvent) must NOT be what opens.
    expect(store.getEvent).not.toHaveBeenCalled();
    expect(vm.selectedEvent?.start).toBe('2026-09-15T09:00:00+02:00');
    expect(vm.showDetailDialog).toBe(true);
  });

  it('warns instead of opening the wrong date when the occurrence is gone', async () => {
    routeStub.query = {
      date: '2026-09-15',
      event: 'ev-1',
      cal: '7',
      recurrence: '2026-09-15T07:00:00Z',
    };
    const { vm, store } = await mountForDeepLink();
    store.events = [];
    vi.mocked(store.fetchEventOccurrence).mockResolvedValue(null);

    await vm.applyDeepLink();

    expect(vm.showDetailDialog).toBe(false);
    expect(store.getEvent).not.toHaveBeenCalled();
  });

  it('keeps unrelated query params when consuming the deep link', async () => {
    routeStub.query = { date: '2026-06-16', view: 'timeGridWeek' };
    const { vm } = await mountForDeepLink();

    await vm.applyDeepLink();

    // Only the four deep-link params are stripped — a param another story adds must
    // survive being consumed here.
    expect(navigateToMock).toHaveBeenCalledWith(
      { path: '/calendar', query: { view: 'timeGridWeek' } },
      { replace: true }
    );
  });

  it('prefers the already-loaded occurrence over refetching the series', async () => {
    routeStub.query = {
      date: '2026-06-16',
      event: 'ev-1',
      cal: '7',
      recurrence: '2026-06-16T07:00:00Z',
    };
    const { vm, store } = await mountForDeepLink();
    store.events = [
      occurrence({ recurrence_id: '2026-06-15T07:00:00Z', summary: 'Wrong occurrence' }),
      occurrence({ recurrence_id: '2026-06-16T07:00:00Z' }),
    ];

    await vm.applyDeepLink();

    // Matching on id alone would have opened the 15th's occurrence (every expanded
    // instance shares the series id).
    expect(vm.selectedEvent?.recurrence_id).toBe('2026-06-16T07:00:00Z');
    expect(vm.selectedEvent?.summary).toBe('Team Standup');
    expect(store.getEvent).not.toHaveBeenCalled();
    expect(vm.showDetailDialog).toBe(true);
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
