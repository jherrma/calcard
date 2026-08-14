<template>
  <div class="space-y-4">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h3 class="font-medium text-surface-900 dark:text-surface-100">Public link</h3>
        <p class="text-sm text-surface-500 dark:text-surface-400">
          Anyone holding the link can subscribe to this calendar and read every event on it — no
          account, no sign-in. Events stay read-only.
        </p>
      </div>
      <InputSwitch
        :model-value="enabled"
        :disabled="store.isLoadingPublic || store.isSaving"
        @update:model-value="togglePublic($event as boolean)"
      />
    </div>

    <div v-if="store.isLoadingPublic" class="flex justify-center py-6">
      <ProgressSpinner style="width: 30px; height: 30px" />
    </div>

    <!-- GET …/public is the only source of the public URL, so a failed status
         call means we know nothing — say so instead of rendering a link-less
         "public" panel (or, worse, an implied "not public"). -->
    <Message v-else-if="store.publicError" severity="error" :closable="false">
      <div class="flex flex-wrap items-center gap-2">
        <span>{{ store.publicError }}</span>
        <Button
          label="Retry"
          icon="pi pi-refresh"
          severity="secondary"
          size="small"
          :disabled="store.isSaving"
          @click="store.fetchPublicAccess(calendarUuid)"
        />
      </div>
    </Message>

    <div v-else-if="enabled" class="space-y-4">
      <Message severity="warn" :closable="false">
        This calendar is public. Treat the link like a password: everyone it reaches sees your event
        titles, times, locations and descriptions.
      </Message>

      <div v-if="publicUrl">
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">
          Subscription URL (iCal)
        </label>
        <div class="flex gap-2">
          <InputText :model-value="publicUrl" readonly class="flex-1 font-mono text-sm" />
          <Button icon="pi pi-copy" severity="secondary" title="Copy link" @click="copyUrl" />
        </div>
        <p class="text-xs text-surface-500 dark:text-surface-400 mt-1">
          Paste this into Google Calendar, Outlook, Apple Calendar or any app that subscribes to an
          iCal feed.
        </p>
      </div>

      <div>
        <Button
          label="Regenerate link"
          icon="pi pi-refresh"
          severity="secondary"
          size="small"
          :disabled="store.isSaving"
          @click="confirmRegenerate"
        />
        <p class="text-xs text-surface-500 dark:text-surface-400 mt-1">
          Generates a new URL and invalidates the current one. Everyone already subscribed stops
          receiving updates until you send them the new link.
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import { useSharingStore } from '~/stores/sharing';

const props = defineProps<{
  /** Calendar UUID (#52). Address books have no public mode, so this panel is calendar-only. */
  calendarUuid: string;
  /** Seed for the switch from the calendar list payload, which carries public_enabled but never the URL. */
  publicEnabled?: boolean;
}>();

const emit = defineEmits<{
  /** Raised after enable/disable/regenerate so the parent can refresh its calendar list. */
  changed: [];
}>();

const store = useSharingStore();
const toast = useAppToast();
const confirm = useConfirm();

// The list payload's public_enabled is the only thing known before the status
// call lands (the token is json:"-" on the domain model, so the URL exists
// nowhere else). Prefer the fetched status once we have it.
const enabled = computed(() => store.publicAccess?.enabled ?? !!props.publicEnabled);
const publicUrl = computed(() => store.publicAccess?.public_url || null);

watch(
  () => props.calendarUuid,
  (uuid) => {
    if (uuid) store.fetchPublicAccess(uuid);
  },
  { immediate: true },
);

const togglePublic = async (next: boolean) => {
  try {
    await store.setPublicAccess(props.calendarUuid, next);
    toast.success(
      next
        ? 'Public link created — anyone with the URL can now view this calendar'
        : 'Public link removed — the old URL no longer works',
      next ? 'Calendar is public' : 'Calendar is private',
    );
    emit('changed');
  } catch (e: unknown) {
    toast.error((e as Error).message);
  }
};

// navigator.clipboard is UNDEFINED in a non-secure context, and this project's
// docker-compose default is plain http:// on a LAN address — so the happy path
// is not guaranteed here. Without the guard the click rejects unhandled: no
// toast at all, and the user pastes whatever was in the clipboard before.
const COPY_FAILED = 'Could not copy automatically — select the URL and copy it manually';

const copyUrl = async () => {
  if (!publicUrl.value) return;
  // Over plain http the API is absent, not failing, so check before calling.
  if (!navigator.clipboard) {
    toast.error(COPY_FAILED);
    return;
  }
  try {
    await navigator.clipboard.writeText(publicUrl.value);
    toast.success('Link copied to clipboard');
  } catch {
    toast.error(COPY_FAILED);
  }
};

const confirmRegenerate = () => {
  confirm.require({
    message:
      'The current link stops working immediately and every existing subscriber loses access until you share the new URL. Continue?',
    header: 'Regenerate public link',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    acceptLabel: 'Regenerate',
    accept: async () => {
      try {
        await store.regeneratePublicToken(props.calendarUuid);
        toast.success('New link generated — the previous URL is no longer valid');
        emit('changed');
      } catch (e: unknown) {
        toast.error((e as Error).message);
      }
    },
  });
};
</script>
