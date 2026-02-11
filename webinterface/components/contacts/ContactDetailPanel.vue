<template>
  <div v-if="contact" class="h-full flex flex-col bg-surface-0 dark:bg-surface-900 border-l border-surface-200 dark:border-surface-800">
    <!-- Header -->
    <div class="flex items-center justify-between p-4 border-b border-surface-200 dark:border-surface-800">
      <h3 class="text-lg font-semibold text-surface-900 dark:text-surface-100 truncate">Contact Details</h3>
      <div class="flex items-center gap-1">
        <button
          class="p-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800 text-surface-500 hover:text-surface-700 dark:hover:text-surface-300"
          title="Edit"
          @click="$emit('edit', contact)"
        >
          <i class="pi pi-pencil" />
        </button>
        <button
          class="p-2 rounded-lg hover:bg-red-100 dark:hover:bg-red-900/30 text-surface-500 hover:text-red-600"
          title="Delete"
          @click="$emit('delete', contact)"
        >
          <i class="pi pi-trash" />
        </button>
        <button
          class="p-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800 text-surface-500 hover:text-surface-700 dark:hover:text-surface-300"
          title="Close"
          @click="$emit('close')"
        >
          <i class="pi pi-times" />
        </button>
      </div>
    </div>

    <!-- Content -->
    <div class="flex-1 overflow-y-auto p-4 space-y-6">
      <!-- Avatar and name -->
      <div class="flex flex-col items-center text-center">
        <div
          class="w-20 h-20 rounded-full flex items-center justify-center text-white font-bold text-2xl mb-3"
          :style="{ backgroundColor: avatarColor }"
        >
          <img
            v-if="contact.photo_url"
            :src="contact.photo_url"
            :alt="contact.formatted_name"
            class="w-20 h-20 rounded-full object-cover"
          >
          <span v-else>{{ initials }}</span>
        </div>
        <h4 class="text-xl font-semibold text-surface-900 dark:text-surface-100">{{ contact.formatted_name }}</h4>
        <p v-if="contact.title || contact.organization" class="text-sm text-surface-500">
          <span v-if="contact.title">{{ contact.title }}</span>
          <span v-if="contact.title && contact.organization"> at </span>
          <span v-if="contact.organization">{{ contact.organization }}</span>
        </p>
      </div>

      <!-- Emails -->
      <section v-if="contact.emails?.length">
        <h5 class="text-xs font-semibold uppercase text-surface-400 mb-2">Email</h5>
        <div class="space-y-2">
          <div v-for="(email, i) in contact.emails" :key="i" class="flex items-center gap-2">
            <i class="pi pi-envelope text-surface-400 text-sm" />
            <div class="flex-1 min-w-0">
              <a :href="'mailto:' + email.value" class="text-sm text-primary-600 dark:text-primary-400 hover:underline truncate block">
                {{ email.value }}
              </a>
              <span class="text-xs text-surface-400 capitalize">{{ email.type }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- Phones -->
      <section v-if="contact.phones?.length">
        <h5 class="text-xs font-semibold uppercase text-surface-400 mb-2">Phone</h5>
        <div class="space-y-2">
          <div v-for="(phone, i) in contact.phones" :key="i" class="flex items-center gap-2">
            <i class="pi pi-phone text-surface-400 text-sm" />
            <div class="flex-1 min-w-0">
              <a :href="'tel:' + phone.value" class="text-sm text-primary-600 dark:text-primary-400 hover:underline truncate block">
                {{ phone.value }}
              </a>
              <span class="text-xs text-surface-400 capitalize">{{ phone.type }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- Addresses -->
      <section v-if="contact.addresses?.length">
        <h5 class="text-xs font-semibold uppercase text-surface-400 mb-2">Address</h5>
        <div class="space-y-2">
          <div v-for="(addr, i) in contact.addresses" :key="i" class="flex items-start gap-2">
            <i class="pi pi-map-marker text-surface-400 text-sm mt-0.5" />
            <div>
              <a
                :href="mapsUrl(addr)"
                target="_blank"
                rel="noopener"
                class="hover:underline text-primary-600 dark:text-primary-400"
              >
                <div v-if="addr.street" class="text-sm">{{ addr.street }}</div>
                <div class="text-sm">
                  <span v-if="addr.city">{{ addr.city }}</span>
                  <span v-if="addr.city && addr.state">, </span>
                  <span v-if="addr.state">{{ addr.state }}</span>
                  <span v-if="addr.postal_code"> {{ addr.postal_code }}</span>
                </div>
                <div v-if="addr.country" class="text-sm">{{ addr.country }}</div>
              </a>
              <span class="text-xs text-surface-400 capitalize">{{ addr.type }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- URLs -->
      <section v-if="contact.urls?.length">
        <h5 class="text-xs font-semibold uppercase text-surface-400 mb-2">Links</h5>
        <div class="space-y-2">
          <div v-for="(url, i) in contact.urls" :key="i" class="flex items-center gap-2">
            <i class="pi pi-link text-surface-400 text-sm" />
            <a :href="url.value" target="_blank" rel="noopener" class="text-sm text-primary-600 dark:text-primary-400 hover:underline truncate">
              {{ url.value }}
            </a>
          </div>
        </div>
      </section>

      <!-- Birthday -->
      <section v-if="contact.birthday">
        <h5 class="text-xs font-semibold uppercase text-surface-400 mb-2">Birthday</h5>
        <div class="flex items-center gap-2">
          <i class="pi pi-calendar text-surface-400 text-sm" />
          <span class="text-sm text-surface-700 dark:text-surface-300">{{ contact.birthday }}</span>
        </div>
      </section>

      <!-- Notes -->
      <section v-if="contact.notes">
        <h5 class="text-xs font-semibold uppercase text-surface-400 mb-2">Notes</h5>
        <p class="text-sm text-surface-700 dark:text-surface-300 whitespace-pre-wrap">{{ contact.notes }}</p>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Contact, ContactAddress } from '~/types/contacts';

const props = defineProps<{
  contact: Contact | null;
}>();

defineEmits<{
  edit: [contact: Contact];
  delete: [contact: Contact];
  close: [];
}>();

const initials = computed(() => {
  if (!props.contact) return '';
  const name = props.contact.formatted_name || '';
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]!.charAt(0) + parts[parts.length - 1]!.charAt(0)).toUpperCase();
  return (parts[0]?.[0] || '?').toUpperCase();
});

const mapsUrl = (addr: ContactAddress): string => {
  const parts = [addr.street, addr.city, addr.state, addr.postal_code, addr.country].filter(Boolean);
  return `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(parts.join(', '))}`;
};

const avatarColor = computed(() => {
  if (!props.contact) return '#3b82f6';
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
