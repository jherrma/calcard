// @vitest-environment nuxt
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createTestingPinia } from '@pinia/testing';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import ImportExportPage from './import-export.vue';
import { useCalendarStore } from '~/stores/calendars';
import type { Calendar } from '~/types/calendar';
import type { AddressBook } from '~/types/contacts';
import type { ImportResult } from '~/types/importexport';

// SCOPE (#170): the .ics import and export selectors. GET /calendars returns
// task lists (`kind: 'tasks'`) next to the calendars; this page is about
// events, and the server skips every event of a file imported into a task list
// (story 03), so neither selector may offer one. Also pins the results label:
// a calendar import's "skipped" now includes the items the target calendar does
// not store (a file's tasks imported into an events-only calendar).

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

const { toastSpies } = vi.hoisted(() => ({
  toastSpies: { success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() },
}));
mockNuxtImport('useAppToast', () => () => toastSpies);

interface PageVm {
  calFile: File | null;
  selectedCalendar: Calendar | null;
  vcfFile: File | null;
  selectedAddressBook: AddressBook | null;
  executeCalendarImport: () => Promise<void>;
  executeContactImport: () => Promise<void>;
}

function calendar(id: number, extra: Partial<Calendar> = {}): Calendar {
  return {
    id: String(id),
    uuid: `cal-uuid-${id}`,
    path: `${id}.ics`,
    name: `Cal ${id}`,
    color: '#123456',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

const MIXED_ACCOUNT = [
  calendar(1, { name: 'Personal', kind: 'events' }),
  calendar(2, { name: 'Tasks', kind: 'tasks' }),
  calendar(3, { name: 'Old mixed', kind: 'both' }),
  calendar(4, { name: 'Team tasks', kind: 'tasks', shared: true, permission: 'read-write' }),
];

async function mountPage(calendars: Calendar[]) {
  // stubActions: true — onMounted refetches both lists, which would replace the
  // calendars seeded here.
  const pinia = createTestingPinia({ stubActions: true });
  useCalendarStore().calendars = calendars;
  const wrapper = mount(ImportExportPage, {
    shallow: true,
    global: { plugins: [pinia], renderStubDefaultSlot: true },
  });
  await flushPromises();
  return { wrapper, vm: wrapper.vm as unknown as PageVm };
}

type Wrapper = Awaited<ReturnType<typeof mountPage>>['wrapper'];

/**
 * The calendar `Select`s (import target, export source), in template order.
 * Select's props come from a base component via `extends`, which the shallow
 * stub does not copy, so `options` arrives as an attr (see calendar.spec.ts).
 */
function calendarSelects(wrapper: Wrapper) {
  return wrapper
    .findAllComponents({ name: 'Select' })
    .filter((s) => s.attributes('placeholder') === 'Select a calendar');
}

function optionNames(select: ReturnType<typeof calendarSelects>[number]): string[] {
  const options = (select.props('options') ?? select.vm.$attrs.options) as Calendar[];
  return options.map((c) => c.name);
}

beforeEach(() => {
  apiMock.mockReset();
  vi.clearAllMocks();
});

describe('import/export selectors never offer a task list (#170)', () => {
  it('the .ics import target lists event and mixed calendars only', async () => {
    const { wrapper, vm } = await mountPage(MIXED_ACCOUNT);
    // The import target only renders once a file is chosen.
    vm.calFile = new File(['BEGIN:VCALENDAR'], 'events.ics');
    await nextTick();

    const selects = calendarSelects(wrapper);
    expect(selects).toHaveLength(2);
    // REVERT PROOF: bound to `calendars`, "Tasks" and "Team tasks" are offered.
    expect(optionNames(selects[0]!)).toEqual(['Personal', 'Old mixed']);
  });

  it('the .ics export source lists event and mixed calendars only', async () => {
    const { wrapper } = await mountPage(MIXED_ACCOUNT);

    // Without a file only the export selector is rendered.
    const selects = calendarSelects(wrapper);
    expect(selects).toHaveLength(1);
    expect(optionNames(selects[0]!)).toEqual(['Personal', 'Old mixed']);
  });

  it('offers nothing to a user who only has task lists', async () => {
    const { wrapper, vm } = await mountPage([
      calendar(1, { name: 'Tasks', kind: 'tasks' }),
      calendar(2, { name: 'Shopping', kind: 'tasks' }),
    ]);
    vm.calFile = new File(['BEGIN:VCALENDAR'], 'events.ics');
    await nextTick();

    const selects = calendarSelects(wrapper);
    expect(selects).toHaveLength(2);
    expect(selects.map(optionNames)).toEqual([[], []]);
  });
});

describe('import results label', () => {
  const result = (extra: Partial<ImportResult> = {}): ImportResult => ({
    total: 4,
    imported: 1,
    skipped: 3,
    failed: 0,
    ...extra,
  });

  it('a calendar import says skipped items may be ones the calendar does not store', async () => {
    const { wrapper, vm } = await mountPage(MIXED_ACCOUNT);
    apiMock.mockResolvedValueOnce(
      result({ errors: [{ index: 2, uid: 'task-1', summary: 'Buy milk', error: 'This calendar does not store tasks' }] }),
    );
    vm.calFile = new File(['BEGIN:VCALENDAR'], 'mixed.ics');
    vm.selectedCalendar = MIXED_ACCOUNT[0]!;

    await vm.executeCalendarImport();
    await nextTick();

    expect(apiMock.mock.calls[0]![0]).toBe('/api/v1/calendars/cal-uuid-1/import?duplicate_handling=skip');
    const text = wrapper.text();
    expect(text).toContain('3 skipped (duplicates, or items this calendar does not store)');
    expect(text).toContain('Buy milk: This calendar does not store tasks');
  });

  it('a contact import still says duplicates', async () => {
    const { wrapper, vm } = await mountPage(MIXED_ACCOUNT);
    apiMock.mockResolvedValueOnce(result());
    vm.vcfFile = new File(['BEGIN:VCARD'], 'people.vcf');
    vm.selectedAddressBook = {
      ID: 1,
      UUID: 'ab-uuid-1',
      Path: 'default',
      Name: 'Contacts',
      Description: '',
      CreatedAt: '2026-01-01T00:00:00Z',
      UpdatedAt: '2026-01-01T00:00:00Z',
    };

    await vm.executeContactImport();
    await nextTick();

    const text = wrapper.text();
    expect(text).toContain('3 skipped (duplicates)');
    expect(text).not.toContain('does not store');
  });
});
