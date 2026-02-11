<template>
  <div class="flex-1 overflow-y-auto p-4 md:p-8">
    <div class="max-w-2xl mx-auto">
      <Card>
        <template #content>
          <div class="flex items-center justify-between mb-4">
            <h2 class="text-xl font-semibold text-surface-900 dark:text-surface-100">Edit Contact</h2>
            <button
              class="text-sm font-medium text-primary-600 dark:text-primary-400 hover:text-primary-700 dark:hover:text-primary-300"
              @click="navigateTo('/contacts')"
            >
              Cancel
            </button>
          </div>
          <ContactsContactForm
            v-if="contact && !isLoading"
            ref="contactFormRef"
            :contact="contact"
            :address-books="contactsStore.addressBooks"
            :is-submitting="isSubmitting"
            :hide-actions="true"
            @submit="handleSubmit"
          />
          <div v-else-if="loadError" class="flex flex-col items-center gap-4 p-8 text-center">
            <i class="pi pi-exclamation-triangle text-4xl text-red-400" />
            <p class="text-surface-600 dark:text-surface-400">{{ loadError }}</p>
            <Button label="Back to Contacts" icon="pi pi-arrow-left" severity="secondary" @click="navigateTo('/contacts')" />
          </div>
          <div v-else class="flex justify-center p-8">
            <ProgressSpinner />
          </div>
        </template>
      </Card>
    </div>

    <!-- Floating save button -->
    <Button
      v-if="contact && !isLoading"
      icon="pi pi-check"
      label="Save"
      class="!fixed bottom-6 right-6 !shadow-lg"
      :loading="isSubmitting"
      rounded
      @click="contactFormRef?.triggerSubmit()"
    />
  </div>
</template>

<script setup lang="ts">
import type { Contact, ContactFormData } from '~/types/contacts';
import { useContactsStore } from '~/stores/contacts';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const route = useRoute();
const contactsStore = useContactsStore();
const toast = useAppToast();

const contactId = route.params.id as string;
const abIdParam = route.query.ab as string;

const contact = ref<Contact | null>(null);
const isLoading = ref(true);
const isSubmitting = ref(false);
const loadError = ref<string | null>(null);
const contactFormRef = ref<{ triggerSubmit: () => void } | null>(null);
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

const handleSubmit = async (data: ContactFormData, photo: File | undefined, removePhoto: boolean) => {
  if (!numericAbId.value) return;

  isSubmitting.value = true;
  try {
    await contactsStore.updateContact(numericAbId.value, contactId, data);

    if (removePhoto) {
      try {
        await contactsStore.deletePhoto(numericAbId.value, contactId);
      } catch {
        toast.warn('Contact saved but photo removal failed');
      }
    } else if (photo) {
      try {
        await contactsStore.uploadPhoto(numericAbId.value, contactId, photo);
      } catch {
        toast.warn('Contact saved but photo upload failed');
      }
    }

    toast.success('Contact updated');
    navigateTo('/contacts');
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to update contact');
  } finally {
    isSubmitting.value = false;
  }
};
</script>
