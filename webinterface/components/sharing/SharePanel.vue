<template>
  <div class="space-y-6">
    <!-- A sharee must never see share management: the endpoints 404 for them
         (#53), so rendering the controls would only produce dead buttons. -->
    <Message v-if="!canManage" severity="info" :closable="false">
      Only the owner of this {{ noun }} can change who it is shared with.
    </Message>

    <template v-else>
      <!-- Invite -->
      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-2">
          Invite someone
        </label>
        <div class="flex flex-col sm:flex-row gap-2">
          <InputText
            v-model="identifier"
            placeholder="Email address or username"
            class="flex-1"
            :disabled="store.isSaving"
            @keyup.enter="invite"
          />
          <Select
            v-model="invitePermission"
            :options="permissionOptions"
            option-label="label"
            option-value="value"
            class="w-full sm:w-36"
            :disabled="store.isSaving"
          />
          <Button
            label="Share"
            icon="pi pi-user-plus"
            :disabled="!identifier.trim() || store.isSaving"
            :loading="store.isSaving"
            @click="invite"
          />
        </div>
        <!-- No user directory is exposed by the API, so there is nothing to
             autocomplete against — say what the field accepts instead. -->
        <p class="text-xs text-surface-500 mt-1">
          Enter the exact email address or username of an existing account.
        </p>
      </div>

      <Divider />

      <!-- Current shares -->
      <div>
        <div class="flex items-center justify-between mb-3">
          <h3 class="text-sm font-medium text-surface-700 dark:text-surface-300">
            Shared with
            <span v-if="store.shares.length" class="text-surface-400">({{ store.shares.length }})</span>
          </h3>
          <Button
            v-if="store.shares.length > 1"
            label="Remove all"
            severity="danger"
            text
            size="small"
            :disabled="store.isSaving"
            @click="confirmRemoveAll"
          />
        </div>

        <div v-if="store.isLoadingShares" class="flex justify-center py-8">
          <ProgressSpinner style="width: 30px; height: 30px" />
        </div>

        <template v-else>
          <!-- A failed load leaves `shares` empty, which is indistinguishable
               from "nothing is shared" — so the error must be shown and the
               "private" claim below must be suppressed. Telling an owner their
               calendar is private when the request merely failed invites them to
               conclude their shares were lost. Also covers a partial bulk
               revoke, where the rows that remain ARE the failures. -->
          <Message v-if="store.sharesError" severity="error" :closable="false">
            <div class="flex flex-wrap items-center gap-2">
              <span>{{ store.sharesError }}</span>
              <Button
                label="Retry"
                icon="pi pi-refresh"
                severity="secondary"
                size="small"
                :disabled="store.isSaving"
                @click="reload"
              />
            </div>
          </Message>

          <div
            v-if="!store.sharesError && store.shares.length === 0"
            class="text-center py-8 text-surface-500 text-sm"
          >
            This {{ noun }} is private — nobody else can see it.
          </div>

          <div v-if="store.shares.length" class="space-y-2">
            <div
              v-for="share in store.shares"
              :key="share.id"
              class="flex flex-wrap items-center gap-3 p-3 bg-surface-50 dark:bg-surface-800 rounded-lg"
            >
              <Avatar
                :label="initials(share)"
                shape="circle"
                class="bg-primary-100 text-primary-700"
              />
              <div class="flex-1 min-w-0">
                <div class="font-medium text-surface-900 dark:text-surface-100 truncate">
                  {{ share.shared_with.display_name || share.shared_with.username }}
                </div>
                <div class="text-sm text-surface-500 truncate">{{ share.shared_with.email }}</div>
              </div>
              <Select
                :model-value="share.permission"
                :options="permissionOptions"
                option-label="label"
                option-value="value"
                class="w-36"
                :disabled="store.isSaving"
                @update:model-value="requestPermissionChange(share, $event as SharePermission)"
              />
              <Button
                icon="pi pi-trash"
                severity="danger"
                text
                rounded
                title="Remove access"
                :disabled="store.isSaving"
                @click="confirmRemove(share)"
              />
            </div>
          </div>
        </template>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { Share, SharePermission, ShareResourceType } from '~/types/sharing';
import { PERMISSION_OPTIONS, permissionLabel, useSharingStore } from '~/stores/sharing';

const props = withDefaults(defineProps<{
  resourceType: ShareResourceType;
  resourceUuid: string;
  /** Owner-only: pass false for a resource that was shared WITH the user (#53). */
  canManage?: boolean;
}>(), {
  canManage: true,
});

const emit = defineEmits<{
  /** Raised after any successful mutation so a parent can refresh its list. */
  changed: [];
}>();

const store = useSharingStore();
const toast = useAppToast();
const confirm = useConfirm();

const identifier = ref('');
const invitePermission = ref<SharePermission>('read');
const permissionOptions = PERMISSION_OPTIONS;

const noun = computed(() => (props.resourceType === 'calendar' ? 'calendar' : 'address book'));

// Refetch on mount AND whenever the panel is pointed at another resource — the
// store holds exactly one resource's shares, so a stale list would be wrong,
// not just outdated.
watch(
  () => [props.resourceType, props.resourceUuid, props.canManage] as const,
  ([type, uuid, canManage]) => {
    store.resetShares();
    if (uuid && canManage) store.fetchShares(type, uuid);
  },
  { immediate: true },
);

/** Retry a failed load. Also the recovery path after a partial bulk revoke. */
const reload = () => {
  if (props.resourceUuid && props.canManage) {
    store.fetchShares(props.resourceType, props.resourceUuid);
  }
};

const initials = (share: Share) => {
  const source = share.shared_with.display_name || share.shared_with.username || share.shared_with.email;
  return source ? source.charAt(0).toUpperCase() : '?';
};

const invite = async () => {
  try {
    const share = await store.createShare(
      props.resourceType,
      props.resourceUuid,
      identifier.value,
      invitePermission.value,
    );
    identifier.value = '';
    toast.success(
      `${share.shared_with.display_name || share.shared_with.email} can now ${
        share.permission === 'read-write' ? 'edit' : 'view'} this ${noun.value}`,
      'Shared',
    );
    emit('changed');
  } catch (e: unknown) {
    toast.error((e as Error).message);
  }
};

// Narrowing an existing grant silently takes edit rights away from someone who
// may be mid-edit, so it asks first. Widening is harmless and applies directly.
const requestPermissionChange = (share: Share, permission: SharePermission) => {
  if (permission === share.permission) return;

  const name = share.shared_with.display_name || share.shared_with.email;
  if (share.permission === 'read-write' && permission === 'read') {
    confirm.require({
      message: `${name} will no longer be able to add, edit or delete anything in this ${noun.value}. Continue?`,
      header: 'Reduce access',
      icon: 'pi pi-exclamation-triangle',
      acceptLabel: 'Set to view only',
      accept: () => applyPermissionChange(share, permission),
    });
    return;
  }
  applyPermissionChange(share, permission);
};

const applyPermissionChange = async (share: Share, permission: SharePermission) => {
  try {
    await store.updateShare(props.resourceType, props.resourceUuid, share.id, permission);
    toast.success(`Permission changed to "${permissionLabel(permission)}"`, 'Updated');
    emit('changed');
  } catch (e: unknown) {
    toast.error((e as Error).message);
  }
};

const confirmRemove = (share: Share) => {
  const name = share.shared_with.display_name || share.shared_with.email;
  const last = store.shares.length === 1;
  confirm.require({
    message: last
      ? `Remove ${name}'s access? This was the only share, so the ${noun.value} becomes private again.`
      : `Remove ${name}'s access to this ${noun.value}?`,
    header: 'Remove access',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    acceptLabel: 'Remove',
    accept: async () => {
      try {
        await store.revokeShare(props.resourceType, props.resourceUuid, share.id);
        toast.success(`${name} no longer has access`, 'Removed');
        emit('changed');
      } catch (e: unknown) {
        toast.error((e as Error).message);
      }
    },
  });
};

const confirmRemoveAll = () => {
  const count = store.shares.length;
  confirm.require({
    message: `Remove all ${count} people from this ${noun.value}? It becomes private again and nobody else will be able to see it.`,
    header: 'Remove all shares',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    acceptLabel: 'Remove all',
    accept: async () => {
      const { revoked, failed, reason } = await store.revokeAllShares(
        props.resourceType,
        props.resourceUuid,
      );
      if (revoked) toast.success(`Removed ${revoked} of ${count} shares`, 'Removed');
      // Name the server's reason: "1 share could not be removed" alone leaves
      // the user with a row still in the list and no explanation for it.
      if (failed) {
        toast.error(
          `${failed} share${failed === 1 ? '' : 's'} could not be removed${reason ? `: ${reason}` : ''}`,
        );
      }
      emit('changed');
    },
  });
};
</script>
