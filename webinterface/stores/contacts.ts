import type { AddressBook, Contact } from '~/types/contacts';

interface ContactsState {
  addressBooks: AddressBook[];
  contacts: Contact[];
  selectedAddressBookIds: Set<number>;
  searchQuery: string;
  sortBy: 'name' | 'organization' | 'email' | 'updated';
  isLoading: boolean;
  error: string | null;
}

export const useContactsStore = defineStore('contacts', {
  state: (): ContactsState => ({
    addressBooks: [],
    contacts: [],
    selectedAddressBookIds: new Set(),
    searchQuery: '',
    sortBy: 'name',
    isLoading: false,
    error: null,
  }),

  getters: {
    filteredContacts(state: ContactsState): Contact[] {
      if (state.selectedAddressBookIds.size === 0) return [];
      return state.contacts.filter((c: Contact) =>
        state.selectedAddressBookIds.has(
          state.addressBooks.find((ab: AddressBook) => ab.UUID === c.addressbook_id)?.ID ?? -1
        )
      );
    },

    sortedContacts(): Contact[] {
      const filtered = [...this.filteredContacts];
      switch (this.sortBy) {
        case 'name':
          return filtered.sort((a: Contact, b: Contact) =>
            (a.formatted_name || '').localeCompare(b.formatted_name || '')
          );
        case 'organization':
          return filtered.sort((a: Contact, b: Contact) =>
            (a.organization || '').localeCompare(b.organization || '')
          );
        case 'email': {
          const primaryEmail = (c: Contact) => c.emails?.find(e => e.primary)?.value || c.emails?.[0]?.value || '';
          return filtered.sort((a: Contact, b: Contact) => primaryEmail(a).localeCompare(primaryEmail(b)));
        }
        case 'updated':
          return filtered.sort((a: Contact, b: Contact) =>
            new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
          );
        default:
          return filtered;
      }
    },

    groupedContacts(): Map<string, Contact[]> {
      const groups = new Map<string, Contact[]>();
      for (const contact of this.sortedContacts) {
        const letter = (contact.formatted_name || '?').charAt(0).toUpperCase();
        const key = /[A-Z]/.test(letter) ? letter : '#';
        if (!groups.has(key)) {
          groups.set(key, []);
        }
        groups.get(key)!.push(contact);
      }
      return groups;
    },

    availableLetters(): string[] {
      return Array.from(this.groupedContacts.keys()).sort();
    },
  },

  actions: {
    async fetchAddressBooks() {
      const api = useApi();
      try {
        const response = await api<{ addressbooks: AddressBook[] }>('/api/v1/addressbooks');
        this.addressBooks = response.addressbooks || [];
        // Initially select all address books
        this.selectedAddressBookIds = new Set(this.addressBooks.map((ab: AddressBook) => ab.ID));
      } catch (e: unknown) {
        this.error = (e as Error).message || 'Failed to load address books';
      }
    },

    async fetchContacts() {
      this.isLoading = true;
      this.error = null;

      try {
        const api = useApi();
        const allContacts: Contact[] = [];

        for (const ab of this.addressBooks) {
          try {
            const response = await api<{ Contacts: Contact[]; Total: number; Limit: number; Offset: number }>(
              `/api/v1/addressbooks/${ab.ID}/contacts`
            );
            if (response.Contacts) {
              allContacts.push(...response.Contacts);
            }
          } catch (e) {
            console.warn(`Failed to load contacts for address book ${ab.Name}`, e);
          }
        }

        this.contacts = allContacts;
      } catch (e: unknown) {
        this.error = (e as Error).message || 'Failed to load contacts';
      } finally {
        this.isLoading = false;
      }
    },

    async searchContacts(query: string) {
      if (!query.trim()) {
        this.searchQuery = '';
        await this.fetchContacts();
        return;
      }

      this.isLoading = true;
      this.error = null;
      this.searchQuery = query;

      try {
        const api = useApi();
        const response = await api<{ contacts: Contact[]; query: string; count: number }>(
          `/api/v1/contacts/search?q=${encodeURIComponent(query)}`
        );
        this.contacts = response.contacts || [];
      } catch (e: unknown) {
        this.error = (e as Error).message || 'Failed to search contacts';
      } finally {
        this.isLoading = false;
      }
    },

    toggleAddressBook(id: number) {
      if (this.selectedAddressBookIds.has(id)) {
        this.selectedAddressBookIds.delete(id);
      } else {
        this.selectedAddressBookIds.add(id);
      }
    },

    selectAllAddressBooks() {
      this.selectedAddressBookIds = new Set(this.addressBooks.map((ab: AddressBook) => ab.ID));
    },

    async deleteContact(addressBookId: number, contactId: string) {
      const api = useApi();
      await api(`/api/v1/addressbooks/${addressBookId}/contacts/${contactId}`, {
        method: 'DELETE',
      });
      this.contacts = this.contacts.filter((c: Contact) => c.id !== contactId);
    },
  },
});
