<template>
  <Dialog
    :visible="visible"
    :header="copy.header"
    :modal="true"
    :style="{ width: '600px' }"
    :breakpoints="isTaskList ? TASK_LIST_BREAKPOINTS : undefined"
    @update:visible="$emit('update:visible', $event)"
  >
    <Tabs :value="activeTab" @update:value="activeTab = $event as string">
      <TabList>
        <Tab value="general">General</Tab>
        <Tab value="sharing">Sharing</Tab>
        <!-- Public task-list pages are deferred (#170 spec §10): no tab for a task list. -->
        <Tab v-if="!isTaskList" value="public">Public Access</Tab>
        <Tab value="integration">Integration</Tab>
      </TabList>
      <TabPanels>
        <!-- General Tab -->
        <TabPanel value="general">
          <div class="space-y-4 pt-4">
            <div>
              <label :for="nameId" class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Name</label>
              <InputText :id="nameId" v-model="form.name" class="w-full" />
            </div>

            <div>
              <label :for="colorId" class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Color</label>
              <div class="flex items-center gap-3">
                <ColorPicker v-model="form.color" />
                <InputText
                  :id="colorId"
                  v-model="form.color"
                  class="w-32 font-mono text-sm"
                  :invalid="colorInvalid"
                  :aria-describedby="colorInvalid ? colorErrorId : undefined"
                />
              </div>
              <p
                v-if="colorInvalid"
                :id="colorErrorId"
                class="mt-1 text-sm text-red-600 dark:text-red-400"
                data-test="color-error"
              >
                {{ COLOR_ERROR }}
              </p>
            </div>

            <!-- A task list has no timezone to choose: its tasks carry their own (#170 spec §7.3). -->
            <div v-if="!isTaskList">
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Timezone</label>
              <Select
                v-model="form.timezone"
                :options="timezones"
                filter
                class="w-full"
              />
            </div>

            <div>
              <label :for="descriptionId" class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Description</label>
              <Textarea :id="descriptionId" v-model="form.description" class="w-full" rows="3" />
            </div>
          </div>
        </TabPanel>

        <!-- Sharing Tab — same panel the standalone share dialog uses (story 043) -->
        <TabPanel value="sharing">
          <div class="pt-4">
            <SharingSharePanel
              v-if="calendar"
              resource-type="calendar"
              :resource-uuid="calendar.uuid"
              :resource-noun="isTaskList ? 'task list' : undefined"
              :can-manage="!calendar.shared"
              @changed="$emit('updated')"
            />
          </div>
        </TabPanel>

        <!-- Public Access Tab -->
        <TabPanel v-if="!isTaskList" value="public">
          <div class="pt-4">
            <SharingPublicLinkPanel
              v-if="calendar && !calendar.shared"
              :calendar-uuid="calendar.uuid"
              :public-enabled="calendar.public_enabled"
              @changed="$emit('updated')"
            />
            <Message v-else-if="calendar" severity="info" :closable="false">
              Only the owner of this calendar can publish it.
            </Message>
          </div>
        </TabPanel>

        <!-- Integration Tab -->
        <TabPanel value="integration">
          <div class="space-y-4 pt-4">
            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">CalDAV URL</label>
              <div class="flex gap-2">
                <InputText :model-value="caldavUrl" readonly class="flex-1 font-mono text-sm" />
                <Button icon="pi pi-copy" severity="secondary" @click="copyUrl(caldavUrl)" />
              </div>
            </div>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>

    <template #footer>
      <div class="flex justify-between w-full">
        <Button
          :label="copy.deleteLabel"
          severity="danger"
          text
          @click="confirmDelete()"
        />
        <div class="flex gap-2">
          <Button label="Cancel" severity="secondary" @click="$emit('update:visible', false)" />
          <Button label="Save" :loading="isSaving" :disabled="!canSave" @click="save" />
        </div>
      </div>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { Calendar, CalendarKind } from '~/types/calendar';
import { useCalendarStore } from '~/stores/calendars';
import { useAuthStore } from '~/stores/auth';
import { useTasksStore } from '~/stores/tasks';

/**
 * Settings of one calendar: General (name, colour, timezone, description),
 * Sharing, Public Access, Integration (the CalDAV URL), and Delete.
 *
 * `kind` says what the collection is (#170). Only `'tasks'` changes anything:
 * the Tasks section opens a task list through `TaskListSettingsDialog` with
 * it, and then the copy talks about a task list ("Delete task list" and a
 * permanent-deletion warning), Timezone and the Public Access tab are hidden,
 * a save sends only the changed fields (none = no request), and a name or
 * colour the server would refuse disables Save. Without it (the Calendar
 * section) the dialog behaves exactly as it always did. The kind itself is
 * never shown or changed here: widening is API-only in v1 (spec §1b).
 */
const props = defineProps<{
  visible: boolean;
  calendar: Calendar | null;
  initialTab?: string;
  kind?: CalendarKind;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  updated: [];
  deleted: [calendar: Calendar];
}>();

const toast = useAppToast();
const confirm = useConfirm();
const calendarStore = useCalendarStore();
const authStore = useAuthStore();
const config = useRuntimeConfig();

const TASK_LIST_BREAKPOINTS = { '640px': 'calc(100vw - 2rem)' };
const COLOR_ERROR = 'Enter the colour as a hex value, for example 3788d8.';
const RETRY_HINT = 'Check your connection and try again.';

const nameId = useId();
const colorId = useId();
const colorErrorId = useId();
const descriptionId = useId();

const isTaskList = computed(() => props.kind === 'tasks');

const isSaving = ref(false);
const activeTab = ref('general');

const form = reactive({
  name: '',
  color: '',
  timezone: '',
  description: '',
});

function fillForm(cal: Calendar) {
  form.name = cal.name;
  form.color = cal.color?.replace('#', '') || '3788d8';
  form.timezone = cal.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone;
  form.description = cal.description || '';
}

/** A tab this kind does not have (a task list's "public") falls back to General. */
function tabFor(tab: string): string {
  return isTaskList.value && tab === 'public' ? 'general' : tab;
}

watch(() => props.calendar, (cal) => {
  if (cal) fillForm(cal);
}, { immediate: true });

watch(() => props.initialTab, (tab) => {
  if (tab) activeTab.value = tabFor(tab);
});

watch(() => props.visible, (vis) => {
  if (vis && props.initialTab) activeTab.value = tabFor(props.initialTab);
  // A task list reopened after a cancelled edit shows what is stored, not the
  // abandoned edit (the form is otherwise only refilled for another list).
  if (vis && isTaskList.value && props.calendar) fillForm(props.calendar);
});

/** The copy for a calendar (unchanged) or a task list. */
const copy = computed(() => {
  if (isTaskList.value) {
    return {
      header: `${props.calendar?.name || 'Task list'} settings`,
      deleteLabel: 'Delete task list',
      updated: 'Task list updated',
      deleted: 'Task list deleted',
      updateFailed: "Couldn't save the task list",
      deleteFailed: "Couldn't delete the task list",
    };
  }
  return {
    header: `${props.calendar?.name || 'Calendar'} Settings`,
    deleteLabel: 'Delete Calendar',
    updated: 'Calendar updated',
    deleted: 'Calendar deleted',
    updateFailed: 'Failed to update calendar',
    deleteFailed: 'Failed to delete calendar',
  };
});

// Only a task list validates in the browser; a calendar sends what it always sent.
const colorInvalid = computed(() => isTaskList.value && normalizeAccentColor(form.color) === null);
const canSave = computed(() => !isTaskList.value || (form.name.trim() !== '' && !colorInvalid.value));

const timezones = computed(() => Intl.supportedValuesOf('timeZone'));

const caldavUrl = computed(() => {
  if (!props.calendar) return '';
  // Same-origin builds have an empty apiBaseUrl; fall back to the page origin so
  // the DAV URL shown to the user is absolute and copy-pasteable (#99). Client-
  // only page (ssr: false), so window is always available.
  const base = (config.public.apiBaseUrl as string) || window.location.origin;
  const username = authStore.user?.username || 'me';
  return `${base}/dav/${username}/calendars/${props.calendar.path}/`;
});

/**
 * What to tell the user when a task-list write failed: the server's sentence
 * for a 4xx, else what to do. Null for a 429: useApi already said so once.
 */
function taskListErrorMessage(e: unknown): string | null {
  if (isRateLimitError(e)) return null;
  const err = e as { status?: number; statusCode?: number; data?: { message?: unknown; error?: unknown } | null } | null;
  const status = err?.statusCode ?? err?.status;
  const message = err?.data?.message ?? err?.data?.error;
  if (status !== undefined && status >= 400 && status < 500 && typeof message === 'string' && message.trim()) {
    return message;
  }
  return RETRY_HINT;
}

function showError(e: unknown, title: string) {
  if (!isTaskList.value) {
    toast.error((e as Error).message || title);
    return;
  }
  const message = taskListErrorMessage(e);
  if (message) toast.error(message, title);
}

/** A task list's changed fields only: name (trimmed), colour (normalised), description (as typed). */
function taskListPatch(cal: Calendar): { name?: string; color?: string; description?: string } {
  const patch: { name?: string; color?: string; description?: string } = {};
  const name = form.name.trim();
  if (name !== cal.name.trim()) patch.name = name;
  const color = normalizeAccentColor(form.color);
  if (color !== null && color !== (cal.color ?? '').toLowerCase()) patch.color = color;
  if (form.description !== (cal.description ?? '')) patch.description = form.description;
  return patch;
}

const save = async () => {
  if (!props.calendar || !canSave.value) return;
  let body: { name?: string; color?: string; timezone?: string; description?: string };
  if (isTaskList.value) {
    body = taskListPatch(props.calendar);
    // Nothing changed: nothing to send, and nothing to announce.
    if (Object.keys(body).length === 0) {
      emit('update:visible', false);
      return;
    }
  } else {
    body = {
      name: form.name,
      color: form.color.startsWith('#') ? form.color : `#${form.color}`,
      timezone: form.timezone,
      description: form.description,
    };
  }
  isSaving.value = true;
  try {
    await calendarStore.updateCalendar(props.calendar.uuid, body);
    toast.success(copy.value.updated);
    emit('updated');
    emit('update:visible', false);
  } catch (e: unknown) {
    showError(e, copy.value.updateFailed);
  } finally {
    isSaving.value = false;
  }
};

/**
 * Asks before deleting. `target` defaults to the dialog's calendar; the Tasks
 * sidebar's "Delete task list" passes its list through `TaskListSettingsDialog`
 * so the question and the delete exist only here.
 */
const confirmDelete = (target: Calendar | null = props.calendar) => {
  if (!target) return;
  if (isTaskList.value) {
    confirm.require({
      message: taskListDeleteMessage(target.name),
      header: 'Delete task list',
      icon: 'pi pi-exclamation-triangle',
      acceptClass: 'p-button-danger',
      acceptLabel: 'Delete task list',
      rejectLabel: 'Cancel',
      accept: () => deleteCalendar(target),
    });
    return;
  }
  confirm.require({
    message: `Are you sure you want to delete this calendar? All ${
      target.kind === 'both' ? 'events and tasks' : 'events'
    } will be permanently deleted.`,
    header: 'Delete Calendar',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => deleteCalendar(target),
  });
};

const deleteCalendar = async (target: Calendar) => {
  try {
    await calendarStore.deleteCalendar(target.uuid);
    // A mixed calendar takes its tasks with it: the overdue badge and counts follow.
    if (target.kind === 'both') void useTasksStore().fetchSummary();
    toast.success(copy.value.deleted);
    emit('deleted', target);
    emit('update:visible', false);
  } catch (e: unknown) {
    showError(e, copy.value.deleteFailed);
  }
};

const copyUrl = async (url: string) => {
  await navigator.clipboard.writeText(url);
  toast.success('URL copied');
};

defineExpose({ confirmDelete });
</script>
