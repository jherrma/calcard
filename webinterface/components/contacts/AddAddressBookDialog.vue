<template>
  <Dialog
    :visible="visible"
    header="Create Address Book"
    :modal="true"
    :style="{ width: '450px' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <div class="space-y-4">
      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Name *</label>
        <InputText v-model="form.name" class="w-full" placeholder="My Contacts" />
      </div>

      <div>
        <label class="block text-sm font-medium text-surface-700 dark:text-surface-300 mb-1">Description</label>
        <Textarea v-model="form.description" class="w-full" rows="2" />
      </div>
    </div>

    <template #footer>
      <Button label="Cancel" severity="secondary" @click="$emit('update:visible', false)" />
      <Button
        label="Create"
        :loading="isCreating"
        :disabled="!form.name.trim()"
        @click="create"
      />
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { useContactsStore } from '~/stores/contacts';

defineProps<{
  visible: boolean;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  created: [];
}>();

const contactsStore = useContactsStore();
const toast = useAppToast();

const isCreating = ref(false);

const form = reactive({
  name: '',
  description: '',
});

const create = async () => {
  isCreating.value = true;
  try {
    await contactsStore.createAddressBook(form);
    toast.success('Address book created');
    emit('created');
    emit('update:visible', false);
    form.name = '';
    form.description = '';
  } catch (e: unknown) {
    toast.error((e as Error).message || 'Failed to create address book');
  } finally {
    isCreating.value = false;
  }
};
</script>
