<template>
  <div
    class="flex items-center gap-3 px-4 py-3 hover:bg-surface-100 dark:hover:bg-surface-800 cursor-pointer group transition-colors"
    @click="$emit('click', contact)"
  >
    <!-- Avatar -->
    <div
      class="w-10 h-10 rounded-full flex items-center justify-center text-white font-semibold text-sm flex-shrink-0"
      :style="{ backgroundColor: avatarColor }"
    >
      <img
        v-if="contact.photo_url"
        :src="contact.photo_url"
        :alt="contact.formatted_name"
        class="w-10 h-10 rounded-full object-cover"
      >
      <span v-else>{{ initials }}</span>
    </div>

    <!-- Info -->
    <div class="flex-1 min-w-0">
      <div class="text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
        <HighlightText :text="contact.formatted_name" :highlight="searchQuery" />
      </div>
      <div class="flex items-center gap-3 text-xs text-surface-500 dark:text-surface-400 truncate">
        <span v-if="primaryEmail" class="truncate">
          <HighlightText :text="primaryEmail" :highlight="searchQuery" />
        </span>
        <span v-if="primaryPhone" class="truncate">{{ primaryPhone }}</span>
      </div>
      <div v-if="contact.organization" class="text-xs text-surface-400 dark:text-surface-500 truncate">
        <HighlightText :text="contact.organization" :highlight="searchQuery" />
      </div>
    </div>

    <!-- Hover actions -->
    <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
      <button
        class="p-1.5 rounded hover:bg-surface-200 dark:hover:bg-surface-700 text-surface-500 hover:text-surface-700 dark:hover:text-surface-300"
        title="Edit"
        @click.stop="$emit('edit', contact)"
      >
        <i class="pi pi-pencil text-sm" />
      </button>
      <button
        class="p-1.5 rounded hover:bg-red-100 dark:hover:bg-red-900/30 text-surface-500 hover:text-red-600"
        title="Delete"
        @click.stop="$emit('delete', contact)"
      >
        <i class="pi pi-trash text-sm" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Contact } from '~/types/contacts';
import HighlightText from '~/components/common/HighlightText.vue';

const props = defineProps<{
  contact: Contact;
  searchQuery?: string;
}>();

defineEmits<{
  click: [contact: Contact];
  edit: [contact: Contact];
  delete: [contact: Contact];
}>();

const primaryEmail = computed(() =>
  props.contact.emails?.find(e => e.primary)?.value || props.contact.emails?.[0]?.value || ''
);

const primaryPhone = computed(() =>
  props.contact.phones?.find(p => p.primary)?.value || props.contact.phones?.[0]?.value || ''
);

const initials = computed(() => {
  const name = props.contact.formatted_name || '';
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]!.charAt(0) + parts[parts.length - 1]!.charAt(0)).toUpperCase();
  return (parts[0]?.[0] || '?').toUpperCase();
});

const avatarColor = computed(() => {
  // Generate a consistent color from the contact name
  const name = props.contact.formatted_name || props.contact.id;
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = name.charCodeAt(i) + ((hash << 5) - hash);
  }
  const colors = [
    '#3b82f6', '#ef4444', '#10b981', '#f59e0b', '#8b5cf6',
    '#ec4899', '#06b6d4', '#f97316', '#6366f1', '#14b8a6',
  ];
  return colors[Math.abs(hash) % colors.length];
});
</script>
