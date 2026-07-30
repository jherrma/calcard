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
        // Diacritic-fold the first character so accented/umlaut names bucket
        // under their base Latin letter (Ärzte→A, Élan→E, Öztürk→O) instead of
        // falling into '#'. NFD splits e.g. 'Ä' into 'A' + combining mark, which
        // the \p{Diacritic} strip removes. Genuinely non-Latin first characters
        // (digits, CJK, emoji) still fail the A–Z test and land in '#'.
        const letter = name.charAt(0).normalize('NFD').replace(/\p{Diacritic}/gu, '').toUpperCase();
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

    ownedAddressBooks(state: ContactsState): AddressBook[] {
      return state.addressBooks.filter((ab: AddressBook) => !ab.shared);
    },

    sharedAddressBooks(state: ContactsState): AddressBook[] {
      return state.addressBooks.filter((ab: AddressBook) => ab.shared);
    },

    // Books the user may write to: everything they own, plus shares granted at
    // 'read-write' (#53). Every write control in the contacts UI keys off this —
    // offering an edit button for a read-only share just earns a 403. Mirrors
    // calendars.writableCalendars.
    writableAddressBooks(state: ContactsState): AddressBook[] {
      return state.addressBooks.filter(
        (ab: AddressBook) => !ab.shared || ab.permission === 'read-write'
      );
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
        const limit = 200; // backend maxPageLimit

        // Fetch every address book IN PARALLEL (was N sequential round-trips).
        // Each task pages through its OWN book (the backend defaults to limit=50,
        // so without paging the UI silently dropped contacts beyond the first 50
        // per book). allSettled keeps the continue-on-error behaviour: one failing
        // book doesn't block the others.
        const results = await Promise.allSettled(
          this.addressBooks.map(async (ab: AddressBook) => {
            const bookContacts: Contact[] = [];
            let offset = 0;
            for (;;) {
              const response = await api<{ Contacts: Contact[]; Total: number; Limit: number; Offset: number }>(
                `/api/v1/addressbooks/${ab.UUID}/contacts?limit=${limit}&offset=${offset}`
              );
              const page = response.Contacts || [];
              bookContacts.push(...page);
              offset += limit;
              if (page.length < limit || offset >= response.Total) break;
            }
            return bookContacts;
          })
        );

        const allContacts: Contact[] = [];
        results.forEach((r, i) => {
          if (r.status === 'fulfilled') {
            allContacts.push(...r.value);
          } else {
            console.warn(`Failed to load contacts for address book ${this.addressBooks[i]?.Name}`, r.reason);
          }
        });

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
        // Raise the search page size from the backend default (20) to its cap
        // (200) so search results aren't silently clipped to 20 matches.
        const response = await api<{ contacts: Contact[]; query: string; count: number }>(
          `/api/v1/contacts/search?q=${encodeURIComponent(query)}&limit=200`
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

    async createAddressBook(data: { name: string; description: string }) {
      const api = useApi();
      const response = await api<{ addressbook: AddressBook }>('/api/v1/addressbooks', {
        method: 'POST',
        body: data,
      });
      const ab = response.addressbook || response as unknown as AddressBook;
      this.addressBooks.push(ab);
      this.selectedAddressBookIds.add(ab.ID);
      return ab;
    },

    async updateAddressBook(id: number, data: { name?: string; description?: string }) {
      const api = useApi();
      const response = await api<AddressBook>(`/api/v1/addressbooks/${this.addressBookUuid(id)}`, {
        method: 'PATCH',
        body: data,
      });
      const idx = this.addressBooks.findIndex((ab: AddressBook) => ab.ID === id);
      if (idx >= 0) {
        this.addressBooks[idx] = response;
      }
      return response;
    },

    async deleteAddressBook(id: number) {
      const api = useApi();
      await api(`/api/v1/addressbooks/${this.addressBookUuid(id)}`, {
        method: 'DELETE',
        body: { confirmation: 'DELETE' },
      });
      this.addressBooks = this.addressBooks.filter((ab: AddressBook) => ab.ID !== id);
      this.selectedAddressBookIds.delete(id);
      this.contacts = this.contacts.filter((c: Contact) => c.addressbook_id !== String(id));
    },

    async deleteContact(addressBookId: number, contactId: string) {
      const api = useApi();
      await api(`/api/v1/addressbooks/${this.addressBookUuid(addressBookId)}/contacts/${contactId}`, {
        method: 'DELETE',
      });
      this.contacts = this.contacts.filter((c: Contact) => c.id !== contactId);
    },

    getAddressBookByNumericId(id: string): AddressBook | undefined {
      return this.addressBooks.find((ab: AddressBook) => String(ab.ID) === id);
    },

    // Whether the user may write to a specific book, by numeric id — the form
    // contacts carry in `addressbook_id`. Used to gate per-contact edit/delete
    // controls (#53). An unknown id is treated as writable so a not-yet-loaded
    // book doesn't silently disable the whole UI; the API is the real gate.
    canWriteAddressBook(id: string | number): boolean {
      const ab = this.addressBooks.find((b: AddressBook) => String(b.ID) === String(id));
      if (!ab) return true;
      return !ab.shared || ab.permission === 'read-write';
    },

    // Map an address book's numeric id (what contacts and the sidebar carry) to
    // its UUID, the canonical external identifier the API now expects on
    // /addressbooks/:id routes (#52). Callers still pass the numeric id; the
    // store resolves it here so there's a single translation point. Falls back
    // to the given value if the book isn't loaded (keeps the URL well-formed).
    addressBookUuid(id: string | number): string {
      return (
        this.addressBooks.find((ab: AddressBook) => String(ab.ID) === String(id))?.UUID ?? String(id)
      );
    },

    async getContact(abId: number, contactId: string): Promise<Contact> {
      const api = useApi();
      return await api<Contact>(`/api/v1/addressbooks/${this.addressBookUuid(abId)}/contacts/${contactId}`);
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
      const contact = await api<Contact>(`/api/v1/addressbooks/${this.addressBookUuid(abId)}/contacts`, {
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
      const updated = await api<Contact>(`/api/v1/addressbooks/${this.addressBookUuid(abId)}/contacts/${contactId}`, {
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
      const url = `${baseURL}/api/v1/addressbooks/${this.addressBookUuid(abId)}/contacts/${contactId}/photo`;

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
      await api(`/api/v1/addressbooks/${this.addressBookUuid(abId)}/contacts/${contactId}/photo`, {
        method: 'DELETE',
      });
    },
  },
});
