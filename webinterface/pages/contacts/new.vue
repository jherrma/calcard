<template>
  <div class="flex-1 overflow-y-auto p-4 md:p-8">
    <div class="max-w-2xl mx-auto">
      <Card>
        <template #content>
          <div class="flex items-center justify-between mb-4">
            <h2 class="text-xl font-semibold text-surface-900 dark:text-surface-100">New Contact</h2>
            <button
              class="text-sm font-medium text-primary-600 dark:text-primary-400 hover:text-primary-700 dark:hover:text-primary-300"
              @click="navigateTo('/contacts')"
            >
              Cancel
            </button>
          </div>
          <ContactsContactForm
            v-if="!isLoadingAbs"
            ref="contactFormRef"
            :address-books="contactsStore.addressBooks"
            :is-submitting="isSubmitting"
            :hide-actions="true"
            @submit="handleSubmit"
            @cancel="navigateTo('/contacts')"
          />
          <div v-else class="flex justify-center p-8">
            <ProgressSpinner />
          </div>
        </template>
      </Card>
    </div>

    <!-- Floating create button -->
    <Button
      v-if="!isLoadingAbs"
      icon="pi pi-check"
      label="Create"
      class="!fixed bottom-6 right-6 !shadow-lg"
      :loading="isSubmitting"
      rounded
      @click="contactFormRef?.triggerSubmit()"
    />
  </div>
</template>

<script setup lang="ts">
import type { ContactFormData } from '~/types/contacts';
import { useContactsStore } from '~/stores/contacts';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const contactsStore = useContactsStore();
const toast = useAppToast();
const isSubmitting = ref(false);
const isLoadingAbs = ref(false);
const contactFormRef = ref<{ selectedAddressBookId: number | null; triggerSubmit: () => void } | null>(null);

onMounted(async () => {
  if (contactsStore.addressBooks.length === 0) {
    isLoadingAbs.value = true;
    await contactsStore.fetchAddressBooks();
    isLoadingAbs.value = false;
  }
});

const handleSubmit = async (data: ContactFormData, photo: File | undefined, _removePhoto: boolean) => {
  const abId = contactFormRef.value?.selectedAddressBookId;
  if (!abId) {
    toast.error('No address book selected');
    return;
  }

  isSubmitting.value = true;
  try {
    const contact = await contactsStore.createContact(abId, data);

    if (photo) {
      try {
        await contactsStore.uploadPhoto(abId, contact.id, photo);
      } catch {
        toast.warn('Contact created but photo upload failed');
      }
    }

    toast.success('Contact created');
    navigateTo('/contacts');
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to create contact');
  } finally {
    isSubmitting.value = false;
  }
};
</script>
