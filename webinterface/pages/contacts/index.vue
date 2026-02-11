<template>
  <div class="flex h-[calc(100vh-8rem)]">
    <!-- Sidebar -->
    <ContactsSidebar
      :address-books="contactsStore.addressBooks"
      :selected-ids="contactsStore.selectedAddressBookIds"
      :total-count="contactsStore.contacts.length"
      @toggle="contactsStore.toggleAddressBook"
      @select-all="contactsStore.selectAllAddressBooks"
    />

    <!-- Main content area -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- Toolbar -->
      <div class="flex items-center gap-3 px-4 py-3 border-b border-surface-200 dark:border-surface-800 bg-surface-0 dark:bg-surface-900">
        <!-- Search -->
        <div class="relative flex-1 max-w-md">
          <i class="pi pi-search absolute left-3 top-1/2 -translate-y-1/2 text-surface-400 text-sm" />
          <input
            v-model="searchInput"
            type="text"
            placeholder="Search contacts..."
            class="w-full pl-9 pr-3 py-2 text-sm rounded-lg border border-surface-300 dark:border-surface-600 bg-surface-0 dark:bg-surface-800 text-surface-900 dark:text-surface-100 placeholder-surface-400 focus:outline-none focus:ring-2 focus:ring-primary-500 focus:border-transparent"
          >
        </div>

        <!-- Sort -->
        <Select
          v-model="contactsStore.sortBy"
          :options="sortOptions"
          option-label="label"
          option-value="value"
          class="w-40 text-sm"
          placeholder="Sort by"
        />

        <!-- Add Contact button (mobile) -->
        <Button
          icon="pi pi-plus"
          label="Add Contact"
          class="lg:hidden"
          size="small"
          @click="navigateTo('/contacts/new')"
        />
      </div>

      <!-- Contact list -->
      <div class="flex-1 flex overflow-hidden">
        <div
          v-if="contactsStore.isLoading"
          class="flex-1 p-4"
        >
          <SkeletonList :count="8" />
        </div>

        <div
          v-else-if="contactsStore.error"
          class="flex-1 flex flex-col items-center justify-center p-8 text-center"
        >
          <i class="pi pi-exclamation-triangle text-4xl text-red-400 mb-4" />
          <p class="text-surface-600 dark:text-surface-400 mb-4">{{ contactsStore.error }}</p>
          <Button label="Retry" icon="pi pi-refresh" severity="secondary" @click="loadData" />
        </div>

        <div
          v-else-if="contactList.length === 0"
          class="flex-1 flex flex-col items-center justify-center p-8 text-center"
        >
          <i class="pi pi-users text-4xl text-surface-300 dark:text-surface-600 mb-4" />
          <p class="text-surface-600 dark:text-surface-400 mb-2">
            {{ contactsStore.searchQuery ? 'No contacts found' : 'No contacts yet' }}
          </p>
          <p v-if="!contactsStore.searchQuery" class="text-sm text-surface-400 mb-4">Add your first contact to get started.</p>
          <Button
            v-if="!contactsStore.searchQuery"
            label="Add Contact"
            icon="pi pi-plus"
            @click="navigateTo('/contacts/new')"
          />
        </div>

        <!-- Virtual scrolled contact list -->
        <div v-else ref="listContainerRef" class="flex-1 overflow-y-auto relative" @scroll="onScroll">
          <div :style="{ height: (totalHeight + LIST_PADDING_TOP) + 'px' }" class="relative">
            <!-- Contact cards with inline letter indicators -->
            <div
              v-for="item in visibleItems"
              :key="item.key"
              :style="{ position: 'absolute', top: (item.top + LIST_PADDING_TOP) + 'px', left: 0, right: 0, height: item.height + 'px' }"
              class="flex justify-center px-4 pl-12 md:pl-16"
            >
              <!-- Letter indicator for first contact in group -->
              <span
                v-if="item.isFirstInGroup"
                class="absolute left-3 md:left-5 top-1/2 -translate-y-1/2 text-2xl md:text-3xl font-bold text-surface-300 dark:text-surface-600 select-none pointer-events-none"
              >
                {{ item.letter }}
              </span>

              <div class="w-full max-w-2xl">
                <ContactListItem
                  :contact="item.contact!"
                  :search-query="contactsStore.searchQuery"
                  @click="selectContact"
                  @edit="handleEdit"
                  @delete="handleDelete"
                />
              </div>
            </div>
          </div>
        </div>

        <!-- Alphabet navigation -->
        <AlphabetNavigation
          v-if="contactList.length > 0"
          :available-letters="contactsStore.availableLetters"
          class="hidden md:flex"
          @scroll-to="scrollToLetter"
        />

      </div>
    </div>

  </div>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import ContactsSidebar from '~/components/contacts/ContactsSidebar.vue';
import ContactListItem from '~/components/contacts/ContactListItem.vue';
import AlphabetNavigation from '~/components/contacts/AlphabetNavigation.vue';
import { useContactsStore } from '~/stores/contacts';
import type { Contact } from '~/types/contacts';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const contactsStore = useContactsStore();
const toast = useAppToast();
const confirm = useConfirm();
const searchInput = ref('');
const listContainerRef = ref<HTMLElement | null>(null);
const scrollTop = ref(0);

const sortOptions = [
  { label: 'First Name', value: 'first_name' as const },
  { label: 'Last Name', value: 'last_name' as const },
];

// Debounced search
const debouncedSearch = useDebounceFn((query: string) => {
  contactsStore.searchContacts(query);
}, 300);

watch(searchInput, (val) => {
  debouncedSearch(val);
});

// Virtual scroll — contacts only (no inline headers)
const ROW_HEIGHT = 84; // card height (64) + gap (20)
const LIST_PADDING_TOP = 16;

interface FlatItem {
  type: 'contact';
  key: string;
  letter: string;
  isFirstInGroup: boolean;
  contact: Contact;
  height: number;
}

const contactList = computed<FlatItem[]>(() => {
  const items: FlatItem[] = [];
  for (const [letter, contacts] of contactsStore.groupedContacts) {
    for (let j = 0; j < contacts.length; j++) {
      const contact = contacts[j]!;
      items.push({
        type: 'contact',
        key: `contact-${contact.id}`,
        letter,
        isFirstInGroup: j === 0,
        contact,
        height: ROW_HEIGHT,
      });
    }
  }
  return items;
});

// Precompute cumulative offsets
const itemOffsets = computed(() => {
  const offsets: number[] = [];
  let offset = 0;
  for (const item of contactList.value) {
    offsets.push(offset);
    offset += item.height;
  }
  return offsets;
});

const totalHeight = computed(() => {
  const len = contactList.value.length;
  if (len === 0) return 0;
  return itemOffsets.value[len - 1]! + contactList.value[len - 1]!.height;
});

const visibleItems = computed(() => {
  const container = listContainerRef.value;
  if (!container || contactList.value.length === 0) {
    return contactList.value.map((item, i) => ({ ...item, top: itemOffsets.value[i]! }));
  }

  const viewTop = scrollTop.value;
  const viewBottom = viewTop + container.clientHeight;
  const buffer = 200;

  const result: (FlatItem & { top: number })[] = [];
  for (let i = 0; i < contactList.value.length; i++) {
    const itemTop = itemOffsets.value[i]!;
    const item = contactList.value[i]!;
    const bottom = itemTop + item.height;
    if (bottom >= viewTop - buffer && itemTop <= viewBottom + buffer) {
      result.push({ ...item, top: itemTop });
    }
  }
  return result;
});

const onScroll = () => {
  if (listContainerRef.value) {
    scrollTop.value = listContainerRef.value.scrollTop;
  }
};

// Scroll to letter
const scrollToLetter = (letter: string) => {
  const container = listContainerRef.value;
  if (!container) return;

  const idx = contactList.value.findIndex(item => item.letter === letter);
  if (idx >= 0) {
    container.scrollTop = itemOffsets.value[idx] ?? 0;
  }
};

// Contact actions
const selectContact = (contact: Contact) => {
  navigateTo(`/contacts/${contact.id}?ab=${contact.addressbook_id}`);
};

const handleEdit = (contact: Contact) => {
  navigateTo(`/contacts/${contact.id}/edit?ab=${contact.addressbook_id}`);
};

const handleDelete = (contact: Contact) => {
  confirm.require({
    message: `Are you sure you want to delete "${contact.formatted_name}"?`,
    header: 'Delete Contact',
    icon: 'pi pi-exclamation-triangle',
    rejectLabel: 'Cancel',
    acceptLabel: 'Delete',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        const ab = contactsStore.addressBooks.find(ab => String(ab.ID) === contact.addressbook_id);
        if (!ab) throw new Error('Address book not found');
        await contactsStore.deleteContact(ab.ID, contact.id);
        toast.success('Contact deleted');
      } catch (e: unknown) {
        toast.error((e as Error).message || 'Failed to delete contact');
      }
    },
  });
};

// Load data
const loadData = async () => {
  await contactsStore.fetchAddressBooks();
  await contactsStore.fetchContacts();
};

onMounted(() => {
  loadData();
});
</script>
