<template>
  <div
    class="flex items-center gap-3 px-2 py-2 rounded-xl hover:bg-surface-100 dark:hover:bg-surface-800 transition-colors group cursor-pointer"
    @click="$emit('select', contact)"
  >
    <!-- Avatar -->
    <div
      class="w-9 h-9 rounded-full flex items-center justify-center text-white font-semibold text-xs flex-shrink-0"
      :style="{ backgroundColor: avatarColor }"
    >
      <img
        v-if="photoSrc"
        :src="photoSrc"
        :alt="contact.formatted_name"
        class="w-9 h-9 rounded-full object-cover"
      >
      <span v-else>{{ initials }}</span>
    </div>

    <div class="flex-1 min-w-0">
      <p class="text-sm font-medium text-surface-900 dark:text-surface-100 truncate">
        {{ contact.formatted_name || 'Unnamed Contact' }}
      </p>
      <p class="text-xs text-surface-500 dark:text-surface-400 truncate">
        {{ subtitle }}
      </p>
    </div>

    <!-- Quick actions. Anchors (not buttons) so the browser's own mail/phone
         handling applies; @click.stop keeps them from also opening the contact. -->
    <div class="flex items-center gap-1 flex-shrink-0">
      <a
        v-if="primaryEmail"
        :href="'mailto:' + primaryEmail"
        class="p-1.5 rounded-full text-surface-400 hover:text-primary-600 hover:bg-surface-200 dark:hover:bg-surface-700"
        :title="`Email ${primaryEmail}`"
        @click.stop
      >
        <i class="pi pi-envelope text-sm" />
      </a>
      <a
        v-if="primaryPhone"
        :href="'tel:' + primaryPhone"
        class="p-1.5 rounded-full text-surface-400 hover:text-primary-600 hover:bg-surface-200 dark:hover:bg-surface-700"
        :title="`Call ${primaryPhone}`"
        @click.stop
      >
        <i class="pi pi-phone text-sm" />
      </a>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Contact } from '~/types/contacts';

// One row of the dashboard's recent-contacts widget (story 042). Mirrors
// components/contacts/ContactListItem.vue's avatar/primary-value handling but
// swaps the edit/delete actions for the story's email/phone quick actions —
// which are safe on a read-only shared book, unlike edit/delete.
const props = defineProps<{
  contact: Contact;
}>();

defineEmits<{
  select: [contact: Contact];
}>();

// Photos sit behind Bearer auth, so fetch them via the authenticated client and
// render a blob URL instead of pointing <img> at the raw URL (401 otherwise).
const photoSrc = useAuthedImage(() => props.contact.photo_url);

const primaryEmail = computed(
  () => props.contact.emails?.find((e) => e.primary)?.value || props.contact.emails?.[0]?.value || ''
);

const primaryPhone = computed(
  () => props.contact.phones?.find((p) => p.primary)?.value || props.contact.phones?.[0]?.value || ''
);

// Organization is the story's requested secondary line; fall back to the primary
// email so the row is never left with an empty subtitle.
const subtitle = computed(() => props.contact.organization || primaryEmail.value || '—');

const initials = computed(() => contactInitials(props.contact.formatted_name));
const avatarColor = computed(() =>
  contactAvatarColor(props.contact.formatted_name || props.contact.id)
);
</script>
