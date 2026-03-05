<template>
  <Dialog
    :visible="visible"
    :header="`${addressBook?.Name || 'Address Book'} Settings`"
    :modal="true"
    :style="{ width: '600px' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <Tabs :value="activeTab" @update:value="activeTab = $event as string">
      <TabList>
        <Tab value="general">General</Tab>
        <Tab value="sharing">Sharing</Tab>
        <Tab value="integration">Integration</Tab>
      </TabList>
      <TabPanels>
        <!-- General Tab -->
        <TabPanel value="general">
          <div class="space-y-4 pt-4">
            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Name</label>
              <InputText v-model="form.name" class="w-full" />
            </div>

            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Description</label>
              <Textarea v-model="form.description" class="w-full" rows="3" />
            </div>
          </div>
        </TabPanel>

        <!-- Sharing Tab -->
        <TabPanel value="sharing">
          <div class="pt-4">
            <ContactsAddressBookSharing v-if="addressBook" :address-book-id="addressBook.ID" />
          </div>
        </TabPanel>

        <!-- Integration Tab -->
        <TabPanel value="integration">
          <div class="space-y-4 pt-4">
            <div>
              <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">CardDAV URL</label>
              <div class="flex gap-2">
                <InputText :model-value="carddavUrl" readonly class="flex-1 font-mono text-sm" />
                <Button icon="pi pi-copy" severity="secondary" @click="copyUrl" />
              </div>
            </div>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>

    <template #footer>
      <div class="flex justify-between w-full">
        <Button
          label="Delete Address Book"
          severity="danger"
          text
          @click="confirmDelete"
        />
        <div class="flex gap-2">
          <Button label="Cancel" severity="secondary" @click="$emit('update:visible', false)" />
          <Button label="Save" :loading="isSaving" @click="save" />
        </div>
      </div>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import type { AddressBook } from '~/types/contacts';
import { useContactsStore } from '~/stores/contacts';

const props = defineProps<{
  visible: boolean;
  addressBook: AddressBook | null;
  initialTab?: string;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  updated: [];
  deleted: [];
}>();

const toast = useAppToast();
const confirm = useConfirm();
const contactsStore = useContactsStore();
const config = useRuntimeConfig();

const isSaving = ref(false);
const activeTab = ref('general');

const form = reactive({
  name: '',
  description: '',
});

watch(() => props.addressBook, (ab) => {
  if (ab) {
    form.name = ab.Name;
    form.description = ab.Description || '';
  }
}, { immediate: true });

watch(() => props.initialTab, (tab) => {
  if (tab) activeTab.value = tab;
});

watch(() => props.visible, (vis) => {
  if (vis && props.initialTab) activeTab.value = props.initialTab;
});

const carddavUrl = computed(() => {
  if (!props.addressBook) return '';
  const base = (config.public.apiBaseUrl as string) || '';
  return `${base}/dav/addressbooks/me/${props.addressBook.UUID}/`;
});

const save = async () => {
  if (!props.addressBook) return;
  isSaving.value = true;
  try {
    await contactsStore.updateAddressBook(props.addressBook.ID, {
      name: form.name,
      description: form.description,
    });
    toast.success('Address book updated');
    emit('updated');
    emit('update:visible', false);
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to update address book');
  } finally {
    isSaving.value = false;
  }
};

const confirmDelete = () => {
  confirm.require({
    message: 'Are you sure you want to delete this address book? All contacts will be permanently deleted.',
    header: 'Delete Address Book',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: deleteAddressBook,
  });
};

const deleteAddressBook = async () => {
  if (!props.addressBook) return;
  try {
    await contactsStore.deleteAddressBook(props.addressBook.ID);
    toast.success('Address book deleted');
    emit('deleted');
    emit('update:visible', false);
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to delete address book');
  }
};

const copyUrl = async () => {
  await navigator.clipboard.writeText(carddavUrl.value);
  toast.success('URL copied');
};
</script>
