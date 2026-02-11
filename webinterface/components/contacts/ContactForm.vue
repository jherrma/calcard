<template>
  <form class="flex flex-col gap-6" @submit.prevent="handleSubmit">
    <!-- Photo -->
    <div class="flex flex-col items-center gap-2">
      <div
        class="w-24 h-24 rounded-full flex items-center justify-center text-white font-bold text-3xl cursor-pointer relative overflow-hidden group"
        :style="{ backgroundColor: photoPreview ? 'transparent' : '#3b82f6' }"
        @click="triggerPhotoSelect"
      >
        <img
          v-if="photoPreview"
          :src="photoPreview"
          alt="Photo"
          class="w-full h-full object-cover"
        >
        <span v-else>{{ initials }}</span>
        <div class="absolute inset-0 bg-black/40 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
          <i class="pi pi-camera text-white text-xl" />
        </div>
      </div>
      <div class="flex gap-2">
        <Button
          v-if="photoPreview"
          label="Remove Photo"
          icon="pi pi-times"
          text
          size="small"
          severity="danger"
          @click="removePhoto"
        />
      </div>
      <input
        ref="fileInputRef"
        type="file"
        accept="image/*"
        class="hidden"
        @change="onFileSelected"
      >
    </div>

    <!-- Address Book selector -->
    <div class="flex flex-col gap-1">
      <label class="text-sm font-medium text-surface-700 dark:text-surface-300">Address Book</label>
      <Select
        v-model="selectedAddressBookId"
        :options="addressBooks"
        option-label="Name"
        option-value="ID"
        placeholder="Select address book"
        :disabled="!!contact"
        :invalid="!!errors.addressBook"
      />
      <small v-if="errors.addressBook" class="text-red-500">{{ errors.addressBook }}</small>
    </div>

    <!-- Name fields -->
    <fieldset class="flex flex-col gap-3">
      <legend class="text-sm font-semibold text-surface-700 dark:text-surface-300 mb-1">Name</legend>
      <div class="grid grid-cols-2 gap-3">
        <div class="flex flex-col gap-1">
          <label class="text-xs text-surface-500">Prefix</label>
          <InputText v-model="form.prefix" placeholder="Mr., Dr." />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs text-surface-500">Suffix</label>
          <InputText v-model="form.suffix" placeholder="Jr., III" />
        </div>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div class="flex flex-col gap-1">
          <label class="text-xs text-surface-500">First Name</label>
          <InputText v-model="form.given_name" placeholder="First name" :invalid="!!errors.name" />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs text-surface-500">Last Name</label>
          <InputText v-model="form.family_name" placeholder="Last name" :invalid="!!errors.name" />
        </div>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div class="flex flex-col gap-1">
          <label class="text-xs text-surface-500">Middle Name</label>
          <InputText v-model="form.middle_name" placeholder="Middle name" />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-xs text-surface-500">Nickname</label>
          <InputText v-model="form.nickname" placeholder="Nickname" />
        </div>
      </div>
      <small v-if="errors.name" class="text-red-500">{{ errors.name }}</small>
    </fieldset>

    <!-- Emails -->
    <fieldset class="flex flex-col gap-2">
      <legend class="text-sm font-semibold text-surface-700 dark:text-surface-300 mb-1">Email Addresses</legend>
      <div
        v-for="(email, i) in form.emails"
        :key="i"
        class="flex items-center gap-2"
      >
        <Select
          v-model="email.type"
          :options="emailTypes"
          class="w-28 flex-shrink-0"
        />
        <InputText
          v-model="email.value"
          placeholder="email@example.com"
          class="flex-1"
        />
        <button
          type="button"
          class="p-2 rounded-lg text-surface-400 hover:text-yellow-500"
          :class="{ 'text-yellow-500': email.primary }"
          title="Set as primary"
          @click="setPrimaryEmail(i)"
        >
          <i class="pi" :class="email.primary ? 'pi-star-fill' : 'pi-star'" />
        </button>
        <button
          type="button"
          class="p-2 rounded-lg text-surface-400 hover:text-red-500"
          @click="form.emails.splice(i, 1)"
        >
          <i class="pi pi-minus-circle" />
        </button>
      </div>
      <Button
        type="button"
        label="Add Email"
        icon="pi pi-plus"
        text
        size="small"
        @click="form.emails.push({ type: 'home', value: '', primary: false })"
      />
    </fieldset>

    <!-- Phones -->
    <fieldset class="flex flex-col gap-2">
      <legend class="text-sm font-semibold text-surface-700 dark:text-surface-300 mb-1">Phone Numbers</legend>
      <div
        v-for="(phone, i) in form.phones"
        :key="i"
        class="flex items-center gap-2"
      >
        <Select
          v-model="phone.type"
          :options="phoneTypes"
          class="w-28 flex-shrink-0"
        />
        <InputText
          v-model="phone.value"
          placeholder="+1 (555) 000-0000"
          class="flex-1"
        />
        <button
          type="button"
          class="p-2 rounded-lg text-surface-400 hover:text-yellow-500"
          :class="{ 'text-yellow-500': phone.primary }"
          title="Set as primary"
          @click="setPrimaryPhone(i)"
        >
          <i class="pi" :class="phone.primary ? 'pi-star-fill' : 'pi-star'" />
        </button>
        <button
          type="button"
          class="p-2 rounded-lg text-surface-400 hover:text-red-500"
          @click="form.phones.splice(i, 1)"
        >
          <i class="pi pi-minus-circle" />
        </button>
      </div>
      <Button
        type="button"
        label="Add Phone"
        icon="pi pi-plus"
        text
        size="small"
        @click="form.phones.push({ type: 'mobile', value: '', primary: false })"
      />
    </fieldset>

    <!-- Addresses -->
    <fieldset class="flex flex-col gap-3">
      <legend class="text-sm font-semibold text-surface-700 dark:text-surface-300 mb-1">Addresses</legend>
      <div
        v-for="(addr, i) in form.addresses"
        :key="i"
        class="flex flex-col gap-2 p-3 rounded-lg border border-surface-200 dark:border-surface-700"
      >
        <div class="flex items-center justify-between">
          <Select
            v-model="addr.type"
            :options="addressTypes"
            class="w-28"
          />
          <button
            type="button"
            class="p-2 rounded-lg text-surface-400 hover:text-red-500"
            @click="form.addresses.splice(i, 1)"
          >
            <i class="pi pi-minus-circle" />
          </button>
        </div>
        <InputText v-model="addr.street" placeholder="Street" />
        <div class="grid grid-cols-2 gap-2">
          <InputText v-model="addr.city" placeholder="City" />
          <InputText v-model="addr.state" placeholder="State / Province" />
        </div>
        <div class="grid grid-cols-2 gap-2">
          <InputText v-model="addr.postal_code" placeholder="Postal Code" />
          <InputText v-model="addr.country" placeholder="Country" />
        </div>
      </div>
      <Button
        type="button"
        label="Add Address"
        icon="pi pi-plus"
        text
        size="small"
        @click="form.addresses.push({ type: 'home', street: '', city: '', state: '', postal_code: '', country: '' })"
      />
    </fieldset>

    <!-- URLs -->
    <fieldset class="flex flex-col gap-2">
      <legend class="text-sm font-semibold text-surface-700 dark:text-surface-300 mb-1">Links</legend>
      <div
        v-for="(url, i) in form.urls"
        :key="i"
        class="flex items-center gap-2"
      >
        <Select
          v-model="url.type"
          :options="urlTypes"
          class="w-28 flex-shrink-0"
        />
        <InputText
          v-model="url.value"
          placeholder="https://example.com"
          class="flex-1"
        />
        <button
          type="button"
          class="p-2 rounded-lg text-surface-400 hover:text-red-500"
          @click="form.urls.splice(i, 1)"
        >
          <i class="pi pi-minus-circle" />
        </button>
      </div>
      <Button
        type="button"
        label="Add Link"
        icon="pi pi-plus"
        text
        size="small"
        @click="form.urls.push({ type: 'homepage', value: '' })"
      />
    </fieldset>

    <!-- Organization and Title -->
    <fieldset class="flex flex-col gap-3">
      <legend class="text-sm font-semibold text-surface-700 dark:text-surface-300 mb-1">Work</legend>
      <div class="flex flex-col gap-1">
        <label class="text-xs text-surface-500">Organization</label>
        <InputText v-model="form.organization" placeholder="Company or organization" :invalid="!!errors.name" />
      </div>
      <div class="flex flex-col gap-1">
        <label class="text-xs text-surface-500">Job Title</label>
        <InputText v-model="form.title" placeholder="Job title" />
      </div>
    </fieldset>

    <!-- Birthday -->
    <div class="flex flex-col gap-1">
      <label class="text-sm font-semibold text-surface-700 dark:text-surface-300">Birthday</label>
      <DatePicker
        v-model="birthdayDate"
        date-format="yy-mm-dd"
        placeholder="Select date"
        :show-icon="true"
        show-button-bar
      />
    </div>

    <!-- Notes -->
    <div class="flex flex-col gap-1">
      <label class="text-sm font-semibold text-surface-700 dark:text-surface-300">Notes</label>
      <Textarea
        v-model="form.notes"
        rows="3"
        placeholder="Add notes..."
        auto-resize
      />
    </div>

    <!-- Actions -->
    <div v-if="!hideActions" class="flex items-center gap-2 pt-2 border-t border-surface-200 dark:border-surface-700">
      <Button
        v-if="contact"
        type="button"
        label="Delete"
        icon="pi pi-trash"
        severity="danger"
        text
        @click="$emit('delete')"
      />
      <div class="flex-1" />
      <Button type="button" label="Cancel" text @click="$emit('cancel')" />
      <Button type="submit" :label="contact ? 'Save' : 'Create'" :loading="isSubmitting" />
    </div>
  </form>
</template>

<script setup lang="ts">
import type { Contact, ContactFormData, ContactEmail, ContactPhone, ContactAddress, ContactURL, AddressBook } from '~/types/contacts';

const props = defineProps<{
  contact?: Contact;
  addressBooks: AddressBook[];
  isSubmitting?: boolean;
  hideActions?: boolean;
}>();

const emit = defineEmits<{
  submit: [data: ContactFormData, photo: File | undefined, removePhoto: boolean];
  cancel: [];
  delete: [];
}>();

const fileInputRef = ref<HTMLInputElement | null>(null);
const selectedPhoto = ref<File | null>(null);
const photoRemoved = ref(false);

const selectedAddressBookId = ref<number | null>(
  props.contact
    ? (props.addressBooks.find((ab: AddressBook) => String(ab.ID) === props.contact!.addressbook_id)?.ID ?? null)
    : (props.addressBooks[0]?.ID ?? null)
);

const emailTypes = ['home', 'work', 'other'];
const phoneTypes = ['mobile', 'home', 'work', 'fax', 'other'];
const addressTypes = ['home', 'work', 'other'];
const urlTypes = ['homepage', 'work', 'blog', 'other'];

const form = reactive({
  prefix: props.contact?.prefix || '',
  given_name: props.contact?.given_name || '',
  middle_name: props.contact?.middle_name || '',
  family_name: props.contact?.family_name || '',
  suffix: props.contact?.suffix || '',
  nickname: props.contact?.nickname || '',
  organization: props.contact?.organization || '',
  title: props.contact?.title || '',
  emails: [...(props.contact?.emails || [])] as ContactEmail[],
  phones: [...(props.contact?.phones || [])] as ContactPhone[],
  addresses: (props.contact?.addresses || []).map((a: ContactAddress) => ({ ...a })) as ContactAddress[],
  urls: [...(props.contact?.urls || [])] as ContactURL[],
  notes: props.contact?.notes || '',
});

// Birthday as Date object for DatePicker
const birthdayDate = ref<Date | null>(
  props.contact?.birthday ? new Date(props.contact.birthday + 'T00:00:00') : null
);

// Photo preview
const photoPreview = computed(() => {
  if (photoRemoved.value) return null;
  if (selectedPhoto.value) return URL.createObjectURL(selectedPhoto.value);
  if (props.contact?.photo_url) return props.contact.photo_url;
  return null;
});

const initials = computed(() => {
  const name = [form.given_name, form.family_name].filter(Boolean).join(' ')
    || form.organization || '';
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0]!.charAt(0) + parts[parts.length - 1]!.charAt(0)).toUpperCase();
  return (parts[0]?.charAt(0) || '?').toUpperCase();
});

const triggerPhotoSelect = () => {
  fileInputRef.value?.click();
};

const onFileSelected = (event: Event) => {
  const target = event.target as HTMLInputElement;
  const file = target.files?.[0];
  if (file) {
    selectedPhoto.value = file;
    photoRemoved.value = false;
  }
};

const removePhoto = () => {
  selectedPhoto.value = null;
  photoRemoved.value = true;
  if (fileInputRef.value) {
    fileInputRef.value.value = '';
  }
};

const setPrimaryEmail = (index: number) => {
  form.emails.forEach((e, i) => { e.primary = i === index; });
};

const setPrimaryPhone = (index: number) => {
  form.phones.forEach((p, i) => { p.primary = i === index; });
};

// Validation
const errors = reactive<Record<string, string>>({});

const validate = (): boolean => {
  Object.keys(errors).forEach(k => delete errors[k]);

  if (!form.given_name.trim() && !form.family_name.trim() && !form.organization.trim()) {
    errors.name = 'At least one of first name, last name, or organization is required';
  }

  if (!selectedAddressBookId.value) {
    errors.addressBook = 'Please select an address book';
  }

  return Object.keys(errors).length === 0;
};

const formatBirthday = (): string => {
  if (!birthdayDate.value) return '';
  const d = birthdayDate.value;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
};

const handleSubmit = () => {
  if (!validate()) return;

  const data: ContactFormData = {
    prefix: form.prefix.trim(),
    given_name: form.given_name.trim(),
    middle_name: form.middle_name.trim(),
    family_name: form.family_name.trim(),
    suffix: form.suffix.trim(),
    nickname: form.nickname.trim(),
    organization: form.organization.trim(),
    title: form.title.trim(),
    emails: form.emails.filter(e => e.value.trim()),
    phones: form.phones.filter(p => p.value.trim()),
    addresses: form.addresses.filter(a =>
      a.street?.trim() || a.city?.trim() || a.state?.trim() || a.postal_code?.trim() || a.country?.trim()
    ),
    urls: form.urls.filter(u => u.value.trim()),
    birthday: formatBirthday(),
    notes: form.notes.trim(),
  };

  emit('submit', data, selectedPhoto.value || undefined, photoRemoved.value);
};

defineExpose({ selectedAddressBookId, triggerSubmit: handleSubmit });
</script>
