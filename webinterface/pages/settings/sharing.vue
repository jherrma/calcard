<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">Shared with me</h2>
    <p class="text-sm text-surface-500 mb-6">
      Calendars and address books that other people gave you access to. To share something of your
      own, open its menu in the calendar or contacts sidebar and pick <em>Share</em>.
    </p>

    <CommonLoadingSpinner v-if="isLoading" />

    <template v-else>
      <!-- Both list actions swallow failures, so `rows` is empty whether nothing
           is shared OR nothing could be loaded. Rendering the empty state for a
           failed load would assert "nothing has been shared with you" on no
           evidence, so the error wins and offers a retry. -->
      <Message v-if="loadError" severity="error" :closable="false">
        <div class="flex flex-wrap items-center gap-3">
          <span>{{ loadError }}</span>
          <Button label="Retry" icon="pi pi-refresh" severity="secondary" size="small" @click="load" />
        </div>
      </Message>

      <div
        v-if="!loadError && rows.length === 0"
        class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-8 text-center"
      >
        <i class="pi pi-users text-3xl text-surface-300 dark:text-surface-600" />
        <p class="text-surface-500 mt-3">Nothing has been shared with you yet.</p>
        <p class="text-xs text-surface-400 mt-1">
          When someone shares a calendar or address book with you it shows up here, and in the
          sidebar of the matching page.
        </p>
      </div>

      <!-- Rows still render alongside an error: one list can fail while the
           other succeeds, and hiding what DID load helps nobody. -->
      <div v-if="rows.length" class="space-y-3">
        <div
          v-for="row in rows"
          :key="`${row.type}-${row.uuid}`"
          class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4"
        >
          <div class="flex items-center justify-between gap-4">
            <div class="flex items-center gap-3 min-w-0">
              <span
                v-if="row.type === 'calendar'"
                class="w-3 h-3 rounded-full flex-shrink-0"
                :style="{ backgroundColor: row.color || '#3788d8' }"
              />
              <i v-else class="pi pi-book text-surface-500" />
              <div class="min-w-0">
                <div class="font-medium text-surface-900 dark:text-surface-0 truncate">{{ row.name }}</div>
                <div class="text-sm text-surface-500">
                  {{ row.type === 'calendar' ? 'Calendar' : 'Address book' }} · shared by {{ row.ownerName }}
                </div>
              </div>
            </div>
            <Tag
              :value="row.permission === 'read-write' ? 'Can edit' : 'View only'"
              :severity="row.permission === 'read-write' ? 'success' : 'info'"
            />
          </div>
        </div>

        <!-- Both omissions are backend gaps, not UI choices — see story 043's
             deferred list. Saying so beats a control that silently does nothing. -->
        <p class="text-xs text-surface-400 pt-2">
          Hiding a shared item, or leaving it, is not supported yet — ask the owner to revoke your
          access. You can still hide a shared calendar for the current session from the calendar
          sidebar.
        </p>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useCalendarStore } from '~/stores/calendars';
import { useContactsStore } from '~/stores/contacts';
import { useSharingStore } from '~/stores/sharing';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const calendarStore = useCalendarStore();
const contactsStore = useContactsStore();
const sharingStore = useSharingStore();

const isLoading = ref(true);
const loadError = ref<string | null>(null);

// Derived from the two LIST endpoints: there is no /shared-with-me endpoint,
// but every listed resource carries shared / permission / owner (#53).
const rows = computed(() => sharingStore.sharedWithMe);

/**
 * Neither fetchCalendars() nor fetchAddressBooks() rejects — both swallow the
 * failure into their own store's `error` (the calendar and contacts pages rely
 * on that). So clear those fields first and read them back afterwards; that is
 * the only way this page can tell "nothing is shared" from "the load failed".
 */
const load = async () => {
  isLoading.value = true;
  loadError.value = null;
  calendarStore.error = null;
  contactsStore.error = null;
  try {
    await Promise.all([
      calendarStore.fetchCalendars(),
      contactsStore.fetchAddressBooks(),
    ]);
    loadError.value = calendarStore.error || contactsStore.error || null;
  } finally {
    isLoading.value = false;
  }
};

onMounted(load);
</script>
