# Story 043: Sharing Management UI

## Story
**As a** user
**I want to** manage sharing settings for my calendars and address books
**So that** I can collaborate with other users and control access to my data

## Acceptance Criteria

### Sharing Dialog
- [ ] Sharing dialog accessible from calendar/addressbook context menu
- [ ] Shows current share status (private, shared, public)
- [ ] List of current shares with user name and permission level
- [ ] Search/autocomplete to find users to share with
- [ ] Permission dropdown (Read, Read-Write)
- [ ] Remove share button with confirmation
- [ ] Save changes with loading indicator

### User Search
- [ ] Autocomplete search for users by email or username
- [ ] Shows user avatar, name, and email in results
- [ ] Prevents sharing with self
- [ ] Handles no results gracefully
- [ ] Debounced search to reduce API calls

### Shared With Me Section
- [ ] Settings page shows calendars/addressbooks shared with user
- [ ] Shows owner name and permission level
- [ ] Toggle visibility of shared items
- [ ] Leave/unsubscribe from shared items

### Share Notifications
- [ ] Toast notification when shared with someone
- [ ] Error handling for invalid users
- [ ] Warning when downgrading permissions
- [ ] Confirmation when removing all shares

### Public Access (Calendar Only)
- [ ] Toggle for public read-only access
- [ ] Display public URL when enabled
- [ ] Copy URL button
- [ ] QR code generation for public URL
- [ ] Warning about public access implications

## Technical Details

### Sharing Dialog Component
```vue
<template>
  <Dialog
    v-model:visible="visible"
    :header="`Share ${resourceType === 'calendar' ? 'Calendar' : 'Address Book'}`"
    :modal="true"
    :style="{ width: '500px' }"
    class="sharing-dialog"
  >
    <div class="sharing-content">
      <!-- Current Shares -->
      <div class="current-shares">
        <h3>Shared with</h3>

        <div v-if="shares.length === 0" class="empty-shares">
          <p>Not shared with anyone yet</p>
        </div>

        <div v-else class="share-list">
          <div
            v-for="share in shares"
            :key="share.id"
            class="share-item"
          >
            <Avatar
              :image="share.user.avatarUrl"
              :label="getInitials(share.user)"
              shape="circle"
            />
            <div class="share-info">
              <span class="share-name">{{ share.user.displayName }}</span>
              <span class="share-email">{{ share.user.email }}</span>
            </div>
            <Dropdown
              v-model="share.permission"
              :options="permissionOptions"
              optionLabel="label"
              optionValue="value"
              class="permission-dropdown"
              @change="updateShare(share)"
            />
            <Button
              icon="pi pi-times"
              text
              rounded
              severity="danger"
              @click="confirmRemoveShare(share)"
            />
          </div>
        </div>
      </div>

      <Divider />

      <!-- Add New Share -->
      <div class="add-share">
        <h3>Add people</h3>
        <div class="user-search">
          <AutoComplete
            v-model="searchQuery"
            :suggestions="userSuggestions"
            optionLabel="displayName"
            placeholder="Search by email or username"
            :loading="searching"
            @complete="searchUsers"
            @item-select="addShare"
          >
            <template #option="{ option }">
              <div class="user-suggestion">
                <Avatar
                  :image="option.avatarUrl"
                  :label="getInitials(option)"
                  shape="circle"
                  size="small"
                />
                <div class="user-info">
                  <span class="user-name">{{ option.displayName }}</span>
                  <span class="user-email">{{ option.email }}</span>
                </div>
              </div>
            </template>
            <template #empty>
              <div class="no-results">
                No users found
              </div>
            </template>
          </AutoComplete>

          <Dropdown
            v-model="newSharePermission"
            :options="permissionOptions"
            optionLabel="label"
            optionValue="value"
            placeholder="Permission"
          />
        </div>
      </div>

      <!-- Public Access (Calendar Only) -->
      <template v-if="resourceType === 'calendar'">
        <Divider />

        <div class="public-access">
          <div class="public-toggle">
            <div class="toggle-info">
              <h3>Public Access</h3>
              <p class="text-muted">
                Anyone with the link can view this calendar
              </p>
            </div>
            <InputSwitch v-model="isPublic" @change="togglePublicAccess" />
          </div>

          <div v-if="isPublic" class="public-url-section">
            <Message severity="warn" :closable="false">
              This calendar is publicly accessible. Anyone with the URL can view events.
            </Message>

            <div class="url-display">
              <InputText
                :value="publicUrl"
                readonly
                class="url-input"
              />
              <Button
                icon="pi pi-copy"
                severity="secondary"
                v-tooltip="'Copy URL'"
                @click="copyPublicUrl"
              />
              <Button
                icon="pi pi-qrcode"
                severity="secondary"
                v-tooltip="'Show QR Code'"
                @click="showQrCode = true"
              />
            </div>

            <div class="url-formats">
              <h4>Subscribe URL (iCal)</h4>
              <div class="url-display">
                <InputText
                  :value="icalUrl"
                  readonly
                  class="url-input"
                />
                <Button
                  icon="pi pi-copy"
                  severity="secondary"
                  @click="copyIcalUrl"
                />
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <template #footer>
      <Button
        label="Cancel"
        severity="secondary"
        @click="visible = false"
      />
      <Button
        label="Done"
        @click="visible = false"
      />
    </template>

    <!-- QR Code Dialog -->
    <Dialog
      v-model:visible="showQrCode"
      header="Public Calendar QR Code"
      :modal="true"
      :style="{ width: '350px' }"
    >
      <div class="qr-code-container">
        <canvas ref="qrCanvas"></canvas>
        <p class="text-center text-muted">
          Scan to subscribe to this calendar
        </p>
      </div>
    </Dialog>

    <!-- Remove Share Confirmation -->
    <ConfirmDialog />
  </Dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import QRCode from 'qrcode'
import { useDebounceFn } from '@vueuse/core'

interface Share {
  id: string
  user: {
    id: string
    displayName: string
    email: string
    avatarUrl?: string
  }
  permission: 'read' | 'read-write'
}

interface User {
  id: string
  displayName: string
  email: string
  avatarUrl?: string
}

const props = defineProps<{
  resourceId: string
  resourceType: 'calendar' | 'addressbook'
}>()

const visible = defineModel<boolean>('visible', { required: true })

const confirm = useConfirm()
const toast = useToast()

const shares = ref<Share[]>([])
const searchQuery = ref('')
const userSuggestions = ref<User[]>([])
const searching = ref(false)
const newSharePermission = ref<'read' | 'read-write'>('read')
const isPublic = ref(false)
const publicUrl = ref('')
const icalUrl = ref('')
const showQrCode = ref(false)
const qrCanvas = ref<HTMLCanvasElement | null>(null)
const loading = ref(false)

const permissionOptions = [
  { label: 'Can view', value: 'read' },
  { label: 'Can edit', value: 'read-write' }
]

watch(visible, async (newValue) => {
  if (newValue) {
    await loadShares()
  }
})

watch(showQrCode, async (newValue) => {
  if (newValue && publicUrl.value) {
    await nextTick()
    generateQrCode()
  }
})

async function loadShares() {
  loading.value = true
  try {
    const endpoint = props.resourceType === 'calendar'
      ? `/api/v1/calendars/${props.resourceId}/shares`
      : `/api/v1/addressbooks/${props.resourceId}/shares`

    const { data } = await useApi().get(endpoint)
    shares.value = data.shares
    isPublic.value = data.isPublic || false
    publicUrl.value = data.publicUrl || ''
    icalUrl.value = data.icalUrl || ''
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to load sharing settings',
      life: 5000
    })
  } finally {
    loading.value = false
  }
}

const searchUsers = useDebounceFn(async (event: { query: string }) => {
  if (event.query.length < 2) {
    userSuggestions.value = []
    return
  }

  searching.value = true
  try {
    const { data } = await useApi().get('/api/v1/users/search', {
      params: { q: event.query }
    })
    // Filter out already shared users and self
    const existingIds = new Set(shares.value.map(s => s.user.id))
    userSuggestions.value = data.users.filter(
      (u: User) => !existingIds.has(u.id)
    )
  } catch (error) {
    userSuggestions.value = []
  } finally {
    searching.value = false
  }
}, 300)

async function addShare(event: { value: User }) {
  const user = event.value
  try {
    const endpoint = props.resourceType === 'calendar'
      ? `/api/v1/calendars/${props.resourceId}/shares`
      : `/api/v1/addressbooks/${props.resourceId}/shares`

    const { data } = await useApi().post(endpoint, {
      userId: user.id,
      permission: newSharePermission.value
    })

    shares.value.push(data.share)
    searchQuery.value = ''

    toast.add({
      severity: 'success',
      summary: 'Shared',
      detail: `Shared with ${user.displayName}`,
      life: 3000
    })
  } catch (error: any) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: error.response?.data?.error || 'Failed to share',
      life: 5000
    })
  }
}

async function updateShare(share: Share) {
  try {
    const endpoint = props.resourceType === 'calendar'
      ? `/api/v1/calendars/${props.resourceId}/shares/${share.id}`
      : `/api/v1/addressbooks/${props.resourceId}/shares/${share.id}`

    await useApi().patch(endpoint, {
      permission: share.permission
    })

    toast.add({
      severity: 'success',
      summary: 'Updated',
      detail: 'Permission updated',
      life: 3000
    })
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to update permission',
      life: 5000
    })
    await loadShares() // Reload to get correct state
  }
}

function confirmRemoveShare(share: Share) {
  confirm.require({
    message: `Remove ${share.user.displayName}'s access to this ${props.resourceType}?`,
    header: 'Remove Share',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: () => removeShare(share)
  })
}

async function removeShare(share: Share) {
  try {
    const endpoint = props.resourceType === 'calendar'
      ? `/api/v1/calendars/${props.resourceId}/shares/${share.id}`
      : `/api/v1/addressbooks/${props.resourceId}/shares/${share.id}`

    await useApi().delete(endpoint)

    shares.value = shares.value.filter(s => s.id !== share.id)

    toast.add({
      severity: 'success',
      summary: 'Removed',
      detail: `Removed ${share.user.displayName}'s access`,
      life: 3000
    })
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to remove share',
      life: 5000
    })
  }
}

async function togglePublicAccess() {
  try {
    const endpoint = `/api/v1/calendars/${props.resourceId}/public`

    if (isPublic.value) {
      const { data } = await useApi().post(endpoint)
      publicUrl.value = data.publicUrl
      icalUrl.value = data.icalUrl

      toast.add({
        severity: 'success',
        summary: 'Public Access Enabled',
        detail: 'Calendar is now publicly accessible',
        life: 3000
      })
    } else {
      await useApi().delete(endpoint)
      publicUrl.value = ''
      icalUrl.value = ''

      toast.add({
        severity: 'success',
        summary: 'Public Access Disabled',
        detail: 'Calendar is now private',
        life: 3000
      })
    }
  } catch (error) {
    isPublic.value = !isPublic.value // Revert
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to update public access',
      life: 5000
    })
  }
}

function copyPublicUrl() {
  navigator.clipboard.writeText(publicUrl.value)
  toast.add({
    severity: 'success',
    summary: 'Copied',
    detail: 'URL copied to clipboard',
    life: 2000
  })
}

function copyIcalUrl() {
  navigator.clipboard.writeText(icalUrl.value)
  toast.add({
    severity: 'success',
    summary: 'Copied',
    detail: 'iCal URL copied to clipboard',
    life: 2000
  })
}

async function generateQrCode() {
  if (qrCanvas.value && icalUrl.value) {
    try {
      await QRCode.toCanvas(qrCanvas.value, icalUrl.value, {
        width: 256,
        margin: 2
      })
    } catch (error) {
      console.error('QR code generation failed:', error)
    }
  }
}

function getInitials(user: { displayName?: string; email?: string }): string {
  const name = user.displayName || user.email || ''
  const parts = name.split(/[\s@]/)
  return parts.map(p => p[0]).slice(0, 2).join('').toUpperCase()
}
</script>

<style scoped>
.sharing-content {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.current-shares h3,
.add-share h3,
.public-access h3 {
  margin: 0 0 0.75rem 0;
  font-size: 1rem;
}

.empty-shares {
  color: var(--text-color-secondary);
  font-style: italic;
}

.share-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.share-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem;
  background: var(--surface-ground);
  border-radius: 6px;
}

.share-info {
  flex: 1;
  min-width: 0;
}

.share-name {
  display: block;
  font-weight: 500;
}

.share-email {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.permission-dropdown {
  width: 120px;
}

.user-search {
  display: flex;
  gap: 0.5rem;
}

.user-search :deep(.p-autocomplete) {
  flex: 1;
}

.user-suggestion {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.user-info {
  display: flex;
  flex-direction: column;
}

.user-name {
  font-weight: 500;
}

.user-email {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.no-results {
  padding: 0.5rem;
  color: var(--text-color-secondary);
}

.public-toggle {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}

.toggle-info h3 {
  margin-bottom: 0.25rem;
}

.text-muted {
  color: var(--text-color-secondary);
  font-size: 0.875rem;
  margin: 0;
}

.public-url-section {
  margin-top: 1rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.url-display {
  display: flex;
  gap: 0.5rem;
}

.url-input {
  flex: 1;
  font-family: monospace;
  font-size: 0.875rem;
}

.url-formats h4 {
  margin: 0 0 0.5rem 0;
  font-size: 0.875rem;
  font-weight: 500;
}

.qr-code-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 1rem;
}

.text-center {
  text-align: center;
}
</style>
```

### Shared With Me Component
```vue
<template>
  <div class="shared-with-me">
    <h2>Shared with me</h2>

    <TabView>
      <TabPanel header="Calendars">
        <div v-if="sharedCalendars.length === 0" class="empty-state">
          <p>No calendars have been shared with you</p>
        </div>
        <div v-else class="shared-list">
          <div
            v-for="item in sharedCalendars"
            :key="item.id"
            class="shared-item"
          >
            <div
              class="color-indicator"
              :style="{ backgroundColor: item.calendar.color }"
            ></div>
            <div class="shared-info">
              <span class="shared-name">{{ item.calendar.name }}</span>
              <span class="shared-owner">
                Shared by {{ item.owner.displayName }}
              </span>
            </div>
            <Tag
              :value="item.permission === 'read' ? 'View only' : 'Can edit'"
              :severity="item.permission === 'read' ? 'info' : 'success'"
            />
            <InputSwitch
              v-model="item.visible"
              v-tooltip="item.visible ? 'Hide from calendar' : 'Show in calendar'"
              @change="toggleVisibility(item, 'calendar')"
            />
            <Button
              icon="pi pi-sign-out"
              text
              rounded
              severity="danger"
              v-tooltip="'Leave'"
              @click="confirmLeave(item, 'calendar')"
            />
          </div>
        </div>
      </TabPanel>

      <TabPanel header="Address Books">
        <div v-if="sharedAddressbooks.length === 0" class="empty-state">
          <p>No address books have been shared with you</p>
        </div>
        <div v-else class="shared-list">
          <div
            v-for="item in sharedAddressbooks"
            :key="item.id"
            class="shared-item"
          >
            <i class="pi pi-book shared-icon"></i>
            <div class="shared-info">
              <span class="shared-name">{{ item.addressbook.name }}</span>
              <span class="shared-owner">
                Shared by {{ item.owner.displayName }}
              </span>
            </div>
            <Tag
              :value="item.permission === 'read' ? 'View only' : 'Can edit'"
              :severity="item.permission === 'read' ? 'info' : 'success'"
            />
            <InputSwitch
              v-model="item.visible"
              v-tooltip="item.visible ? 'Hide' : 'Show'"
              @change="toggleVisibility(item, 'addressbook')"
            />
            <Button
              icon="pi pi-sign-out"
              text
              rounded
              severity="danger"
              v-tooltip="'Leave'"
              @click="confirmLeave(item, 'addressbook')"
            />
          </div>
        </div>
      </TabPanel>
    </TabView>

    <ConfirmDialog />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'

interface SharedCalendar {
  id: string
  calendar: {
    id: string
    name: string
    color: string
  }
  owner: {
    id: string
    displayName: string
  }
  permission: 'read' | 'read-write'
  visible: boolean
}

interface SharedAddressbook {
  id: string
  addressbook: {
    id: string
    name: string
  }
  owner: {
    id: string
    displayName: string
  }
  permission: 'read' | 'read-write'
  visible: boolean
}

const confirm = useConfirm()
const toast = useToast()

const sharedCalendars = ref<SharedCalendar[]>([])
const sharedAddressbooks = ref<SharedAddressbook[]>([])

onMounted(async () => {
  await loadSharedItems()
})

async function loadSharedItems() {
  try {
    const [calResponse, abResponse] = await Promise.all([
      useApi().get('/api/v1/calendars/shared-with-me'),
      useApi().get('/api/v1/addressbooks/shared-with-me')
    ])
    sharedCalendars.value = calResponse.data.calendars
    sharedAddressbooks.value = abResponse.data.addressbooks
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to load shared items',
      life: 5000
    })
  }
}

async function toggleVisibility(
  item: SharedCalendar | SharedAddressbook,
  type: 'calendar' | 'addressbook'
) {
  try {
    const endpoint = type === 'calendar'
      ? `/api/v1/calendars/shared-with-me/${item.id}/visibility`
      : `/api/v1/addressbooks/shared-with-me/${item.id}/visibility`

    await useApi().patch(endpoint, { visible: item.visible })
  } catch (error) {
    item.visible = !item.visible // Revert
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to update visibility',
      life: 5000
    })
  }
}

function confirmLeave(
  item: SharedCalendar | SharedAddressbook,
  type: 'calendar' | 'addressbook'
) {
  const name = type === 'calendar'
    ? (item as SharedCalendar).calendar.name
    : (item as SharedAddressbook).addressbook.name

  confirm.require({
    message: `Stop receiving updates from "${name}"? You'll need to be re-invited to access it again.`,
    header: 'Leave Shared Item',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    acceptLabel: 'Leave',
    accept: () => leaveShared(item, type)
  })
}

async function leaveShared(
  item: SharedCalendar | SharedAddressbook,
  type: 'calendar' | 'addressbook'
) {
  try {
    const endpoint = type === 'calendar'
      ? `/api/v1/calendars/shared-with-me/${item.id}`
      : `/api/v1/addressbooks/shared-with-me/${item.id}`

    await useApi().delete(endpoint)

    if (type === 'calendar') {
      sharedCalendars.value = sharedCalendars.value.filter(c => c.id !== item.id)
    } else {
      sharedAddressbooks.value = sharedAddressbooks.value.filter(a => a.id !== item.id)
    }

    toast.add({
      severity: 'success',
      summary: 'Left',
      detail: 'You have left the shared item',
      life: 3000
    })
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to leave',
      life: 5000
    })
  }
}
</script>

<style scoped>
.shared-with-me {
  padding: 1.5rem;
}

.empty-state {
  color: var(--text-color-secondary);
  padding: 2rem;
  text-align: center;
}

.shared-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.shared-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem;
  background: var(--surface-ground);
  border-radius: 6px;
}

.color-indicator {
  width: 12px;
  height: 12px;
  border-radius: 3px;
}

.shared-icon {
  font-size: 1.25rem;
  color: var(--text-color-secondary);
}

.shared-info {
  flex: 1;
  min-width: 0;
}

.shared-name {
  display: block;
  font-weight: 500;
}

.shared-owner {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}
</style>
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 039 (Calendar/Addressbook Settings)
- Backend Story 023 (Calendar Sharing)
- Backend Story 024 (Addressbook Sharing)

## Estimation
- **Complexity:** Medium
- **Components:** 2 main components (SharingDialog, SharedWithMe)

## Notes
- User search must exclude current user and already-shared users
- Permission changes take effect immediately
- Consider real-time updates via WebSocket for collaborative scenarios
- QR code uses qrcode npm package
