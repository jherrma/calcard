# Story 037: Contact Management UI

## Title
Implement Contact Create, Edit, and Delete UI

## Description
As a user, I want to create, edit, and delete contacts through the web interface so that I can manage my address books.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AD-4.2.5 | Users can create contacts with name |
| AD-4.2.6 | Users can add multiple email addresses to contact |
| AD-4.2.7 | Users can add multiple phone numbers with types |
| AD-4.2.8 | Users can add postal addresses |
| AD-4.2.9 | Users can add organization/company |
| AD-4.2.10 | Users can add notes to contacts |
| AD-4.2.11 | Users can edit existing contacts |
| AD-4.2.12 | Users can delete contacts |
| AD-4.2.13 | Users can add contact photo |

## Acceptance Criteria

### Contact Detail Panel

- [ ] Shows on right side when contact selected (desktop)
- [ ] Full page on mobile
- [ ] Displays:
  - [ ] Photo or avatar
  - [ ] Full name
  - [ ] All email addresses with labels
  - [ ] All phone numbers with labels
  - [ ] All addresses with labels
  - [ ] Organization and title
  - [ ] Birthday
  - [ ] Notes
  - [ ] URLs
- [ ] Edit and Delete buttons
- [ ] Clickable emails (mailto:)
- [ ] Clickable phones (tel:)
- [ ] Clickable addresses (maps link)

### Create Contact Page

- [ ] Route: `/contacts/new`
- [ ] Form fields:
  - [ ] Photo upload
  - [ ] Name fields (prefix, first, middle, last, suffix)
  - [ ] Nickname
  - [ ] Email addresses (multiple, with type)
  - [ ] Phone numbers (multiple, with type)
  - [ ] Addresses (multiple, with type)
  - [ ] Organization and title
  - [ ] Birthday
  - [ ] URLs (multiple, with type)
  - [ ] Notes
- [ ] Address book selector
- [ ] Save and Cancel buttons
- [ ] Validation (at least one name field required)

### Edit Contact Page

- [ ] Route: `/contacts/{id}`
- [ ] Pre-filled form with existing data
- [ ] Same fields as create
- [ ] Save, Cancel, Delete buttons

### Multi-value Fields

- [ ] Add button to add more emails/phones/addresses
- [ ] Remove button on each item
- [ ] Type selector (Work, Home, Mobile, etc.)
- [ ] Primary indicator (star icon)
- [ ] Reorder by drag (optional)

### Photo Upload

- [ ] Click avatar to upload
- [ ] Accept JPEG, PNG, GIF
- [ ] Max size: 1MB
- [ ] Crop/resize preview (optional)
- [ ] Remove photo option

### Delete Contact

- [ ] Confirmation dialog
- [ ] Success message
- [ ] Redirect to contact list

## Technical Notes

### Contact Form Component
```vue
<!-- components/contacts/ContactForm.vue -->
<template>
  <form @submit.prevent="handleSubmit" class="space-y-6">
    <!-- Photo -->
    <div class="flex items-center gap-4">
      <div class="relative">
        <Avatar
          v-if="photoPreview || contact?.has_photo"
          :image="photoPreview || existingPhotoUrl"
          shape="circle"
          size="xlarge"
          class="w-24 h-24"
        />
        <Avatar
          v-else
          :label="initials"
          shape="circle"
          size="xlarge"
          class="w-24 h-24 bg-primary-500 text-white text-2xl"
        />
        <label
          class="absolute bottom-0 right-0 bg-white rounded-full p-2 shadow cursor-pointer hover:bg-gray-50"
        >
          <i class="pi pi-camera text-gray-600" />
          <input
            type="file"
            accept="image/jpeg,image/png,image/gif"
            class="hidden"
            @change="handlePhotoChange"
          />
        </label>
      </div>
      <Button
        v-if="photoPreview || contact?.has_photo"
        label="Remove photo"
        severity="secondary"
        text
        size="small"
        @click="removePhoto"
      />
    </div>

    <!-- Address Book -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        Address Book
      </label>
      <Dropdown
        v-model="form.addressbook_id"
        :options="addressBooks"
        option-label="name"
        option-value="id"
        class="w-full"
        :disabled="isEditing"
      />
    </div>

    <!-- Name -->
    <fieldset class="border rounded-lg p-4">
      <legend class="text-sm font-medium text-gray-700 px-2">Name</legend>
      <div class="grid grid-cols-2 md:grid-cols-3 gap-4">
        <div>
          <label class="block text-xs text-gray-500 mb-1">Prefix</label>
          <InputText v-model="form.prefix" class="w-full" placeholder="Dr." />
        </div>
        <div>
          <label class="block text-xs text-gray-500 mb-1">First name</label>
          <InputText v-model="form.given_name" class="w-full" />
        </div>
        <div>
          <label class="block text-xs text-gray-500 mb-1">Middle name</label>
          <InputText v-model="form.middle_name" class="w-full" />
        </div>
        <div>
          <label class="block text-xs text-gray-500 mb-1">Last name</label>
          <InputText v-model="form.family_name" class="w-full" />
        </div>
        <div>
          <label class="block text-xs text-gray-500 mb-1">Suffix</label>
          <InputText v-model="form.suffix" class="w-full" placeholder="Jr." />
        </div>
        <div>
          <label class="block text-xs text-gray-500 mb-1">Nickname</label>
          <InputText v-model="form.nickname" class="w-full" />
        </div>
      </div>
      <small v-if="errors.name" class="p-error mt-2 block">{{ errors.name }}</small>
    </fieldset>

    <!-- Emails -->
    <fieldset class="border rounded-lg p-4">
      <legend class="text-sm font-medium text-gray-700 px-2">Email Addresses</legend>
      <div class="space-y-3">
        <div
          v-for="(email, index) in form.emails"
          :key="index"
          class="flex items-center gap-2"
        >
          <Dropdown
            v-model="email.type"
            :options="emailTypes"
            class="w-28"
          />
          <InputText
            v-model="email.value"
            type="email"
            class="flex-1"
            placeholder="email@example.com"
          />
          <Button
            :icon="email.primary ? 'pi pi-star-fill' : 'pi pi-star'"
            :severity="email.primary ? 'warning' : 'secondary'"
            text
            rounded
            @click="setPrimaryEmail(index)"
          />
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            @click="removeEmail(index)"
            :disabled="form.emails.length === 1"
          />
        </div>
      </div>
      <Button
        label="Add email"
        icon="pi pi-plus"
        severity="secondary"
        text
        size="small"
        class="mt-2"
        @click="addEmail"
      />
    </fieldset>

    <!-- Phones -->
    <fieldset class="border rounded-lg p-4">
      <legend class="text-sm font-medium text-gray-700 px-2">Phone Numbers</legend>
      <div class="space-y-3">
        <div
          v-for="(phone, index) in form.phones"
          :key="index"
          class="flex items-center gap-2"
        >
          <Dropdown
            v-model="phone.type"
            :options="phoneTypes"
            class="w-28"
          />
          <InputText
            v-model="phone.value"
            type="tel"
            class="flex-1"
            placeholder="+1-555-123-4567"
          />
          <Button
            :icon="phone.primary ? 'pi pi-star-fill' : 'pi pi-star'"
            :severity="phone.primary ? 'warning' : 'secondary'"
            text
            rounded
            @click="setPrimaryPhone(index)"
          />
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            @click="removePhone(index)"
          />
        </div>
      </div>
      <Button
        label="Add phone"
        icon="pi pi-plus"
        severity="secondary"
        text
        size="small"
        class="mt-2"
        @click="addPhone"
      />
    </fieldset>

    <!-- Addresses -->
    <fieldset class="border rounded-lg p-4">
      <legend class="text-sm font-medium text-gray-700 px-2">Addresses</legend>
      <div class="space-y-4">
        <div
          v-for="(address, index) in form.addresses"
          :key="index"
          class="border rounded p-3 relative"
        >
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            size="small"
            class="absolute top-2 right-2"
            @click="removeAddress(index)"
          />
          <div class="grid grid-cols-2 gap-3">
            <div class="col-span-2">
              <Dropdown
                v-model="address.type"
                :options="addressTypes"
                class="w-32"
              />
            </div>
            <div class="col-span-2">
              <InputText
                v-model="address.street"
                class="w-full"
                placeholder="Street address"
              />
            </div>
            <div>
              <InputText
                v-model="address.city"
                class="w-full"
                placeholder="City"
              />
            </div>
            <div>
              <InputText
                v-model="address.state"
                class="w-full"
                placeholder="State/Province"
              />
            </div>
            <div>
              <InputText
                v-model="address.postal_code"
                class="w-full"
                placeholder="Postal code"
              />
            </div>
            <div>
              <InputText
                v-model="address.country"
                class="w-full"
                placeholder="Country"
              />
            </div>
          </div>
        </div>
      </div>
      <Button
        label="Add address"
        icon="pi pi-plus"
        severity="secondary"
        text
        size="small"
        class="mt-2"
        @click="addAddress"
      />
    </fieldset>

    <!-- Organization -->
    <fieldset class="border rounded-lg p-4">
      <legend class="text-sm font-medium text-gray-700 px-2">Work</legend>
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="block text-xs text-gray-500 mb-1">Organization</label>
          <InputText v-model="form.organization" class="w-full" />
        </div>
        <div>
          <label class="block text-xs text-gray-500 mb-1">Title</label>
          <InputText v-model="form.title" class="w-full" />
        </div>
      </div>
    </fieldset>

    <!-- Birthday -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">Birthday</label>
      <Calendar
        v-model="form.birthday"
        date-format="yy-mm-dd"
        :show-icon="true"
        class="w-full md:w-64"
      />
    </div>

    <!-- Notes -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">Notes</label>
      <Textarea
        v-model="form.notes"
        rows="4"
        class="w-full"
        placeholder="Add notes..."
      />
    </div>

    <!-- Actions -->
    <div class="flex justify-between pt-4 border-t">
      <Button
        v-if="isEditing"
        label="Delete"
        icon="pi pi-trash"
        severity="danger"
        text
        @click="$emit('delete')"
      />
      <div v-else />
      <div class="flex gap-2">
        <Button
          label="Cancel"
          severity="secondary"
          @click="$emit('cancel')"
        />
        <Button
          :label="isEditing ? 'Save' : 'Create'"
          :loading="isSubmitting"
          type="submit"
        />
      </div>
    </div>
  </form>
</template>

<script setup lang="ts">
import type { Contact, ContactFormData } from '~/types';

const props = defineProps<{
  contact?: Contact;
}>();

const emit = defineEmits<{
  submit: [data: ContactFormData, photo?: File];
  cancel: [];
  delete: [];
}>();

const contactsStore = useContactsStore();
const config = useRuntimeConfig();

const isEditing = computed(() => !!props.contact);
const isSubmitting = ref(false);
const errors = reactive<Record<string, string>>({});

const photoFile = ref<File | null>(null);
const photoPreview = ref<string | null>(null);
const removeExistingPhoto = ref(false);

// Form state
const form = reactive<ContactFormData>({
  addressbook_id: props.contact?.addressbook_id || contactsStore.addressBooks[0]?.id || '',
  prefix: props.contact?.prefix || '',
  given_name: props.contact?.given_name || '',
  middle_name: props.contact?.middle_name || '',
  family_name: props.contact?.family_name || '',
  suffix: props.contact?.suffix || '',
  nickname: props.contact?.nickname || '',
  emails: props.contact?.emails?.length
    ? [...props.contact.emails]
    : [{ type: 'home', value: '', primary: true }],
  phones: props.contact?.phones?.length
    ? [...props.contact.phones]
    : [],
  addresses: props.contact?.addresses?.length
    ? [...props.contact.addresses]
    : [],
  organization: props.contact?.organization || '',
  title: props.contact?.title || '',
  birthday: props.contact?.birthday ? new Date(props.contact.birthday) : null,
  notes: props.contact?.notes || '',
  urls: props.contact?.urls || [],
});

const addressBooks = computed(() => {
  return contactsStore.addressBooks.filter(ab => !ab.shared || ab.permission === 'read-write');
});

const emailTypes = ['home', 'work', 'other'];
const phoneTypes = ['cell', 'home', 'work', 'fax', 'other'];
const addressTypes = ['home', 'work', 'other'];

const existingPhotoUrl = computed(() => {
  if (!props.contact?.has_photo || removeExistingPhoto.value) return null;
  return `${config.public.apiBaseUrl}/api/v1/addressbooks/${props.contact.addressbook_id}/contacts/${props.contact.id}/photo`;
});

const initials = computed(() => {
  const name = form.given_name || form.family_name || form.organization || 'C';
  return name.substring(0, 2).toUpperCase();
});

// Photo handling
const handlePhotoChange = (event: Event) => {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;

  if (file.size > 1024 * 1024) {
    alert('Photo must be less than 1MB');
    return;
  }

  photoFile.value = file;
  photoPreview.value = URL.createObjectURL(file);
  removeExistingPhoto.value = false;
};

const removePhoto = () => {
  photoFile.value = null;
  photoPreview.value = null;
  removeExistingPhoto.value = true;
};

// Multi-value field handlers
const addEmail = () => {
  form.emails.push({ type: 'work', value: '', primary: false });
};

const removeEmail = (index: number) => {
  const wasPrimary = form.emails[index].primary;
  form.emails.splice(index, 1);
  if (wasPrimary && form.emails.length > 0) {
    form.emails[0].primary = true;
  }
};

const setPrimaryEmail = (index: number) => {
  form.emails.forEach((e, i) => e.primary = i === index);
};

const addPhone = () => {
  form.phones.push({ type: 'cell', value: '', primary: form.phones.length === 0 });
};

const removePhone = (index: number) => {
  const wasPrimary = form.phones[index].primary;
  form.phones.splice(index, 1);
  if (wasPrimary && form.phones.length > 0) {
    form.phones[0].primary = true;
  }
};

const setPrimaryPhone = (index: number) => {
  form.phones.forEach((p, i) => p.primary = i === index);
};

const addAddress = () => {
  form.addresses.push({
    type: 'home',
    street: '',
    city: '',
    state: '',
    postal_code: '',
    country: '',
  });
};

const removeAddress = (index: number) => {
  form.addresses.splice(index, 1);
};

// Validation
const validate = () => {
  errors.name = '';

  const hasName = form.given_name || form.family_name || form.organization;
  if (!hasName) {
    errors.name = 'At least one name field is required (first name, last name, or organization)';
    return false;
  }

  return true;
};

// Submit
const handleSubmit = () => {
  if (!validate()) return;

  isSubmitting.value = true;

  // Clean up empty values
  const data: ContactFormData = {
    ...form,
    emails: form.emails.filter(e => e.value.trim()),
    phones: form.phones.filter(p => p.value.trim()),
    addresses: form.addresses.filter(a => a.street || a.city),
  };

  emit('submit', data, photoFile.value || undefined);
};
</script>
```

### Contact Detail Panel
```vue
<!-- components/contacts/ContactDetailPanel.vue -->
<template>
  <aside class="w-96 bg-white border-l flex flex-col">
    <!-- Header -->
    <div class="p-4 border-b flex items-center justify-between">
      <h2 class="font-semibold">Contact Details</h2>
      <Button
        icon="pi pi-times"
        severity="secondary"
        text
        rounded
        @click="$emit('close')"
      />
    </div>

    <!-- Content -->
    <div class="flex-1 overflow-y-auto p-4">
      <!-- Photo and name -->
      <div class="text-center mb-6">
        <Avatar
          v-if="contact.has_photo"
          :image="photoUrl"
          shape="circle"
          size="xlarge"
          class="w-24 h-24 mx-auto"
        />
        <Avatar
          v-else
          :label="initials"
          shape="circle"
          size="xlarge"
          class="w-24 h-24 mx-auto text-2xl"
          :style="{ backgroundColor: avatarColor }"
        />
        <h3 class="mt-3 text-xl font-semibold">{{ contact.formatted_name }}</h3>
        <p v-if="contact.organization" class="text-gray-500">
          {{ contact.title ? `${contact.title} at ` : '' }}{{ contact.organization }}
        </p>
      </div>

      <!-- Emails -->
      <div v-if="contact.emails?.length" class="mb-4">
        <h4 class="text-sm font-medium text-gray-500 mb-2">Email</h4>
        <div class="space-y-2">
          <a
            v-for="email in contact.emails"
            :key="email.value"
            :href="`mailto:${email.value}`"
            class="flex items-center gap-2 text-primary-600 hover:underline"
          >
            <i class="pi pi-envelope text-gray-400" />
            <span>{{ email.value }}</span>
            <span class="text-xs text-gray-400">({{ email.type }})</span>
          </a>
        </div>
      </div>

      <!-- Phones -->
      <div v-if="contact.phones?.length" class="mb-4">
        <h4 class="text-sm font-medium text-gray-500 mb-2">Phone</h4>
        <div class="space-y-2">
          <a
            v-for="phone in contact.phones"
            :key="phone.value"
            :href="`tel:${phone.value}`"
            class="flex items-center gap-2 text-primary-600 hover:underline"
          >
            <i class="pi pi-phone text-gray-400" />
            <span>{{ phone.value }}</span>
            <span class="text-xs text-gray-400">({{ phone.type }})</span>
          </a>
        </div>
      </div>

      <!-- Addresses -->
      <div v-if="contact.addresses?.length" class="mb-4">
        <h4 class="text-sm font-medium text-gray-500 mb-2">Address</h4>
        <div class="space-y-3">
          <div v-for="(address, index) in contact.addresses" :key="index">
            <a
              :href="getMapsUrl(address)"
              target="_blank"
              class="text-sm hover:text-primary-600"
            >
              <div>{{ address.street }}</div>
              <div>{{ address.city }}, {{ address.state }} {{ address.postal_code }}</div>
              <div>{{ address.country }}</div>
            </a>
            <span class="text-xs text-gray-400">({{ address.type }})</span>
          </div>
        </div>
      </div>

      <!-- Birthday -->
      <div v-if="contact.birthday" class="mb-4">
        <h4 class="text-sm font-medium text-gray-500 mb-2">Birthday</h4>
        <div class="flex items-center gap-2">
          <i class="pi pi-gift text-gray-400" />
          <span>{{ formatDate(contact.birthday) }}</span>
        </div>
      </div>

      <!-- Notes -->
      <div v-if="contact.notes" class="mb-4">
        <h4 class="text-sm font-medium text-gray-500 mb-2">Notes</h4>
        <p class="text-sm whitespace-pre-wrap">{{ contact.notes }}</p>
      </div>
    </div>

    <!-- Actions -->
    <div class="p-4 border-t flex gap-2">
      <Button
        label="Edit"
        icon="pi pi-pencil"
        class="flex-1"
        @click="$emit('edit', contact)"
      />
      <Button
        icon="pi pi-trash"
        severity="danger"
        outlined
        @click="$emit('delete', contact)"
      />
    </div>
  </aside>
</template>

<script setup lang="ts">
import type { Contact, Address } from '~/types';

const props = defineProps<{
  contact: Contact;
}>();

defineEmits<{
  close: [];
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
  let hash = 0;
  for (let i = 0; i < props.contact.formatted_name.length; i++) {
    hash = props.contact.formatted_name.charCodeAt(i) + ((hash << 5) - hash);
  }
  return `hsl(${hash % 360}, 65%, 45%)`;
});

const formatDate = (dateStr: string) => {
  return new Date(dateStr).toLocaleDateString('en-US', {
    month: 'long',
    day: 'numeric',
    year: 'numeric',
  });
};

const getMapsUrl = (address: Address) => {
  const query = [address.street, address.city, address.state, address.postal_code, address.country]
    .filter(Boolean)
    .join(', ');
  return `https://maps.google.com/?q=${encodeURIComponent(query)}`;
};
</script>
```

### Create/Edit Contact Page
```vue
<!-- pages/contacts/[id].vue -->
<template>
  <div class="max-w-3xl mx-auto">
    <div class="bg-white rounded-lg shadow p-6">
      <h1 class="text-2xl font-semibold mb-6">
        {{ isNew ? 'Create Contact' : 'Edit Contact' }}
      </h1>

      <LoadingSpinner v-if="isLoading" />

      <ContactForm
        v-else
        :contact="contact"
        @submit="handleSubmit"
        @cancel="router.back()"
        @delete="confirmDelete"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Contact, ContactFormData } from '~/types';

definePageMeta({
  middleware: 'auth',
});

const route = useRoute();
const router = useRouter();
const toast = useAppToast();
const confirm = useConfirm();
const api = useApi();

const isNew = computed(() => route.params.id === 'new');
const isLoading = ref(!isNew.value);
const contact = ref<Contact | undefined>();

// Fetch contact if editing
onMounted(async () => {
  if (!isNew.value) {
    try {
      // Need to find which address book the contact belongs to
      // This might require a different API endpoint
      const response = await api.get<Contact>(`/api/v1/contacts/${route.params.id}`);
      contact.value = response;
    } catch {
      toast.error('Contact not found');
      router.push('/contacts');
    } finally {
      isLoading.value = false;
    }
  }
});

const handleSubmit = async (data: ContactFormData, photo?: File) => {
  try {
    if (isNew.value) {
      const response = await api.post<Contact>(
        `/api/v1/addressbooks/${data.addressbook_id}/contacts`,
        data
      );

      if (photo) {
        await uploadPhoto(data.addressbook_id, response.id, photo);
      }

      toast.success('Contact created');
    } else {
      await api.patch(
        `/api/v1/addressbooks/${contact.value!.addressbook_id}/contacts/${contact.value!.id}`,
        data
      );

      if (photo) {
        await uploadPhoto(contact.value!.addressbook_id, contact.value!.id, photo);
      }

      toast.success('Contact updated');
    }

    router.push('/contacts');
  } catch (e: any) {
    toast.error(e.message || 'Failed to save contact');
  }
};

const uploadPhoto = async (addressbookId: string, contactId: string, photo: File) => {
  const formData = new FormData();
  formData.append('photo', photo);

  await $fetch(`/api/v1/addressbooks/${addressbookId}/contacts/${contactId}/photo`, {
    method: 'PUT',
    body: formData,
  });
};

const confirmDelete = () => {
  confirm.require({
    message: 'Are you sure you want to delete this contact?',
    header: 'Delete Contact',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: deleteContact,
  });
};

const deleteContact = async () => {
  try {
    await api.delete(
      `/api/v1/addressbooks/${contact.value!.addressbook_id}/contacts/${contact.value!.id}`
    );
    toast.success('Contact deleted');
    router.push('/contacts');
  } catch {
    toast.error('Failed to delete contact');
  }
};
</script>
```

## Definition of Done

- [ ] Contact detail panel shows all fields
- [ ] Create contact form with all fields
- [ ] Edit contact form pre-fills data
- [ ] Multiple emails with type selector
- [ ] Multiple phones with type selector
- [ ] Multiple addresses with all fields
- [ ] Primary indicator for emails/phones
- [ ] Photo upload works
- [ ] Photo preview shown
- [ ] Remove photo option
- [ ] Delete contact with confirmation
- [ ] Form validation works
- [ ] Clickable emails (mailto:)
- [ ] Clickable phones (tel:)
- [ ] Clickable addresses (maps)
- [ ] Loading states displayed
- [ ] Success/error toasts
