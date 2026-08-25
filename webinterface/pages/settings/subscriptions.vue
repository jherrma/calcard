<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-2">Calendar Subscriptions</h2>
    <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">
      Follow a calendar published elsewhere — a holiday calendar, a sports schedule,
      an export from another service. The server fetches the feed on a schedule and
      mirrors it into a calendar of your own. Subscribed calendars are read-only:
      the next refresh replaces their contents, so anything you added would be lost.
    </p>

    <div class="mb-6">
      <Button label="Add Subscription" icon="pi pi-plus" @click="showAddDialog = true" />
    </div>

    <CommonLoadingSpinner v-if="loading" />

    <!-- A failed load must never render as "you have no subscriptions": that is
         a false statement about the account, and it hides the very feed the
         user came here to fix. -->
    <Message v-else-if="loadError" severity="error" :closable="false" class="mb-4">
      <div class="flex items-center justify-between gap-4">
        <span>{{ loadError }}</span>
        <Button label="Retry" icon="pi pi-refresh" severity="secondary" size="small" @click="fetchSubscriptions" />
      </div>
    </Message>

    <div
      v-else-if="subscriptions.length === 0"
      class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-8 text-center"
    >
      <i class="pi pi-cloud-download text-4xl text-surface-300 dark:text-surface-600 mb-3" />
      <p class="text-surface-500 dark:text-surface-400">
        No subscriptions yet. Add a feed URL to follow a calendar published elsewhere.
      </p>
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="sub in subscriptions"
        :key="sub.id"
        class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 mb-1 flex-wrap">
              <span class="w-3 h-3 rounded-full flex-shrink-0" :style="{ backgroundColor: sub.color }" />
              <span class="font-medium text-surface-900 dark:text-surface-0">{{ sub.name }}</span>
              <Tag :value="statusLabel(sub)" :severity="statusSeverity(sub)" />
            </div>

            <div class="text-sm text-surface-500 dark:text-surface-400 space-y-0.5">
              <div class="truncate" :title="sub.url">
                <span class="font-medium">Feed:</span>
                <span class="font-mono text-xs">{{ sub.url }}</span>
              </div>
              <div>
                <span class="font-medium">Events:</span> {{ sub.event_count }}
                <span class="mx-1">·</span>
                <span class="font-medium">Refreshes:</span> {{ intervalLabel(sub.refresh_interval) }}
              </div>
              <div v-if="sub.last_synced_at">
                <span class="font-medium">Last refreshed:</span> {{ formatRelative(sub.last_synced_at) }}
              </div>
              <div v-else>Never refreshed</div>
              <div v-if="sub.next_sync_at">
                <span class="font-medium">Next refresh:</span> {{ formatDateTime(sub.next_sync_at) }}
              </div>
            </div>

            <!-- The reason is written by the server for exactly this spot; it
                 never contains the feed URL, which may carry a secret token. -->
            <Message v-if="sub.last_error" severity="warn" :closable="false" class="mt-2 text-sm">
              {{ sub.last_error }}
              <span v-if="sub.status === 'disabled'">
                Automatic refreshes are paused. Refresh manually to resume them.
              </span>
            </Message>
          </div>

          <div class="flex items-center gap-1 flex-shrink-0">
            <Button
              icon="pi pi-refresh"
              severity="secondary"
              text
              rounded
              aria-label="Refresh now"
              :loading="refreshingId === sub.id"
              @click="refresh(sub)"
            />
            <Button
              icon="pi pi-pencil"
              severity="secondary"
              text
              rounded
              aria-label="Edit subscription"
              @click="openEdit(sub)"
            />
            <Button
              icon="pi pi-trash"
              severity="danger"
              text
              rounded
              aria-label="Remove subscription"
              @click="confirmRemove(sub)"
            />
          </div>
        </div>
      </div>
    </div>

    <!-- Add dialog -->
    <Dialog
      v-model:visible="showAddDialog"
      header="Add Calendar Subscription"
      :modal="true"
      :style="{ width: '32rem' }"
      :closable="!saving"
      @hide="resetAddForm"
    >
      <form class="space-y-4" @submit.prevent="handleAdd">
        <div class="flex flex-col gap-2">
          <label for="sub-url" class="text-sm font-medium text-surface-700 dark:text-surface-300">Feed URL</label>
          <InputText
            id="sub-url"
            v-model="addForm.url"
            placeholder="https://example.com/calendar.ics"
            class="w-full font-mono text-sm"
            :disabled="saving"
          />
          <small class="text-surface-500 dark:text-surface-400">
            An iCalendar (.ics) feed. webcal:// links work too.
          </small>
        </div>

        <div class="flex flex-col gap-2">
          <label for="sub-name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Name</label>
          <InputText id="sub-name" v-model="addForm.name" placeholder="Taken from the feed" class="w-full" :disabled="saving" />
        </div>

        <div class="flex flex-col gap-2">
          <label for="sub-interval" class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Refresh every
          </label>
          <Select
            id="sub-interval"
            v-model="addForm.refreshInterval"
            :options="intervalOptions"
            option-label="label"
            option-value="value"
            class="w-full"
            :disabled="saving"
          />
        </div>

        <Message v-if="formError" severity="error" :closable="true" @close="formError = ''">
          {{ formError }}
        </Message>

        <p class="text-xs text-surface-500 dark:text-surface-400">
          The feed is fetched now to check it works, so this can take a moment.
        </p>

        <div class="flex justify-end gap-2 pt-2">
          <Button label="Cancel" severity="secondary" text :disabled="saving" @click="showAddDialog = false" />
          <Button type="submit" label="Subscribe" icon="pi pi-plus" :loading="saving" />
        </div>
      </form>
    </Dialog>

    <!-- Edit dialog -->
    <Dialog
      v-model:visible="showEditDialog"
      header="Edit Subscription"
      :modal="true"
      :style="{ width: '32rem' }"
      :closable="!saving"
    >
      <form class="space-y-4" @submit.prevent="handleEdit">
        <div class="flex flex-col gap-2">
          <label for="edit-name" class="text-sm font-medium text-surface-700 dark:text-surface-300">Name</label>
          <InputText id="edit-name" v-model="editForm.name" class="w-full" :disabled="saving" />
        </div>

        <div class="flex flex-col gap-2">
          <label for="edit-color" class="text-sm font-medium text-surface-700 dark:text-surface-300">Colour</label>
          <ColorPicker id="edit-color" v-model="editForm.color" format="hex" :disabled="saving" />
        </div>

        <div class="flex flex-col gap-2">
          <label for="edit-url" class="text-sm font-medium text-surface-700 dark:text-surface-300">Feed URL</label>
          <InputText id="edit-url" v-model="editForm.url" class="w-full font-mono text-sm" :disabled="saving" />
          <small class="text-surface-500 dark:text-surface-400">
            Changing this refreshes the calendar from the new feed straight away.
          </small>
        </div>

        <div class="flex flex-col gap-2">
          <label for="edit-interval" class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Refresh every
          </label>
          <Select
            id="edit-interval"
            v-model="editForm.refreshInterval"
            :options="intervalOptions"
            option-label="label"
            option-value="value"
            class="w-full"
            :disabled="saving"
          />
        </div>

        <Message v-if="formError" severity="error" :closable="true" @close="formError = ''">
          {{ formError }}
        </Message>

        <div class="flex justify-end gap-2 pt-2">
          <Button label="Cancel" severity="secondary" text :disabled="saving" @click="showEditDialog = false" />
          <Button type="submit" label="Save" :loading="saving" />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import type {
  CalendarSubscription,
  CalendarSubscriptionListResponse,
  CalendarSubscriptionRefreshResponse,
} from '~/types/calendar';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const api = useApi();
const toast = useAppToast();
const confirm = useConfirm();
const calendarStore = useCalendarStore();

const loading = ref(true);
const loadError = ref<string | null>(null);
const saving = ref(false);
const formError = ref('');
const refreshingId = ref<string | null>(null);
const subscriptions = ref<CalendarSubscription[]>([]);

const showAddDialog = ref(false);
const showEditDialog = ref(false);
const editingId = ref<string | null>(null);

// The server accepts only this closed set; offering a free-text duration would
// invite a value it rejects.
const intervalOptions = [
  { label: 'Every 15 minutes', value: '15m' },
  { label: 'Every 30 minutes', value: '30m' },
  { label: 'Every hour', value: '1h' },
  { label: 'Every 6 hours', value: '6h' },
  { label: 'Every 12 hours', value: '12h' },
  { label: 'Once a day', value: '24h' },
];

const addForm = reactive({ url: '', name: '', refreshInterval: '1h' });
const editForm = reactive({ name: '', color: '', url: '', refreshInterval: '1h' });

const intervalLabel = (value: string) =>
  intervalOptions.find(o => o.value === value)?.label ?? value;

const statusLabel = (sub: CalendarSubscription) => {
  switch (sub.status) {
    case 'synced':
      return 'up to date';
    case 'error':
      return 'refresh failed';
    case 'disabled':
      return 'paused';
    default:
      return 'not refreshed yet';
  }
};

const statusSeverity = (sub: CalendarSubscription) => {
  switch (sub.status) {
    case 'synced':
      return 'success';
    case 'error':
      return 'warn';
    case 'disabled':
      return 'danger';
    default:
      return 'secondary';
  }
};

const formatDateTime = (value: string) =>
  new Date(value).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });

const formatRelative = (value: string) => {
  const diffMins = Math.floor((Date.now() - new Date(value).getTime()) / 60000);
  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 30) return `${diffDays}d ago`;
  return formatDateTime(value);
};

// The server sends #RRGGBB; PrimeVue's ColorPicker works in bare hex.
const stripHash = (color: string) => color.replace(/^#/, '');
const withHash = (color: string) => (color.startsWith('#') ? color : `#${color}`);

const fetchSubscriptions = async () => {
  loading.value = true;
  try {
    const data = await api<CalendarSubscriptionListResponse>('/api/v1/calendar-subscriptions');
    subscriptions.value = data.subscriptions || [];
    loadError.value = null;
  } catch {
    loadError.value = 'Failed to load calendar subscriptions. Your existing subscriptions are unaffected.';
    toast.error('Failed to load calendar subscriptions');
  } finally {
    loading.value = false;
  }
};

// Every mutation here changes the calendar list too — a new subscription adds a
// calendar, a rename or recolour changes one, a removal deletes one — so the
// calendar store is refreshed alongside, or the sidebar keeps showing the old
// state until a full reload.
const refreshCalendars = () => calendarStore.fetchCalendars();

const resetAddForm = () => {
  addForm.url = '';
  addForm.name = '';
  addForm.refreshInterval = '1h';
  formError.value = '';
};

const errorText = (e: unknown, fallback: string) =>
  (e as { data?: { message?: string } })?.data?.message || fallback;

const handleAdd = async () => {
  if (!addForm.url.trim()) {
    formError.value = 'Feed URL is required';
    return;
  }

  saving.value = true;
  formError.value = '';
  try {
    await api<CalendarSubscription>('/api/v1/calendar-subscriptions', {
      method: 'POST',
      body: {
        url: addForm.url.trim(),
        name: addForm.name.trim(),
        refresh_interval: addForm.refreshInterval,
      },
    });
    showAddDialog.value = false;
    await Promise.all([fetchSubscriptions(), refreshCalendars()]);
    toast.success('Subscription added');
  } catch (e: unknown) {
    formError.value = errorText(e, 'Failed to add the subscription');
  } finally {
    saving.value = false;
  }
};

const openEdit = (sub: CalendarSubscription) => {
  editingId.value = sub.id;
  editForm.name = sub.name;
  editForm.color = stripHash(sub.color);
  editForm.url = sub.url;
  editForm.refreshInterval = sub.refresh_interval;
  formError.value = '';
  showEditDialog.value = true;
};

const handleEdit = async () => {
  if (!editingId.value) return;

  saving.value = true;
  formError.value = '';
  try {
    await api<CalendarSubscription>(`/api/v1/calendar-subscriptions/${editingId.value}`, {
      method: 'PATCH',
      body: {
        name: editForm.name.trim(),
        color: withHash(editForm.color),
        url: editForm.url.trim(),
        refresh_interval: editForm.refreshInterval,
      },
    });
    showEditDialog.value = false;
    await Promise.all([fetchSubscriptions(), refreshCalendars()]);
    toast.success('Subscription updated');
  } catch (e: unknown) {
    formError.value = errorText(e, 'Failed to update the subscription');
  } finally {
    saving.value = false;
  }
};

const refresh = async (sub: CalendarSubscription) => {
  refreshingId.value = sub.id;
  try {
    const result = await api<CalendarSubscriptionRefreshResponse>(
      `/api/v1/calendar-subscriptions/${sub.id}/refresh`,
      { method: 'POST' },
    );
    // A failed refresh answers 200 with the reason on the subscription: the
    // request worked, the third-party feed is what did not. Reporting success
    // here would contradict the error the row is about to render.
    if (result.synced) {
      toast.success(`"${result.name}" refreshed`);
    } else {
      toast.error(result.last_error || 'The feed could not be refreshed');
    }
    await Promise.all([fetchSubscriptions(), refreshCalendars()]);
  } catch {
    toast.error('Failed to refresh the subscription');
  } finally {
    refreshingId.value = null;
  }
};

const confirmRemove = (sub: CalendarSubscription) => {
  confirm.require({
    message: `Remove "${sub.name}"? The mirrored calendar and its ${sub.event_count} events are deleted too. Subscribing again brings them back.`,
    header: 'Remove Subscription',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => remove(sub),
  });
};

const remove = async (sub: CalendarSubscription) => {
  try {
    await api(`/api/v1/calendar-subscriptions/${sub.id}`, { method: 'DELETE' });
    subscriptions.value = subscriptions.value.filter(s => s.id !== sub.id);
    await refreshCalendars();
    toast.success(`"${sub.name}" has been removed`);
  } catch {
    toast.error('Failed to remove the subscription');
  }
};

onMounted(fetchSubscriptions);
</script>
