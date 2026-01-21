# Story 036: Contact List and Search UI

## Title
Implement Contact List View and Search

## Description
As a user, I want to view and search my contacts so that I can find and access contact information quickly.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AD-4.2.1 | Users can view list of contacts |
| AD-4.2.2 | Users can search contacts by name |
| AD-4.2.3 | Users can search contacts by email |
| AD-4.2.4 | Users can search contacts by phone |

## Acceptance Criteria

### Contacts Page

- [ ] Route: `/contacts`
- [ ] Address book sidebar (similar to calendar sidebar)
- [ ] Contact list in main area
- [ ] Search bar at top
- [ ] "Add Contact" button
- [ ] Responsive layout (sidebar collapses on mobile)

### Address Book Sidebar

- [ ] List of all address books (owned + shared)
- [ ] Checkbox to filter by address book
- [ ] Address book contact count
- [ ] "Add Address Book" button
- [ ] Address book actions menu (edit, share, delete)
- [ ] "All Contacts" option to show all

### Contact List

- [ ] Virtual scrolling for large lists
- [ ] Contact card shows:
  - [ ] Avatar (initials or photo)
  - [ ] Full name
  - [ ] Primary email
  - [ ] Primary phone
  - [ ] Organization (if set)
- [ ] Click to view contact details
- [ ] Hover actions (edit, delete)
- [ ] Empty state when no contacts
- [ ] Loading skeleton while fetching

### Search

- [ ] Search input with icon
- [ ] Debounced search (300ms)
- [ ] Search across:
  - [ ] Name (formatted, given, family)
  - [ ] Email addresses
  - [ ] Phone numbers
  - [ ] Organization
- [ ] Highlight matching text in results
- [ ] Clear search button
- [ ] "No results" state

### Sorting

- [ ] Sort dropdown: Name (A-Z), Name (Z-A), Recently updated
- [ ] Persist sort preference

### Alphabet Navigation

- [ ] Alphabet strip on right side (A-Z)
- [ ] Click letter to jump to contacts
- [ ] Highlight letters with contacts
- [ ] Sticky section headers (A, B, C...)

## Technical Notes

### Contacts Store
```typescript
// stores/contacts.ts
import { defineStore } from 'pinia';
import type { AddressBook, Contact } from '~/types';

interface ContactsState {
  addressBooks: AddressBook[];
  contacts: Contact[];
  selectedAddressBookIds: Set<string>;
  searchQuery: string;
  sortBy: 'name_asc' | 'name_desc' | 'updated';
  isLoading: boolean;
  error: string | null;
}

export const useContactsStore = defineStore('contacts', {
  state: (): ContactsState => ({
    addressBooks: [],
    contacts: [],
    selectedAddressBookIds: new Set(),
    searchQuery: '',
    sortBy: 'name_asc',
    isLoading: false,
    error: null,
  }),

  getters: {
    filteredContacts: (state) => {
      let contacts = state.contacts;

      // Filter by address book
      if (state.selectedAddressBookIds.size > 0) {
        contacts = contacts.filter(c =>
          state.selectedAddressBookIds.has(c.addressbook_id)
        );
      }

      // Sort
      contacts = [...contacts].sort((a, b) => {
        switch (state.sortBy) {
          case 'name_asc':
            return a.formatted_name.localeCompare(b.formatted_name);
          case 'name_desc':
            return b.formatted_name.localeCompare(a.formatted_name);
          case 'updated':
            return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
          default:
            return 0;
        }
      });

      return contacts;
    },

    groupedContacts: (state) => {
      const contacts = state.filteredContacts;
      const groups: Record<string, Contact[]> = {};

      for (const contact of contacts) {
        const letter = contact.formatted_name.charAt(0).toUpperCase();
        if (!groups[letter]) {
          groups[letter] = [];
        }
        groups[letter].push(contact);
      }

      return groups;
    },

    availableLetters: (state) => {
      return Object.keys(state.groupedContacts).sort();
    },
  },

  actions: {
    async fetchAddressBooks() {
      const api = useApi();
      const response = await api.get<{ addressbooks: AddressBook[] }>('/api/v1/addressbooks');
      this.addressBooks = response.addressbooks;

      // Initially show all
      this.selectedAddressBookIds = new Set(this.addressBooks.map(ab => ab.id));
    },

    async fetchContacts() {
      this.isLoading = true;
      this.error = null;

      try {
        const api = useApi();
        const allContacts: Contact[] = [];

        for (const ab of this.addressBooks) {
          const response = await api.get<{ contacts: Contact[] }>(
            `/api/v1/addressbooks/${ab.id}/contacts`
          );
          allContacts.push(...response.contacts.map(c => ({
            ...c,
            addressbook_id: ab.id,
          })));
        }

        this.contacts = allContacts;
      } catch (e: any) {
        this.error = e.message || 'Failed to load contacts';
      } finally {
        this.isLoading = false;
      }
    },

    async searchContacts(query: string) {
      if (!query.trim()) {
        await this.fetchContacts();
        return;
      }

      this.isLoading = true;
      this.error = null;

      try {
        const api = useApi();
        const response = await api.get<{ results: Contact[] }>(
          `/api/v1/contacts/search?q=${encodeURIComponent(query)}`
        );
        this.contacts = response.results;
      } catch (e: any) {
        this.error = e.message || 'Search failed';
      } finally {
        this.isLoading = false;
      }
    },

    toggleAddressBook(id: string) {
      if (this.selectedAddressBookIds.has(id)) {
        this.selectedAddressBookIds.delete(id);
      } else {
        this.selectedAddressBookIds.add(id);
      }
    },

    selectAllAddressBooks() {
      this.selectedAddressBookIds = new Set(this.addressBooks.map(ab => ab.id));
    },
  },
});
```

### Contacts Page
```vue
<!-- pages/contacts/index.vue -->
<template>
  <div class="flex h-[calc(100vh-8rem)]">
    <!-- Sidebar -->
    <ContactsSidebar
      :address-books="contactsStore.addressBooks"
      :selected-ids="contactsStore.selectedAddressBookIds"
      @toggle="contactsStore.toggleAddressBook"
      @select-all="contactsStore.selectAllAddressBooks"
      @add-addressbook="showAddAddressBookDialog = true"
    />

    <!-- Main content -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- Toolbar -->
      <div class="bg-white border-b p-4 flex items-center gap-4">
        <!-- Search -->
        <div class="flex-1 max-w-md relative">
          <i class="pi pi-search absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <InputText
            v-model="searchQuery"
            placeholder="Search contacts..."
            class="w-full pl-10"
            @input="debouncedSearch"
          />
          <button
            v-if="searchQuery"
            class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
            @click="clearSearch"
          >
            <i class="pi pi-times" />
          </button>
        </div>

        <!-- Sort -->
        <Dropdown
          v-model="contactsStore.sortBy"
          :options="sortOptions"
          option-label="label"
          option-value="value"
          class="w-40"
        />

        <!-- Add contact -->
        <Button
          label="Add Contact"
          icon="pi pi-plus"
          @click="router.push('/contacts/new')"
        />
      </div>

      <!-- Contact list -->
      <div class="flex-1 overflow-hidden flex">
        <div class="flex-1 overflow-y-auto p-4">
          <!-- Loading -->
          <SkeletonList v-if="contactsStore.isLoading" :count="10" />

          <!-- Error -->
          <div v-else-if="contactsStore.error" class="text-center py-12">
            <i class="pi pi-exclamation-circle text-4xl text-red-400" />
            <p class="mt-2 text-gray-600">{{ contactsStore.error }}</p>
            <Button
              label="Retry"
              severity="secondary"
              class="mt-4"
              @click="loadContacts"
            />
          </div>

          <!-- Empty state -->
          <div v-else-if="contactsStore.filteredContacts.length === 0" class="text-center py-12">
            <i class="pi pi-users text-4xl text-gray-300" />
            <p class="mt-2 text-gray-500">
              {{ searchQuery ? 'No contacts found' : 'No contacts yet' }}
            </p>
            <Button
              v-if="!searchQuery"
              label="Add your first contact"
              class="mt-4"
              @click="router.push('/contacts/new')"
            />
          </div>

          <!-- Grouped contacts -->
          <div v-else>
            <div
              v-for="(contacts, letter) in contactsStore.groupedContacts"
              :key="letter"
              :id="`section-${letter}`"
            >
              <div class="sticky top-0 bg-gray-100 px-2 py-1 text-sm font-semibold text-gray-600 z-10">
                {{ letter }}
              </div>
              <div class="space-y-1">
                <ContactListItem
                  v-for="contact in contacts"
                  :key="contact.id"
                  :contact="contact"
                  :highlight="searchQuery"
                  @click="selectContact(contact)"
                  @edit="editContact(contact)"
                  @delete="confirmDeleteContact(contact)"
                />
              </div>
            </div>
          </div>
        </div>

        <!-- Alphabet nav -->
        <AlphabetNavigation
          :letters="contactsStore.availableLetters"
          @select="scrollToLetter"
        />
      </div>
    </div>

    <!-- Contact detail panel (desktop) -->
    <ContactDetailPanel
      v-if="selectedContact && !isMobile"
      :contact="selectedContact"
      @close="selectedContact = null"
      @edit="editContact"
      @delete="confirmDeleteContact"
    />

    <!-- Contact detail dialog (mobile) -->
    <ContactDetailDialog
      v-if="isMobile"
      v-model:visible="showContactDetail"
      :contact="selectedContact"
      @edit="editContact"
      @delete="confirmDeleteContact"
    />

    <!-- Add address book dialog -->
    <AddAddressBookDialog
      v-model:visible="showAddAddressBookDialog"
      @created="onAddressBookCreated"
    />

    <!-- Delete confirmation -->
    <ConfirmDialog />
  </div>
</template>

<script setup lang="ts">
import { useDebounceFn, useMediaQuery } from '@vueuse/core';
import type { Contact } from '~/types';

definePageMeta({
  middleware: 'auth',
});

const contactsStore = useContactsStore();
const router = useRouter();
const confirm = useConfirm();
const toast = useAppToast();
const api = useApi();

const searchQuery = ref('');
const selectedContact = ref<Contact | null>(null);
const showContactDetail = ref(false);
const showAddAddressBookDialog = ref(false);

const isMobile = useMediaQuery('(max-width: 1024px)');

const sortOptions = [
  { label: 'Name (A-Z)', value: 'name_asc' },
  { label: 'Name (Z-A)', value: 'name_desc' },
  { label: 'Recently updated', value: 'updated' },
];

// Load data on mount
onMounted(async () => {
  await contactsStore.fetchAddressBooks();
  await contactsStore.fetchContacts();
});

// Debounced search
const debouncedSearch = useDebounceFn(() => {
  if (searchQuery.value.trim()) {
    contactsStore.searchContacts(searchQuery.value);
  } else {
    contactsStore.fetchContacts();
  }
}, 300);

const clearSearch = () => {
  searchQuery.value = '';
  contactsStore.fetchContacts();
};

const loadContacts = () => {
  contactsStore.fetchContacts();
};

const selectContact = (contact: Contact) => {
  selectedContact.value = contact;
  if (isMobile.value) {
    showContactDetail.value = true;
  }
};

const editContact = (contact: Contact) => {
  router.push(`/contacts/${contact.id}`);
};

const confirmDeleteContact = (contact: Contact) => {
  confirm.require({
    message: `Are you sure you want to delete ${contact.formatted_name}?`,
    header: 'Delete Contact',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => deleteContact(contact),
  });
};

const deleteContact = async (contact: Contact) => {
  try {
    await api.delete(`/api/v1/addressbooks/${contact.addressbook_id}/contacts/${contact.id}`);
    toast.success('Contact deleted');
    selectedContact.value = null;
    await contactsStore.fetchContacts();
  } catch {
    toast.error('Failed to delete contact');
  }
};

const scrollToLetter = (letter: string) => {
  const element = document.getElementById(`section-${letter}`);
  element?.scrollIntoView({ behavior: 'smooth' });
};

const onAddressBookCreated = () => {
  contactsStore.fetchAddressBooks();
};
</script>
```

### Contact List Item
```vue
<!-- components/contacts/ContactListItem.vue -->
<template>
  <div
    class="flex items-center gap-3 p-3 rounded-lg hover:bg-gray-50 cursor-pointer group"
    @click="$emit('click')"
  >
    <!-- Avatar -->
    <Avatar
      v-if="contact.has_photo"
      :image="photoUrl"
      shape="circle"
      size="large"
    />
    <Avatar
      v-else
      :label="initials"
      shape="circle"
      size="large"
      :style="{ backgroundColor: avatarColor }"
      class="text-white"
    />

    <!-- Info -->
    <div class="flex-1 min-w-0">
      <div class="font-medium text-gray-900 truncate">
        <HighlightText :text="contact.formatted_name" :highlight="highlight" />
      </div>
      <div v-if="contact.primary_email" class="text-sm text-gray-500 truncate">
        <HighlightText :text="contact.primary_email" :highlight="highlight" />
      </div>
      <div v-if="contact.organization" class="text-sm text-gray-400 truncate">
        {{ contact.organization }}
      </div>
    </div>

    <!-- Actions -->
    <div class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
      <Button
        icon="pi pi-pencil"
        severity="secondary"
        text
        rounded
        size="small"
        @click.stop="$emit('edit', contact)"
      />
      <Button
        icon="pi pi-trash"
        severity="danger"
        text
        rounded
        size="small"
        @click.stop="$emit('delete', contact)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Contact } from '~/types';

const props = defineProps<{
  contact: Contact;
  highlight?: string;
}>();

defineEmits<{
  click: [];
  edit: [contact: Contact];
  delete: [contact: Contact];
}>();

const config = useRuntimeConfig();

const photoUrl = computed(() => {
  return `${config.public.apiBaseUrl}/api/v1/addressbooks/${props.contact.addressbook_id}/contacts/${props.contact.id}/photo`;
});

const initials = computed(() => {
  const name = props.contact.formatted_name;
  const parts = name.split(' ');
  if (parts.length >= 2) {
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
  }
  return name.substring(0, 2).toUpperCase();
});

const avatarColor = computed(() => {
  // Generate consistent color from name
  let hash = 0;
  for (let i = 0; i < props.contact.formatted_name.length; i++) {
    hash = props.contact.formatted_name.charCodeAt(i) + ((hash << 5) - hash);
  }
  const hue = hash % 360;
  return `hsl(${hue}, 65%, 45%)`;
});
</script>
```

### Alphabet Navigation
```vue
<!-- components/contacts/AlphabetNavigation.vue -->
<template>
  <div class="w-6 flex flex-col items-center py-2 bg-gray-50 border-l">
    <button
      v-for="letter in alphabet"
      :key="letter"
      :class="[
        'text-xs py-0.5 w-full text-center',
        letters.includes(letter)
          ? 'text-primary-600 font-medium hover:bg-primary-50'
          : 'text-gray-300 cursor-default'
      ]"
      :disabled="!letters.includes(letter)"
      @click="$emit('select', letter)"
    >
      {{ letter }}
    </button>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  letters: string[];
}>();

defineEmits<{
  select: [letter: string];
}>();

const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('');
</script>
```

### Highlight Text Component
```vue
<!-- components/common/HighlightText.vue -->
<template>
  <span v-html="highlightedText" />
</template>

<script setup lang="ts">
const props = defineProps<{
  text: string;
  highlight?: string;
}>();

const highlightedText = computed(() => {
  if (!props.highlight || !props.text) {
    return props.text;
  }

  const regex = new RegExp(`(${escapeRegex(props.highlight)})`, 'gi');
  return props.text.replace(regex, '<mark class="bg-yellow-200">$1</mark>');
});

function escapeRegex(str: string) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
</script>
```

## Definition of Done

- [ ] Contacts page displays all contacts
- [ ] Address book sidebar with filtering
- [ ] Contact list with avatar, name, email
- [ ] Search by name, email, phone, organization
- [ ] Search results highlight matching text
- [ ] Debounced search (300ms)
- [ ] Sorting options work
- [ ] Alphabet navigation scrolls to section
- [ ] Click contact opens detail view
- [ ] Hover actions (edit, delete) visible
- [ ] Loading skeleton while fetching
- [ ] Empty state when no contacts
- [ ] Error state with retry button
- [ ] Responsive on mobile
