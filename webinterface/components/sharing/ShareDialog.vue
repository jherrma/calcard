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
      <!-- Explicit status line (story 043 AC "shows current share status"). The
           two states are INDEPENDENT — a calendar can be shared with named
           people and publicly linked at the same time — so this renders one tag
           per fact rather than a single tri-state. Suppressed while the list is
           unknown (loading, failed, or not ours to read): claiming "Private" on
           no evidence is exactly the mistake the panels avoid. -->
      <div
        v-if="canManage && !store.isLoadingShares && !store.sharesError"
        class="flex flex-wrap items-center gap-2"
      >
        <span class="text-sm text-surface-500">Status</span>
        <Tag v-if="store.shares.length" severity="info" :value="`Shared with ${store.shares.length}`" />
        <Tag v-if="isPublic" severity="warn" value="Public link" />
        <Tag v-if="!store.shares.length && !isPublic" severity="secondary" value="Private" />
      </div>

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

// Address books have no public mode; for calendars the fetched status wins and
// the list payload's public_enabled covers the window before it lands.
const isPublic = computed(() =>
  props.resourceType === 'calendar' && (store.publicAccess?.enabled ?? !!props.publicEnabled));

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
