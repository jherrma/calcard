<template>
  <aside class="w-64 bg-surface-0 dark:bg-surface-900 border-r border-surface-200 dark:border-surface-800 flex-col hidden lg:flex">
    <div class="p-4 border-b border-surface-200 dark:border-surface-800">
      <Button
        label="Add Contact"
        icon="pi pi-plus"
        class="w-full"
        @click="navigateTo('/contacts/new')"
      />
    </div>

    <div class="flex-1 overflow-y-auto p-4">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-surface-700 dark:text-surface-300">Address Books</h3>
        <button
          class="text-surface-400 hover:text-surface-600 dark:hover:text-surface-200"
          @click="$emit('add-addressbook')"
        >
          <i class="pi pi-plus text-sm" />
        </button>
      </div>

      <div class="space-y-1">
        <!-- All Contacts option -->
        <div
          class="flex items-center gap-2 p-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800 cursor-pointer"
          @click="$emit('select-all')"
        >
          <Checkbox
            :model-value="allSelected"
            :binary="true"
            @update:model-value="$emit('select-all')"
          />
          <i class="pi pi-address-book text-surface-500" />
          <span class="flex-1 text-sm text-surface-700 dark:text-surface-300">All Contacts</span>
          <span class="text-xs text-surface-400">{{ totalCount }}</span>
        </div>

        <!-- Individual address books -->
        <div
          v-for="ab in addressBooks"
          :key="ab.ID"
          class="flex items-center gap-2 p-2 rounded-lg hover:bg-surface-100 dark:hover:bg-surface-800 group cursor-pointer"
          @click="$emit('toggle', ab.ID)"
        >
          <Checkbox
            :model-value="selectedIds.has(ab.ID)"
            :binary="true"
            @update:model-value="$emit('toggle', ab.ID)"
          />
          <i class="pi pi-book text-surface-500" />
          <span class="flex-1 text-sm truncate text-surface-700 dark:text-surface-300">{{ ab.Name }}</span>
          <button
            class="opacity-0 group-hover:opacity-100 text-surface-400 hover:text-surface-600 dark:hover:text-surface-200"
            @click.stop="showMenu($event, ab)"
          >
            <i class="pi pi-ellipsis-v text-sm" />
          </button>
        </div>
      </div>
    </div>

    <Menu ref="menuRef" :model="menuItems" :popup="true" />
  </aside>
</template>

<script setup lang="ts">
import type { AddressBook } from '~/types/contacts';

const props = defineProps<{
  addressBooks: AddressBook[];
  selectedIds: Set<number>;
  totalCount: number;
}>();

const emit = defineEmits<{
  toggle: [id: number];
  'select-all': [];
  'add-addressbook': [];
  'edit-addressbook': [ab: AddressBook];
  'share-addressbook': [ab: AddressBook];
  'delete-addressbook': [ab: AddressBook];
}>();

const allSelected = computed(() =>
  props.addressBooks.length > 0 && props.selectedIds.size === props.addressBooks.length
);

const menuRef = ref();
const selectedAb = ref<AddressBook | null>(null);

const menuItems = computed(() => [
  {
    label: 'Edit',
    icon: 'pi pi-pencil',
    command: () => { if (selectedAb.value) emit('edit-addressbook', selectedAb.value); },
  },
  {
    label: 'Share',
    icon: 'pi pi-share-alt',
    command: () => { if (selectedAb.value) emit('share-addressbook', selectedAb.value); },
  },
  { separator: true },
  {
    label: 'Delete',
    icon: 'pi pi-trash',
    class: 'text-red-600',
    command: () => { if (selectedAb.value) emit('delete-addressbook', selectedAb.value); },
  },
]);

const showMenu = (event: Event, ab: AddressBook) => {
  selectedAb.value = ab;
  menuRef.value.toggle(event);
};
</script>
