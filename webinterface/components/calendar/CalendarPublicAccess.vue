<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h3 class="font-medium text-surface-900 dark:text-surface-100">Public Access</h3>
        <p class="text-sm text-surface-500">
          Allow anyone with the link to view this calendar (read-only)
        </p>
      </div>
      <InputSwitch v-model="isPublic" @update:model-value="togglePublic" />
    </div>

    <div v-if="isPublic && publicUrl" class="space-y-4">
      <Message severity="info" :closable="false">
        Anyone with this URL can view your calendar events. They cannot make changes.
      </Message>

      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Public iCal URL</label>
        <div class="flex gap-2">
          <InputText :model-value="publicUrl" readonly class="flex-1 font-mono text-sm" />
          <Button icon="pi pi-copy" severity="secondary" @click="copyUrl" />
        </div>
        <p class="text-xs text-surface-500 mt-1">
          Use this URL in Google Calendar, Outlook, or any app that supports iCal subscriptions
        </p>
      </div>

      <div>
        <Button
          label="Regenerate URL"
          severity="secondary"
          size="small"
          @click="confirmRegenerate"
        />
        <p class="text-xs text-surface-500 mt-1">
          This will invalidate the current URL. Anyone using the old URL will lose access.
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { Calendar } from '~/types/calendar';
import type { PublicAccessStatus } from '~/types/sharing';

const props = defineProps<{
  calendar: Calendar | null;
}>();

const emit = defineEmits<{
  updated: [];
}>();

const toast = useAppToast();
const confirm = useConfirm();
const api = useApi();

const isPublic = ref(false);
const publicUrl = ref<string | null>(null);

watch(() => props.calendar, (cal) => {
  if (cal) {
    isPublic.value = !!cal.public_enabled;
    publicUrl.value = cal.public_url || null;
  }
}, { immediate: true });

const togglePublic = async () => {
  try {
    const response = await api<PublicAccessStatus>(
      `/api/v1/calendars/${props.calendar?.id}/public`,
      {
        method: 'POST',
        body: { enabled: isPublic.value },
      }
    );
    publicUrl.value = response.public_url || null;
    toast.success(isPublic.value ? 'Public access enabled' : 'Public access disabled');
    emit('updated');
  } catch (e: unknown) {
    isPublic.value = !isPublic.value;
    toast.error((e as Error).message || 'Failed to update public access');
  }
};

const copyUrl = async () => {
  if (publicUrl.value) {
    await navigator.clipboard.writeText(publicUrl.value);
    toast.success('URL copied');
  }
};

const confirmRegenerate = () => {
  confirm.require({
    message: 'This will create a new URL and invalidate the old one. Continue?',
    header: 'Regenerate URL',
    icon: 'pi pi-exclamation-triangle',
    accept: regenerateUrl,
  });
};

const regenerateUrl = async () => {
  try {
    const response = await api<PublicAccessStatus>(
      `/api/v1/calendars/${props.calendar?.id}/public/regenerate`,
      { method: 'POST' }
    );
    publicUrl.value = response.public_url || null;
    toast.success('New URL generated');
    emit('updated');
  } catch {
    toast.error('Failed to regenerate URL');
  }
};
</script>
