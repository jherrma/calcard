<template>
  <div class="flex-1 overflow-y-auto p-4 md:p-8">
    <div class="max-w-2xl mx-auto">
      <Card>
        <template #content>
          <!-- Loading -->
          <div v-if="isLoading" class="flex justify-center p-8">
            <ProgressSpinner />
          </div>

          <!-- Error -->
          <div v-else-if="loadError" class="flex flex-col items-center gap-4 p-8 text-center">
            <i class="pi pi-exclamation-triangle text-4xl text-red-400" />
            <p class="text-surface-600 dark:text-surface-400">{{ loadError }}</p>
            <Button label="Back to Contacts" icon="pi pi-arrow-left" severity="secondary" @click="navigateTo('/contacts')" />
          </div>

          <!-- Contact detail -->
          <div v-else-if="contact">
            <!-- Header -->
            <div class="flex items-center justify-between mb-6">
              <h2 class="text-xl font-semibold text-surface-900 dark:text-surface-100">Contact</h2>
              <div class="flex items-center gap-4">
                <button
                  class="text-sm font-medium text-primary-600 dark:text-primary-400 hover:text-primary-700 dark:hover:text-primary-300"
                  @click="navigateTo(`/contacts/${contactId}/edit?ab=${abIdParam}`)"
                >
                  Edit
                </button>
                <button
                  class="text-sm font-medium text-red-600 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300"
                  @click="handleDelete"
                >
                  Delete
                </button>
              </div>
            </div>

            <!-- Avatar and name -->
            <div class="flex flex-col items-center text-center mb-6">
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
              <h3 class="text-xl font-semibold text-surface-900 dark:text-surface-100">{{ contact.formatted_name }}</h3>
              <p v-if="contact.title || contact.organization" class="text-sm text-surface-500">
                <span v-if="contact.title">{{ contact.title }}</span>
                <span v-if="contact.title && contact.organization"> at </span>
                <span v-if="contact.organization">{{ contact.organization }}</span>
              </p>
            </div>

            <div class="space-y-6">
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
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { Contact, ContactAddress } from '~/types/contacts';
import { useContactsStore } from '~/stores/contacts';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const route = useRoute();
const contactsStore = useContactsStore();
const toast = useAppToast();
const confirm = useConfirm();

const contactId = route.params.id as string;
const abIdParam = route.query.ab as string;

const contact = ref<Contact | null>(null);
const isLoading = ref(true);
const loadError = ref<string | null>(null);
const numericAbId = ref<number | null>(null);

onMounted(async () => {
  try {
    if (contactsStore.addressBooks.length === 0) {
      await contactsStore.fetchAddressBooks();
    }

    if (!abIdParam) {
      loadError.value = 'Missing address book parameter';
      isLoading.value = false;
      return;
    }

    const ab = contactsStore.getAddressBookByNumericId(abIdParam);
    if (!ab) {
      loadError.value = 'Address book not found';
      isLoading.value = false;
      return;
    }

    numericAbId.value = ab.ID;
    contact.value = await contactsStore.getContact(ab.ID, contactId);
  } catch (e: unknown) {
    loadError.value = (e as Error).message || 'Failed to load contact';
  } finally {
    isLoading.value = false;
  }
});

const initials = computed(() => {
  if (!contact.value) return '';
  const name = contact.value.formatted_name || '';
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]!.charAt(0) + parts[parts.length - 1]!.charAt(0)).toUpperCase();
  return (parts[0]?.[0] || '?').toUpperCase();
});

const avatarColor = computed(() => {
  if (!contact.value) return '#3b82f6';
  const name = contact.value.formatted_name || contact.value.id;
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

const mapsUrl = (addr: ContactAddress): string => {
  const parts = [addr.street, addr.city, addr.state, addr.postal_code, addr.country].filter(Boolean);
  return `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(parts.join(', '))}`;
};

const handleDelete = () => {
  if (!numericAbId.value || !contact.value) return;

  confirm.require({
    message: `Are you sure you want to delete "${contact.value.formatted_name}"?`,
    header: 'Delete Contact',
    icon: 'pi pi-exclamation-triangle',
    rejectLabel: 'Cancel',
    acceptLabel: 'Delete',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await contactsStore.deleteContact(numericAbId.value!, contactId);
        toast.success('Contact deleted');
        navigateTo('/contacts');
      } catch (e: unknown) {
        toast.error((e as Error).message || 'Failed to delete contact');
      }
    },
  });
};
</script>
