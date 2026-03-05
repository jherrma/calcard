<template>
  <Dialog
    :visible="visible"
    :header="`${calendar?.name || 'Calendar'} Settings`"
    :modal="true"
    :style="{ width: '600px' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <Tabs :value="activeTab" @update:value="activeTab = $event as string">
      <TabList>
        <Tab value="general">General</Tab>
        <Tab value="sharing">Sharing</Tab>
        <Tab value="public">Public Access</Tab>
        <Tab value="integration">Integration</Tab>
      </TabList>
      <TabPanels>
        <!-- General Tab -->
        <TabPanel value="general">
          <div class="space-y-4 pt-4">
            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Name</label>
              <InputText v-model="form.name" class="w-full" />
            </div>

            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Color</label>
              <div class="flex items-center gap-3">
                <ColorPicker v-model="form.color" />
                <InputText v-model="form.color" class="w-32 font-mono text-sm" />
              </div>
            </div>

            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Timezone</label>
              <Select
                v-model="form.timezone"
                :options="timezones"
                filter
                class="w-full"
              />
            </div>

            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Description</label>
              <Textarea v-model="form.description" class="w-full" rows="3" />
            </div>
          </div>
        </TabPanel>

        <!-- Sharing Tab -->
        <TabPanel value="sharing">
          <div class="pt-4">
            <CalendarSharing v-if="calendar" :calendar-id="calendar.id" />
          </div>
        </TabPanel>

        <!-- Public Access Tab -->
        <TabPanel value="public">
          <div class="pt-4">
            <CalendarPublicAccess
              :calendar="calendar"
              @updated="$emit('updated')"
            />
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
          label="Delete Calendar"
          severity="danger"
          text
          @click="confirmDelete"
        />
        <div class="flex gap-2">
          <Button label="Cancel" severity="secondary" @click="$emit('update:visible', false)" />
          <Button label="Save" :loading="isSaving" @click="save" />
        </div>
      </div>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { Calendar } from '~/types/calendar';
import { useCalendarStore } from '~/stores/calendars';
import { useAuthStore } from '~/stores/auth';

const props = defineProps<{
  visible: boolean;
  calendar: Calendar | null;
  initialTab?: string;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  updated: [];
  deleted: [];
}>();

const toast = useAppToast();
const confirm = useConfirm();
const calendarStore = useCalendarStore();
const authStore = useAuthStore();
const config = useRuntimeConfig();

const isSaving = ref(false);
const activeTab = ref('general');

const form = reactive({
  name: '',
  color: '',
  timezone: '',
  description: '',
});

watch(() => props.calendar, (cal) => {
  if (cal) {
    form.name = cal.name;
    form.color = cal.color?.replace('#', '') || '3788d8';
    form.timezone = cal.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone;
    form.description = cal.description || '';
  }
}, { immediate: true });

watch(() => props.initialTab, (tab) => {
  if (tab) activeTab.value = tab;
});

watch(() => props.visible, (vis) => {
  if (vis && props.initialTab) activeTab.value = props.initialTab;
});

const timezones = computed(() => Intl.supportedValuesOf('timeZone'));

const caldavUrl = computed(() => {
  if (!props.calendar) return '';
  const base = (config.public.apiBaseUrl as string) || '';
  const username = authStore.user?.username || 'me';
  return `${base}/dav/${username}/calendars/${props.calendar.path}/`;
});

const save = async () => {
  if (!props.calendar) return;
  isSaving.value = true;
  try {
    await calendarStore.updateCalendar(props.calendar.uuid, {
      name: form.name,
      color: form.color.startsWith('#') ? form.color : `#${form.color}`,
      timezone: form.timezone,
      description: form.description,
    });
    toast.success('Calendar updated');
    emit('updated');
    emit('update:visible', false);
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to update calendar');
  } finally {
    isSaving.value = false;
  }
};

const confirmDelete = () => {
  confirm.require({
    message: 'Are you sure you want to delete this calendar? All events will be permanently deleted.',
    header: 'Delete Calendar',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: deleteCalendar,
  });
};

const deleteCalendar = async () => {
  if (!props.calendar) return;
  try {
    await calendarStore.deleteCalendar(props.calendar.uuid);
    toast.success('Calendar deleted');
    emit('deleted');
    emit('update:visible', false);
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to delete calendar');
  }
};

const copyUrl = async (url: string) => {
  await navigator.clipboard.writeText(url);
  toast.success('URL copied');
};
</script>
