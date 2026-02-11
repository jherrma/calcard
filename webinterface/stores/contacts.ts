import type { AddressBook, Contact, ContactFormData } from '~/types/contacts';
import { useAuthStore } from '~/stores/auth';

interface ContactsState {
  addressBooks: AddressBook[];
  contacts: Contact[];
  selectedAddressBookIds: Set<number>;
  searchQuery: string;
  sortBy: 'first_name' | 'last_name';
  isLoading: boolean;
  error: string | null;
}

export const useContactsStore = defineStore('contacts', {
  state: (): ContactsState => ({
    addressBooks: [],
    contacts: [],
    selectedAddressBookIds: new Set(),
    searchQuery: '',
    sortBy: 'first_name',
    isLoading: false,
    error: null,
  }),

  getters: {
    filteredContacts(state: ContactsState): Contact[] {
      if (state.selectedAddressBookIds.size === 0) return [];
      return state.contacts.filter((c: Contact) =>
        state.selectedAddressBookIds.has(
          state.addressBooks.find((ab: AddressBook) => String(ab.ID) === c.addressbook_id)?.ID ?? -1
        )
      );
    },

    sortedContacts(): Contact[] {
      const filtered = [...this.filteredContacts];
      switch (this.sortBy) {
        case 'first_name':
          return filtered.sort((a: Contact, b: Contact) =>
            (a.given_name || a.formatted_name || '').localeCompare(b.given_name || b.formatted_name || '')
          );
        case 'last_name':
          return filtered.sort((a: Contact, b: Contact) =>
            (a.family_name || a.formatted_name || '').localeCompare(b.family_name || b.formatted_name || '')
          );
        default:
          return filtered;
      }
    },

    groupedContacts(): Map<string, Contact[]> {
      const groups = new Map<string, Contact[]>();
      for (const contact of this.sortedContacts) {
        const name = this.sortBy === 'last_name'
          ? (contact.family_name || contact.formatted_name || '?')
          : (contact.given_name || contact.formatted_name || '?');
        const letter = name.charAt(0).toUpperCase();
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

    getAddressBookByNumericId(id: string): AddressBook | undefined {
      return this.addressBooks.find((ab: AddressBook) => String(ab.ID) === id);
    },

    async getContact(abId: number, contactId: string): Promise<Contact> {
      const api = useApi();
      return await api<Contact>(`/api/v1/addressbooks/${abId}/contacts/${contactId}`);
    },

    buildFormattedName(data: ContactFormData): string {
      const parts = [data.prefix, data.given_name, data.middle_name, data.family_name, data.suffix]
        .map(s => s.trim())
        .filter(Boolean);
      if (parts.length > 0) return parts.join(' ');
      if (data.organization.trim()) return data.organization.trim();
      return 'Unnamed Contact';
    },

    async createContact(abId: number, data: ContactFormData): Promise<Contact> {
      const api = useApi();
      const payload = {
        ...data,
        formatted_name: this.buildFormattedName(data),
      };
      const contact = await api<Contact>(`/api/v1/addressbooks/${abId}/contacts`, {
        method: 'POST',
        body: payload,
      });
      this.contacts.push(contact);
      return contact;
    },

    async updateContact(abId: number, contactId: string, data: ContactFormData): Promise<Contact> {
      const api = useApi();
      const payload = {
        ...data,
        formatted_name: this.buildFormattedName(data),
      };
      const updated = await api<Contact>(`/api/v1/addressbooks/${abId}/contacts/${contactId}`, {
        method: 'PATCH',
        body: payload,
      });
      const idx = this.contacts.findIndex((c: Contact) => c.id === contactId);
      if (idx >= 0) {
        this.contacts[idx] = updated;
      }
      return updated;
    },

    async uploadPhoto(abId: number, contactId: string, file: File) {
      const config = useRuntimeConfig();
      const authStore = useAuthStore();
      const baseURL = (config.public.apiBaseUrl as string) || '';
      const url = `${baseURL}/api/v1/addressbooks/${abId}/contacts/${contactId}/photo`;

      await $fetch(url, {
        method: 'PUT',
        body: file,
        headers: {
          'Content-Type': file.type,
          ...(authStore.accessToken ? { Authorization: `Bearer ${authStore.accessToken}` } : {}),
        },
      });
    },

    async deletePhoto(abId: number, contactId: string) {
      const api = useApi();
      await api(`/api/v1/addressbooks/${abId}/contacts/${contactId}/photo`, {
        method: 'DELETE',
      });
    },
  },
});
