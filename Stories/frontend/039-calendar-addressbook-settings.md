# Story 039: Calendar and Address Book Settings

## Title
Implement Calendar/Address Book Settings and Sharing UI

## Description
As a user, I want to manage my calendars and address books settings, including sharing them with other users.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| CD-3.1.3 | Users can rename calendars |
| CD-3.1.4 | Users can set calendar color |
| CD-3.1.5 | Users can set calendar timezone |
| CD-3.1.6 | Users can delete calendars |
| SH-5.1.1 | Users can share calendars with other users |
| SH-5.1.2 | Users can grant read-only access |
| SH-5.1.3 | Users can grant read-write access |
| SH-5.1.4 | Users can view list of shares |
| SH-5.1.5 | Users can modify share permissions |
| SH-5.1.6 | Users can revoke shares |
| AD-4.1.3 | Users can rename address books |
| SH-5.2.1 | Users can share address books |

## Acceptance Criteria

### Calendar Settings Dialog/Page

- [ ] Accessible from calendar sidebar menu
- [ ] Displays:
  - [ ] Calendar name (editable)
  - [ ] Calendar color picker
  - [ ] Timezone selector
  - [ ] Description (editable)
  - [ ] CalDAV URL (copy button)
  - [ ] Public URL toggle and link
- [ ] Save and Cancel buttons
- [ ] Delete button (with confirmation)

### Calendar Sharing Section

- [ ] List of current shares:
  - [ ] User avatar and name
  - [ ] Permission level (read/read-write)
  - [ ] Change permission dropdown
  - [ ] Remove share button
- [ ] "Share with..." input:
  - [ ] User search by email/username
  - [ ] Permission selector
  - [ ] Add button
- [ ] Empty state when no shares

### Address Book Settings Dialog/Page

- [ ] Same pattern as calendar settings
- [ ] Address book name (editable)
- [ ] Description (editable)
- [ ] CardDAV URL (copy button)
- [ ] Delete button (with confirmation)

### Address Book Sharing Section

- [ ] Same pattern as calendar sharing
- [ ] List of shares with permissions
- [ ] Add share by user search

### Create Calendar Dialog

- [ ] Name input (required)
- [ ] Color picker
- [ ] Timezone selector
- [ ] Description (optional)
- [ ] Create button

### Create Address Book Dialog

- [ ] Name input (required)
- [ ] Description (optional)
- [ ] Create button

### Public Calendar Settings

- [ ] Toggle to enable/disable public access
- [ ] Public iCal URL display (when enabled)
- [ ] Copy URL button
- [ ] Regenerate URL button (invalidates old URL)
- [ ] Warning about public access

## Technical Notes

### Calendar Settings Dialog
```vue
<!-- components/calendar/CalendarSettingsDialog.vue -->
<template>
  <Dialog
    :visible="visible"
    :header="`${calendar?.name} Settings`"
    :modal="true"
    :style="{ width: '600px' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <TabView>
      <!-- General Tab -->
      <TabPanel header="General">
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <InputText v-model="form.name" class="w-full" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Color</label>
            <div class="flex items-center gap-2">
              <ColorPicker v-model="form.color" />
              <InputText v-model="form.color" class="w-32 font-mono" />
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Timezone</label>
            <Dropdown
              v-model="form.timezone"
              :options="timezones"
              filter
              class="w-full"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Description</label>
            <Textarea v-model="form.description" class="w-full" rows="3" />
          </div>
        </div>
      </TabPanel>

      <!-- Sharing Tab -->
      <TabPanel header="Sharing">
        <CalendarSharing :calendar-id="calendar?.id" />
      </TabPanel>

      <!-- Public Access Tab -->
      <TabPanel header="Public Access">
        <CalendarPublicAccess :calendar="calendar" @updated="$emit('updated')" />
      </TabPanel>

      <!-- Integration Tab -->
      <TabPanel header="Integration">
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">CalDAV URL</label>
            <div class="flex gap-2">
              <InputText :value="caldavUrl" readonly class="flex-1 font-mono text-sm" />
              <Button icon="pi pi-copy" severity="secondary" @click="copyUrl(caldavUrl)" />
            </div>
          </div>

          <div v-if="calendar?.public_url">
            <label class="block text-sm font-medium text-gray-700 mb-1">Public iCal URL</label>
            <div class="flex gap-2">
              <InputText :value="calendar.public_url" readonly class="flex-1 font-mono text-sm" />
              <Button icon="pi pi-copy" severity="secondary" @click="copyUrl(calendar.public_url)" />
            </div>
          </div>
        </div>
      </TabPanel>
    </TabView>

    <template #footer>
      <div class="flex justify-between">
        <Button
          label="Delete Calendar"
          severity="danger"
          text
          @click="confirmDelete"
        />
        <div class="flex gap-2">
          <Button label="Cancel" severity="secondary" @click="$emit('update:visible', false)" />
          <Button label="Save" :loading="isSaving" @click="save" />
        </div>
      </div>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import type { Calendar } from '~/types';

const props = defineProps<{
  visible: boolean;
  calendar: Calendar | null;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  updated: [];
  deleted: [];
}>();

const toast = useAppToast();
const confirm = useConfirm();
const api = useApi();
const config = useRuntimeConfig();

const isSaving = ref(false);

const form = reactive({
  name: '',
  color: '',
  timezone: '',
  description: '',
});

// Initialize form when calendar changes
watch(() => props.calendar, (cal) => {
  if (cal) {
    form.name = cal.name;
    form.color = cal.color;
    form.timezone = cal.timezone;
    form.description = cal.description || '';
  }
}, { immediate: true });

const timezones = computed(() => Intl.supportedValuesOf('timeZone'));

const caldavUrl = computed(() => {
  if (!props.calendar) return '';
  return `${config.public.apiBaseUrl}/dav/calendars/${props.calendar.owner_username || 'me'}/${props.calendar.id}/`;
});

const save = async () => {
  isSaving.value = true;
  try {
    await api.patch(`/api/v1/calendars/${props.calendar?.id}`, form);
    toast.success('Calendar updated');
    emit('updated');
    emit('update:visible', false);
  } catch (e: any) {
    toast.error(e.message || 'Failed to update calendar');
  } finally {
    isSaving.value = false;
  }
};

const confirmDelete = () => {
  confirm.require({
    message: 'Are you sure you want to delete this calendar? All events will be permanently deleted.',
    header: 'Delete Calendar',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: deleteCalendar,
  });
};

const deleteCalendar = async () => {
  try {
    await api.delete(`/api/v1/calendars/${props.calendar?.id}`, {
      body: JSON.stringify({ confirmation: 'DELETE' }),
    });
    toast.success('Calendar deleted');
    emit('deleted');
    emit('update:visible', false);
  } catch (e: any) {
    toast.error(e.message || 'Failed to delete calendar');
  }
};

const copyUrl = async (url: string) => {
  await navigator.clipboard.writeText(url);
  toast.success('URL copied');
};
</script>
```

### Calendar Sharing Component
```vue
<!-- components/calendar/CalendarSharing.vue -->
<template>
  <div class="space-y-6">
    <!-- Add share -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-2">Share with</label>
      <div class="flex gap-2">
        <AutoComplete
          v-model="newShare.user"
          :suggestions="userSuggestions"
          @complete="searchUsers"
          field="display_name"
          placeholder="Search by email or username"
          class="flex-1"
        >
          <template #item="{ item }">
            <div class="flex items-center gap-2">
              <Avatar :label="item.display_name?.charAt(0)" shape="circle" />
              <div>
                <div>{{ item.display_name }}</div>
                <div class="text-xs text-gray-500">{{ item.email }}</div>
              </div>
            </div>
          </template>
        </AutoComplete>
        <Dropdown
          v-model="newShare.permission"
          :options="permissionOptions"
          option-label="label"
          option-value="value"
          class="w-36"
        />
        <Button
          label="Share"
          :disabled="!newShare.user"
          :loading="isSharing"
          @click="addShare"
        />
      </div>
    </div>

    <!-- Current shares -->
    <div>
      <h3 class="text-sm font-medium text-gray-700 mb-3">Shared with</h3>

      <div v-if="shares.length === 0" class="text-center py-8 text-gray-500">
        This calendar is not shared with anyone
      </div>

      <div v-else class="space-y-2">
        <div
          v-for="share in shares"
          :key="share.id"
          class="flex items-center justify-between p-3 bg-gray-50 rounded-lg"
        >
          <div class="flex items-center gap-3">
            <Avatar
              :label="share.shared_with.display_name?.charAt(0)"
              shape="circle"
            />
            <div>
              <div class="font-medium">{{ share.shared_with.display_name }}</div>
              <div class="text-sm text-gray-500">{{ share.shared_with.email }}</div>
            </div>
          </div>

          <div class="flex items-center gap-2">
            <Dropdown
              :model-value="share.permission"
              :options="permissionOptions"
              option-label="label"
              option-value="value"
              class="w-36"
              @update:model-value="updatePermission(share, $event)"
            />
            <Button
              icon="pi pi-trash"
              severity="danger"
              text
              rounded
              @click="confirmRemove(share)"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { CalendarShare, User } from '~/types';

const props = defineProps<{
  calendarId: string;
}>();

const toast = useAppToast();
const confirm = useConfirm();
const api = useApi();

const shares = ref<CalendarShare[]>([]);
const userSuggestions = ref<User[]>([]);
const isSharing = ref(false);

const newShare = reactive({
  user: null as User | null,
  permission: 'read',
});

const permissionOptions = [
  { label: 'Can view', value: 'read' },
  { label: 'Can edit', value: 'read-write' },
];

onMounted(async () => {
  await fetchShares();
});

const fetchShares = async () => {
  const response = await api.get<{ shares: CalendarShare[] }>(
    `/api/v1/calendars/${props.calendarId}/shares`
  );
  shares.value = response.shares;
};

const searchUsers = async (event: { query: string }) => {
  if (event.query.length < 2) {
    userSuggestions.value = [];
    return;
  }

  const response = await api.get<{ users: User[] }>(
    `/api/v1/users/search?q=${encodeURIComponent(event.query)}`
  );
  userSuggestions.value = response.users;
};

const addShare = async () => {
  if (!newShare.user) return;

  isSharing.value = true;
  try {
    await api.post(`/api/v1/calendars/${props.calendarId}/shares`, {
      user_identifier: newShare.user.email,
      permission: newShare.permission,
    });
    toast.success(`Shared with ${newShare.user.display_name}`);
    newShare.user = null;
    await fetchShares();
  } catch (e: any) {
    toast.error(e.message || 'Failed to share calendar');
  } finally {
    isSharing.value = false;
  }
};

const updatePermission = async (share: CalendarShare, permission: string) => {
  try {
    await api.patch(`/api/v1/calendars/${props.calendarId}/shares/${share.id}`, {
      permission,
    });
    share.permission = permission;
    toast.success('Permission updated');
  } catch {
    toast.error('Failed to update permission');
  }
};

const confirmRemove = (share: CalendarShare) => {
  confirm.require({
    message: `Remove ${share.shared_with.display_name}'s access to this calendar?`,
    header: 'Remove Access',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => removeShare(share),
  });
};

const removeShare = async (share: CalendarShare) => {
  try {
    await api.delete(`/api/v1/calendars/${props.calendarId}/shares/${share.id}`);
    toast.success('Access removed');
    await fetchShares();
  } catch {
    toast.error('Failed to remove access');
  }
};
</script>
```

### Calendar Public Access Component
```vue
<!-- components/calendar/CalendarPublicAccess.vue -->
<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h3 class="font-medium">Public Access</h3>
        <p class="text-sm text-gray-500">
          Allow anyone with the link to view this calendar (read-only)
        </p>
      </div>
      <InputSwitch v-model="isPublic" @change="togglePublic" />
    </div>

    <div v-if="isPublic && publicUrl" class="space-y-4">
      <Message severity="info" :closable="false">
        Anyone with this URL can view your calendar events.
        They cannot make changes.
      </Message>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Public iCal URL</label>
        <div class="flex gap-2">
          <InputText :value="publicUrl" readonly class="flex-1 font-mono text-sm" />
          <Button icon="pi pi-copy" severity="secondary" @click="copyUrl" />
        </div>
        <p class="text-xs text-gray-500 mt-1">
          Use this URL in Google Calendar, Outlook, or any app that supports iCal subscriptions
        </p>
      </div>

      <div>
        <Button
          label="Regenerate URL"
          severity="secondary"
          size="small"
          @click="confirmRegenerate"
        />
        <p class="text-xs text-gray-500 mt-1">
          This will invalidate the current URL. Anyone using the old URL will lose access.
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Calendar } from '~/types';

const props = defineProps<{
  calendar: Calendar | null;
}>();

const emit = defineEmits<{
  updated: [];
}>();

const toast = useAppToast();
const confirm = useConfirm();
const api = useApi();

const isPublic = ref(false);
const publicUrl = ref<string | null>(null);

watch(() => props.calendar, (cal) => {
  if (cal) {
    isPublic.value = !!cal.public_url;
    publicUrl.value = cal.public_url || null;
  }
}, { immediate: true });

const togglePublic = async () => {
  try {
    const response = await api.post<{ public_url: string | null }>(
      `/api/v1/calendars/${props.calendar?.id}/public`,
      { enabled: isPublic.value }
    );
    publicUrl.value = response.public_url;
    toast.success(isPublic.value ? 'Public access enabled' : 'Public access disabled');
    emit('updated');
  } catch (e: any) {
    isPublic.value = !isPublic.value; // Revert
    toast.error(e.message || 'Failed to update public access');
  }
};

const copyUrl = async () => {
  if (publicUrl.value) {
    await navigator.clipboard.writeText(publicUrl.value);
    toast.success('URL copied');
  }
};

const confirmRegenerate = () => {
  confirm.require({
    message: 'This will create a new URL and invalidate the old one. Continue?',
    header: 'Regenerate URL',
    icon: 'pi pi-exclamation-triangle',
    accept: regenerateUrl,
  });
};

const regenerateUrl = async () => {
  try {
    const response = await api.post<{ public_url: string }>(
      `/api/v1/calendars/${props.calendar?.id}/public/regenerate`
    );
    publicUrl.value = response.public_url;
    toast.success('New URL generated');
    emit('updated');
  } catch {
    toast.error('Failed to regenerate URL');
  }
};
</script>
```

### Create Calendar Dialog
```vue
<!-- components/calendar/AddCalendarDialog.vue -->
<template>
  <Dialog
    :visible="visible"
    header="Create Calendar"
    :modal="true"
    :style="{ width: '450px' }"
    @update:visible="$emit('update:visible', $event)"
  >
    <div class="space-y-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Name *</label>
        <InputText v-model="form.name" class="w-full" placeholder="My Calendar" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Color</label>
        <div class="flex items-center gap-2">
          <ColorPicker v-model="form.color" />
          <InputText v-model="form.color" class="w-32 font-mono" />
        </div>
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Timezone</label>
        <Dropdown
          v-model="form.timezone"
          :options="timezones"
          filter
          class="w-full"
        />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Description</label>
        <Textarea v-model="form.description" class="w-full" rows="2" />
      </div>
    </div>

    <template #footer>
      <Button label="Cancel" severity="secondary" @click="$emit('update:visible', false)" />
      <Button
        label="Create"
        :loading="isCreating"
        :disabled="!form.name"
        @click="create"
      />
    </template>
  </Dialog>
</template>

<script setup lang="ts">
const props = defineProps<{
  visible: boolean;
}>();

const emit = defineEmits<{
  'update:visible': [value: boolean];
  created: [];
}>();

const toast = useAppToast();
const api = useApi();

const isCreating = ref(false);

const form = reactive({
  name: '',
  color: '#3788d8',
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  description: '',
});

const timezones = computed(() => Intl.supportedValuesOf('timeZone'));

const create = async () => {
  isCreating.value = true;
  try {
    await api.post('/api/v1/calendars', form);
    toast.success('Calendar created');
    emit('created');
    emit('update:visible', false);
    // Reset form
    form.name = '';
    form.description = '';
  } catch (e: any) {
    toast.error(e.message || 'Failed to create calendar');
  } finally {
    isCreating.value = false;
  }
};
</script>
```

## Definition of Done

- [ ] Calendar settings dialog with all fields
- [ ] Color picker works
- [ ] Timezone selector works
- [ ] Calendar sharing - add share works
- [ ] Calendar sharing - list shows all shares
- [ ] Calendar sharing - change permission works
- [ ] Calendar sharing - remove share works
- [ ] Public access toggle works
- [ ] Public URL displayed and copyable
- [ ] Regenerate public URL works
- [ ] CalDAV URL displayed and copyable
- [ ] Delete calendar with confirmation
- [ ] Create calendar dialog works
- [ ] Address book settings (same pattern)
- [ ] Address book sharing works
- [ ] Create address book dialog works
