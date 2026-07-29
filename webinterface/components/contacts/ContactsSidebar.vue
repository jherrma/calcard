<template>
  <aside class="w-64 bg-surface-0 dark:bg-surface-900 border-r border-surface-200 dark:border-surface-800 flex-col hidden lg:flex">
    <div class="p-4 border-b border-surface-200 dark:border-surface-800">
      <Button
        label="Add Contact"
        icon="pi pi-plus"
        class="w-full"
        :disabled="!hasWritableBook"
        :title="hasWritableBook ? undefined : 'You have read-only access to every address book'"
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
          <i
            :class="ab.shared ? 'pi pi-users' : 'pi pi-book'"
            class="text-surface-500"
            :title="sharedLabel(ab)"
          />
          <span class="flex-1 text-sm truncate text-surface-700 dark:text-surface-300" :title="sharedLabel(ab)">
            {{ ab.Name }}
          </span>
          <!-- Read-only shares get an explicit marker: without it a sharee has
               no way to tell why the edit controls are missing. -->
          <i
            v-if="isReadOnly(ab)"
            class="pi pi-lock text-xs text-surface-400"
            title="Read-only access"
          />
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

// A shared book with only 'read' grants no writes (#53).
const isReadOnly = (ab: AddressBook) => !!ab.shared && ab.permission !== 'read-write';

const sharedLabel = (ab: AddressBook) =>
  ab.shared
    ? `Shared by ${ab.owner?.display_name || 'another user'}${ab.permission === 'read-write' ? '' : ' (read-only)'}`
    : undefined;

// "Add Contact" needs somewhere to put it. With only read-only shares there is
// no writable target, so the button would always 403.
const hasWritableBook = computed(() =>
  props.addressBooks.some((ab: AddressBook) => !ab.shared || ab.permission === 'read-write')
);

const menuRef = ref();
const selectedAb = ref<AddressBook | null>(null);

// Renaming, re-sharing and deleting a book are OWNER-only on the backend (a
// sharee gets a 404), so those entries only appear for books you own. A shared
// book's menu shows who shared it instead of actions that cannot succeed.
const menuItems = computed(() => {
  const ab = selectedAb.value;
  if (ab && ab.shared) {
    return [
      {
        label: sharedLabel(ab),
        icon: ab.permission === 'read-write' ? 'pi pi-users' : 'pi pi-lock',
        disabled: true,
      },
    ];
  }
  return [
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
  ];
});

const showMenu = (event: Event, ab: AddressBook) => {
  selectedAb.value = ab;
  menuRef.value.toggle(event);
};
</script>
