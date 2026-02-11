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
          v-else-if="flatList.length === 0"
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
        <div v-else ref="listContainerRef" class="flex-1 overflow-y-auto" @scroll="onScroll">
          <div :style="{ height: totalHeight + 'px', position: 'relative' }">
            <div
              v-for="item in visibleItems"
              :key="item.key"
              :style="{ position: 'absolute', top: item.top + 'px', left: 0, right: 0, height: item.height + 'px' }"
            >
              <!-- Section header -->
              <div
                v-if="item.type === 'header'"
                class="sticky top-0 z-10 px-4 py-1.5 text-xs font-bold uppercase text-surface-500 dark:text-surface-400 bg-surface-50 dark:bg-surface-800 border-b border-surface-200 dark:border-surface-700"
                :data-letter="item.letter"
              >
                {{ item.letter }}
              </div>
              <!-- Contact row -->
              <ContactListItem
                v-else
                :contact="item.contact!"
                :search-query="contactsStore.searchQuery"
                @click="selectContact"
                @edit="handleEdit"
                @delete="handleDelete"
              />
            </div>
          </div>
        </div>

        <!-- Alphabet navigation -->
        <AlphabetNavigation
          v-if="flatList.length > 0"
          :available-letters="contactsStore.availableLetters"
          class="hidden md:flex"
          @scroll-to="scrollToLetter"
        />

        <!-- Detail panel (desktop) -->
        <div
          v-if="selectedContact && !isMobile"
          class="w-96 flex-shrink-0 hidden md:block"
        >
          <ContactDetailPanel
            :contact="selectedContact"
            @edit="handleEdit"
            @delete="handleDelete"
            @close="selectedContact = null"
          />
        </div>
      </div>
    </div>

    <!-- Detail dialog (mobile) -->
    <Dialog
      v-model:visible="showMobileDetail"
      modal
      :header="selectedContact?.formatted_name || 'Contact'"
      class="w-full max-w-lg"
      :dismissable-mask="true"
    >
      <ContactDetailPanel
        v-if="selectedContact"
        :contact="selectedContact"
        @edit="handleEdit"
        @delete="handleDelete"
        @close="showMobileDetail = false"
      />
    </Dialog>

    <ConfirmDialog />
  </div>
</template>

<script setup lang="ts">
import { useConfirm } from 'primevue/useconfirm';
import ContactsSidebar from '~/components/contacts/ContactsSidebar.vue';
import ContactListItem from '~/components/contacts/ContactListItem.vue';
import AlphabetNavigation from '~/components/contacts/AlphabetNavigation.vue';
import ContactDetailPanel from '~/components/contacts/ContactDetailPanel.vue';
import { useContactsStore } from '~/stores/contacts';
import type { Contact } from '~/types/contacts';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const contactsStore = useContactsStore();
const toast = useAppToast();
const confirm = useConfirm();
const isMobile = useMediaQuery('(max-width: 767px)');

const selectedContact = ref<Contact | null>(null);
const showMobileDetail = ref(false);
const searchInput = ref('');
const listContainerRef = ref<HTMLElement | null>(null);
const scrollTop = ref(0);

const sortOptions = [
  { label: 'Name', value: 'name' as const },
  { label: 'Organization', value: 'organization' as const },
  { label: 'Email', value: 'email' as const },
  { label: 'Last Updated', value: 'updated' as const },
];

// Debounced search
const debouncedSearch = useDebounceFn((query: string) => {
  contactsStore.searchContacts(query);
}, 300);

watch(searchInput, (val) => {
  debouncedSearch(val);
});

// Virtual scroll constants
const HEADER_HEIGHT = 28;
const ROW_HEIGHT = 64;

interface FlatItem {
  type: 'header' | 'contact';
  key: string;
  letter?: string;
  contact?: Contact;
  height: number;
}

const flatList = computed<FlatItem[]>(() => {
  const items: FlatItem[] = [];
  for (const [letter, contacts] of contactsStore.groupedContacts) {
    items.push({ type: 'header', key: `header-${letter}`, letter, height: HEADER_HEIGHT });
    for (const contact of contacts) {
      items.push({ type: 'contact', key: `contact-${contact.id}`, contact, height: ROW_HEIGHT });
    }
  }
  return items;
});

// Precompute cumulative offsets for virtual scroll
const itemOffsets = computed(() => {
  const offsets: number[] = [];
  let offset = 0;
  for (const item of flatList.value) {
    offsets.push(offset);
    offset += item.height;
  }
  return offsets;
});

const totalHeight = computed(() => {
  const len = flatList.value.length;
  if (len === 0) return 0;
  const lastOffset = itemOffsets.value[len - 1]!;
  const lastItem = flatList.value[len - 1]!;
  return lastOffset + lastItem.height;
});

const visibleItems = computed(() => {
  const container = listContainerRef.value;
  if (!container || flatList.value.length === 0) {
    return flatList.value.map((item, i) => ({ ...item, top: itemOffsets.value[i]! }));
  }

  const viewTop = scrollTop.value;
  const viewBottom = viewTop + container.clientHeight;
  const buffer = 200; // render extra items above/below

  const result: (FlatItem & { top: number })[] = [];
  for (let i = 0; i < flatList.value.length; i++) {
    const itemTop = itemOffsets.value[i]!;
    const item = flatList.value[i]!;
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

  const idx = flatList.value.findIndex(item => item.type === 'header' && item.letter === letter);
  if (idx >= 0) {
    container.scrollTop = itemOffsets.value[idx] ?? 0;
  }
};

// Contact actions
const selectContact = (contact: Contact) => {
  selectedContact.value = contact;
  if (isMobile.value) {
    showMobileDetail.value = true;
  }
};

const handleEdit = (contact: Contact) => {
  navigateTo(`/contacts/${contact.id}/edit`);
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
        const ab = contactsStore.addressBooks.find(ab => ab.UUID === contact.addressbook_id);
        if (!ab) throw new Error('Address book not found');
        await contactsStore.deleteContact(ab.ID, contact.id);
        if (selectedContact.value?.id === contact.id) {
          selectedContact.value = null;
          showMobileDetail.value = false;
        }
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
