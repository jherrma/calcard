<template>
  <div class="space-y-6">
    <!-- Add share -->
    <div>
      <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-2">Share with</label>
      <div class="flex gap-2">
        <InputText
          v-model="newShareUser"
          placeholder="Email or username"
          class="flex-1"
        />
        <Select
          v-model="newSharePermission"
          :options="permissionOptions"
          option-label="label"
          option-value="value"
          class="w-36"
        />
        <Button
          label="Share"
          :disabled="!newShareUser.trim()"
          :loading="isSharing"
          @click="addShare"
        />
      </div>
    </div>

    <!-- Current shares -->
    <div>
      <h3 class="text-sm font-medium text-surface-700 dark:text-surface-300 mb-3">Shared with</h3>

      <div v-if="isLoadingShares" class="text-center py-8">
        <ProgressSpinner style="width: 30px; height: 30px" />
      </div>

      <div v-else-if="shares.length === 0" class="text-center py-8 text-surface-500">
        This calendar is not shared with anyone
      </div>

      <div v-else class="space-y-2">
        <div
          v-for="share in shares"
          :key="share.id"
          class="flex items-center justify-between p-3 bg-surface-50 dark:bg-surface-800 rounded-lg"
        >
          <div class="flex items-center gap-3">
            <Avatar
              :label="share.shared_with.display_name?.charAt(0)?.toUpperCase() || '?'"
              shape="circle"
              class="bg-primary-100 text-primary-700"
            />
            <div>
              <div class="font-medium text-surface-900 dark:text-surface-100">{{ share.shared_with.display_name }}</div>
              <div class="text-sm text-surface-500">{{ share.shared_with.email }}</div>
            </div>
          </div>

          <div class="flex items-center gap-2">
            <Select
              :model-value="share.permission"
              :options="permissionOptions"
              option-label="label"
              option-value="value"
              class="w-36"
              @update:model-value="updatePermission(share, $event as string)"
            />
            <Button
              icon="pi pi-trash"
              severity="danger"
              text
              rounded
              @click="confirmRemove(share)"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { CalendarShare } from '~/types/sharing';

const props = defineProps<{
  calendarId: string;
}>();

const toast = useAppToast();
const confirm = useConfirm();
const api = useApi();

const shares = ref<CalendarShare[]>([]);
const isLoadingShares = ref(false);
const isSharing = ref(false);
const newShareUser = ref('');
const newSharePermission = ref('read');

const permissionOptions = [
  { label: 'Can view', value: 'read' },
  { label: 'Can edit', value: 'read-write' },
];

onMounted(async () => {
  await fetchShares();
});

watch(() => props.calendarId, () => {
  fetchShares();
});

const fetchShares = async () => {
  isLoadingShares.value = true;
  try {
    const response = await api<{ shares: CalendarShare[] }>(
      `/api/v1/calendars/${props.calendarId}/shares`
    );
    shares.value = response.shares || [];
  } catch {
    shares.value = [];
  } finally {
    isLoadingShares.value = false;
  }
};

const addShare = async () => {
  if (!newShareUser.value.trim()) return;

  isSharing.value = true;
  try {
    await api(`/api/v1/calendars/${props.calendarId}/shares`, {
      method: 'POST',
      body: {
        user_identifier: newShareUser.value.trim(),
        permission: newSharePermission.value,
      },
    });
    toast.success(`Shared with ${newShareUser.value}`);
    newShareUser.value = '';
    await fetchShares();
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to share calendar');
  } finally {
    isSharing.value = false;
  }
};

const updatePermission = async (share: CalendarShare, permission: string) => {
  try {
    await api(`/api/v1/calendars/${props.calendarId}/shares/${share.id}`, {
      method: 'PATCH',
      body: { permission },
    });
    share.permission = permission;
    toast.success('Permission updated');
  } catch {
    toast.error('Failed to update permission');
  }
};

const confirmRemove = (share: CalendarShare) => {
  confirm.require({
    message: `Remove ${share.shared_with.display_name}'s access to this calendar?`,
    header: 'Remove Access',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => removeShare(share),
  });
};

const removeShare = async (share: CalendarShare) => {
  try {
    await api(`/api/v1/calendars/${props.calendarId}/shares/${share.id}`, {
      method: 'DELETE',
    });
    toast.success('Access removed');
    await fetchShares();
  } catch {
    toast.error('Failed to remove access');
  }
};
</script>
