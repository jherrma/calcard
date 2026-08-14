<template>
  <DashboardWidgetCard title="Recent Contacts" icon="pi pi-users">
    <template #actions>
      <button
        type="button"
        class="text-xs font-medium text-primary-600 dark:text-primary-400 hover:underline"
        @click="$emit('view-all')"
      >
        View all
      </button>
    </template>

    <CommonSkeletonList v-if="showSkeleton" :count="4" />

    <div v-else-if="contacts.length === 0" class="flex flex-col items-center gap-2 py-8 text-center">
      <i class="pi pi-user-plus text-3xl text-surface-300 dark:text-surface-600" />
      <p class="text-sm text-surface-600 dark:text-surface-400">No contacts yet</p>
      <p class="text-xs text-surface-500 dark:text-surface-400">
        Add your first contact to see it here.
      </p>
      <Button
        v-if="canCreate"
        label="Add Contact"
        icon="pi pi-plus"
        size="small"
        class="mt-1"
        @click="$emit('add')"
      />
    </div>

    <div v-else class="grid gap-0.5 sm:grid-cols-2">
      <DashboardRecentContactRow
        v-for="contact in contacts"
        :key="contact.id"
        :contact="contact"
        @select="$emit('select', $event)"
      />
    </div>

    <template #footer>
      <!-- Honest about what "recent" means here: the API can sort by update time,
           but nothing records what the user merely VIEWED (story 042). -->
      <span>Most recently updated contacts</span>
    </template>
  </DashboardWidgetCard>
</template>

<script setup lang="ts">
import type { Contact } from '~/types/contacts';

const props = defineProps<{
  contacts: Contact[];
  /** False when every address book is a read-only share — creating would 403. */
  canCreate?: boolean;
  loading?: boolean;
}>();

defineEmits<{
  select: [contact: Contact];
  'view-all': [];
  add: [];
}>();

const showSkeleton = computed(() => props.loading && props.contacts.length === 0);
</script>
