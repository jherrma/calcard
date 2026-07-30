<template>
  <Dialog
    :visible="visible"
    :header="header"
    :modal="true"
    :style="{ width: '560px' }"
    :breakpoints="{ '640px': '95vw' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <div class="pt-2 space-y-6">
      <SharingSharePanel
        v-if="resourceUuid"
        :resource-type="resourceType"
        :resource-uuid="resourceUuid"
        :can-manage="canManage"
        @changed="$emit('changed')"
      />

      <!-- Public links exist for calendars only, and only the owner can touch them. -->
      <template v-if="resourceType === 'calendar' && resourceUuid && canManage">
        <Divider />
        <SharingPublicLinkPanel
          :calendar-uuid="resourceUuid"
          :public-enabled="publicEnabled"
          @changed="$emit('changed')"
        />
      </template>
    </div>

    <template #footer>
      <Button label="Done" @click="$emit('update:visible', false)" />
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import type { ShareResourceType } from '~/types/sharing';
import { useSharingStore } from '~/stores/sharing';

const props = withDefaults(defineProps<{
  visible: boolean;
  resourceType: ShareResourceType;
  /** Resource UUID (#52). Empty/undefined while no resource is selected. */
  resourceUuid: string | undefined;
  resourceName?: string;
  /** False for a resource shared WITH the user — the share endpoints 404 for them (#53). */
  canManage?: boolean;
  /** Calendars only: seeds the public switch before the status call lands. */
  publicEnabled?: boolean;
}>(), {
  canManage: true,
});

defineEmits<{
  'update:visible': [value: boolean];
  /** Any successful share or public-link change, so the caller can refresh its list. */
  changed: [];
}>();

const store = useSharingStore();

const header = computed(() => {
  const kind = props.resourceType === 'calendar' ? 'calendar' : 'address book';
  return props.resourceName ? `Share "${props.resourceName}"` : `Share ${kind}`;
});

// Wipe the previous resource's shares and public status on close. The Dialog
// unmounts its content while hidden, so the panels refetch on reopen anyway;
// this just makes sure the next open cannot flash the last resource's shares
// during the enter animation.
watch(() => props.visible, (open) => {
  if (!open) store.reset();
});
</script>
