# Story 050: PWA & Push Notifications

## Story
**As a** user
**I want to** install the application on my device and receive notifications for events
**So that** I can access it like a native app and never miss important appointments

## Acceptance Criteria

### Progressive Web App
- [ ] Application installable on desktop and mobile
- [ ] Works offline with cached data
- [ ] App icon and splash screen configured
- [ ] Standalone display mode (no browser chrome)
- [ ] Service worker caches critical assets
- [ ] Background sync for offline changes

### Push Notifications
- [ ] Permission request for notifications
- [ ] Event reminders at configurable times
- [ ] Notification for shared calendar invites
- [ ] Notification actions (snooze, dismiss, view)
- [ ] Notification settings in user preferences
- [ ] Quiet hours configuration

### Notification Preferences
- [ ] Enable/disable all notifications
- [ ] Per-calendar notification settings
- [ ] Default reminder times (5min, 15min, 30min, 1hr, 1day)
- [ ] Custom reminder times
- [ ] Sound/vibration preferences
- [ ] Email notifications as fallback

### Offline Capabilities
- [ ] View cached calendars and contacts offline
- [ ] Create/edit events offline (queue for sync)
- [ ] Conflict resolution on sync
- [ ] Clear offline indicator
- [ ] Data freshness timestamp

### Install Experience
- [ ] Install prompt at appropriate time
- [ ] Custom install banner (not browser default)
- [ ] Post-install welcome screen
- [ ] Update notification for new versions

## Technical Details

### PWA Configuration (nuxt.config.ts)
```typescript
// nuxt.config.ts
export default defineNuxtConfig({
  modules: ['@vite-pwa/nuxt'],

  pwa: {
    registerType: 'autoUpdate',
    manifest: {
      name: 'CalDAV Calendar',
      short_name: 'Calendar',
      description: 'Self-hosted CalDAV/CardDAV server with web interface',
      theme_color: '#3B82F6',
      background_color: '#ffffff',
      display: 'standalone',
      orientation: 'portrait-primary',
      start_url: '/',
      scope: '/',
      icons: [
        {
          src: '/icons/icon-72x72.png',
          sizes: '72x72',
          type: 'image/png'
        },
        {
          src: '/icons/icon-96x96.png',
          sizes: '96x96',
          type: 'image/png'
        },
        {
          src: '/icons/icon-128x128.png',
          sizes: '128x128',
          type: 'image/png'
        },
        {
          src: '/icons/icon-144x144.png',
          sizes: '144x144',
          type: 'image/png'
        },
        {
          src: '/icons/icon-152x152.png',
          sizes: '152x152',
          type: 'image/png'
        },
        {
          src: '/icons/icon-192x192.png',
          sizes: '192x192',
          type: 'image/png',
          purpose: 'any maskable'
        },
        {
          src: '/icons/icon-384x384.png',
          sizes: '384x384',
          type: 'image/png'
        },
        {
          src: '/icons/icon-512x512.png',
          sizes: '512x512',
          type: 'image/png',
          purpose: 'any maskable'
        }
      ],
      screenshots: [
        {
          src: '/screenshots/desktop.png',
          sizes: '1280x720',
          type: 'image/png',
          form_factor: 'wide'
        },
        {
          src: '/screenshots/mobile.png',
          sizes: '750x1334',
          type: 'image/png',
          form_factor: 'narrow'
        }
      ],
      shortcuts: [
        {
          name: 'New Event',
          short_name: 'New Event',
          url: '/calendar?action=new',
          icons: [{ src: '/icons/shortcut-event.png', sizes: '96x96' }]
        },
        {
          name: 'New Contact',
          short_name: 'New Contact',
          url: '/contacts?action=new',
          icons: [{ src: '/icons/shortcut-contact.png', sizes: '96x96' }]
        }
      ]
    },
    workbox: {
      navigateFallback: '/',
      globPatterns: ['**/*.{js,css,html,png,svg,ico,woff2}'],
      runtimeCaching: [
        {
          urlPattern: /^https:\/\/api\..*\/v1\/.*/,
          handler: 'NetworkFirst',
          options: {
            cacheName: 'api-cache',
            expiration: {
              maxEntries: 100,
              maxAgeSeconds: 60 * 60 * 24 // 24 hours
            },
            networkTimeoutSeconds: 10
          }
        },
        {
          urlPattern: /\.(?:png|jpg|jpeg|svg|gif|webp)$/,
          handler: 'CacheFirst',
          options: {
            cacheName: 'image-cache',
            expiration: {
              maxEntries: 50,
              maxAgeSeconds: 60 * 60 * 24 * 30 // 30 days
            }
          }
        }
      ]
    },
    client: {
      installPrompt: true,
      periodicSyncForUpdates: 3600 // Check for updates hourly
    }
  }
})
```

### Service Worker for Push Notifications
```typescript
// service-worker/push.ts
/// <reference lib="webworker" />
declare const self: ServiceWorkerGlobalScope

self.addEventListener('push', (event) => {
  if (!event.data) return

  const data = event.data.json()

  const options: NotificationOptions = {
    body: data.body,
    icon: '/icons/icon-192x192.png',
    badge: '/icons/badge-72x72.png',
    vibrate: [100, 50, 100],
    data: {
      url: data.url,
      eventId: data.eventId
    },
    actions: [
      { action: 'view', title: 'View' },
      { action: 'snooze', title: 'Snooze 10min' },
      { action: 'dismiss', title: 'Dismiss' }
    ],
    tag: data.tag || 'default',
    renotify: true,
    requireInteraction: data.requireInteraction || false
  }

  event.waitUntil(
    self.registration.showNotification(data.title, options)
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()

  const { action } = event
  const { url, eventId } = event.notification.data

  if (action === 'snooze') {
    // Send snooze request to server
    event.waitUntil(
      fetch('/api/v1/notifications/snooze', {
        method: 'POST',
        body: JSON.stringify({ eventId, minutes: 10 }),
        headers: { 'Content-Type': 'application/json' }
      })
    )
  } else if (action === 'view' || !action) {
    // Open the app to the event
    event.waitUntil(
      clients.matchAll({ type: 'window' }).then((clientList) => {
        // Focus existing window if available
        for (const client of clientList) {
          if (client.url === url && 'focus' in client) {
            return client.focus()
          }
        }
        // Open new window
        if (clients.openWindow) {
          return clients.openWindow(url)
        }
      })
    )
  }
})

// Background sync for offline changes
self.addEventListener('sync', (event) => {
  if (event.tag === 'sync-changes') {
    event.waitUntil(syncOfflineChanges())
  }
})

async function syncOfflineChanges() {
  const db = await openDB('offline-changes')
  const changes = await db.getAll('pending')

  for (const change of changes) {
    try {
      await fetch(change.url, {
        method: change.method,
        body: JSON.stringify(change.data),
        headers: { 'Content-Type': 'application/json' }
      })
      await db.delete('pending', change.id)
    } catch (error) {
      // Will retry on next sync
      console.error('Sync failed:', error)
    }
  }
}
```

### Push Notification Composable
```typescript
// composables/usePushNotifications.ts
import { ref, computed } from 'vue'

export function usePushNotifications() {
  const permission = ref<NotificationPermission>('default')
  const subscription = ref<PushSubscription | null>(null)
  const isSupported = ref(false)

  // Check support on mount
  onMounted(() => {
    isSupported.value = 'Notification' in window && 'serviceWorker' in navigator
    if (isSupported.value) {
      permission.value = Notification.permission
    }
  })

  const canRequestPermission = computed(() =>
    isSupported.value && permission.value === 'default'
  )

  const isEnabled = computed(() =>
    permission.value === 'granted' && subscription.value !== null
  )

  async function requestPermission(): Promise<boolean> {
    if (!isSupported.value) return false

    const result = await Notification.requestPermission()
    permission.value = result

    if (result === 'granted') {
      await subscribe()
      return true
    }
    return false
  }

  async function subscribe() {
    try {
      const registration = await navigator.serviceWorker.ready

      // Get VAPID public key from server
      const { data } = await useApi().get('/api/v1/notifications/vapid-public-key')

      const sub = await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(data.publicKey)
      })

      subscription.value = sub

      // Send subscription to server
      await useApi().post('/api/v1/notifications/subscribe', {
        endpoint: sub.endpoint,
        keys: {
          p256dh: arrayBufferToBase64(sub.getKey('p256dh')!),
          auth: arrayBufferToBase64(sub.getKey('auth')!)
        }
      })
    } catch (error) {
      console.error('Failed to subscribe:', error)
      throw error
    }
  }

  async function unsubscribe() {
    if (subscription.value) {
      await subscription.value.unsubscribe()
      await useApi().delete('/api/v1/notifications/subscribe')
      subscription.value = null
    }
  }

  return {
    permission,
    subscription,
    isSupported,
    canRequestPermission,
    isEnabled,
    requestPermission,
    subscribe,
    unsubscribe
  }
}

// Helper functions
function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - base64String.length % 4) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = window.atob(base64)
  const outputArray = new Uint8Array(rawData.length)
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i)
  }
  return outputArray
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return window.btoa(binary)
}
```

### Install Prompt Component
```vue
<!-- components/pwa/InstallPrompt.vue -->
<template>
  <Transition name="slide-up">
    <div v-if="showPrompt && !dismissed" class="install-prompt">
      <div class="prompt-content">
        <img src="/icons/icon-72x72.png" alt="" class="app-icon" />
        <div class="prompt-text">
          <h3>Install CalDAV Calendar</h3>
          <p>Add to your home screen for quick access</p>
        </div>
        <div class="prompt-actions">
          <Button
            label="Install"
            size="small"
            @click="install"
          />
          <Button
            icon="pi pi-times"
            text
            rounded
            size="small"
            @click="dismiss"
          />
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useStorage } from '@vueuse/core'

const showPrompt = ref(false)
const deferredPrompt = ref<any>(null)
const dismissed = useStorage('install-prompt-dismissed', false)

onMounted(() => {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferredPrompt.value = e

    // Show prompt after user has engaged with the app
    setTimeout(() => {
      if (!dismissed.value) {
        showPrompt.value = true
      }
    }, 30000) // 30 seconds
  })

  window.addEventListener('appinstalled', () => {
    showPrompt.value = false
    deferredPrompt.value = null
  })
})

async function install() {
  if (!deferredPrompt.value) return

  deferredPrompt.value.prompt()
  const { outcome } = await deferredPrompt.value.userChoice

  if (outcome === 'accepted') {
    showPrompt.value = false
  }

  deferredPrompt.value = null
}

function dismiss() {
  showPrompt.value = false
  dismissed.value = true
}
</script>

<style scoped>
.install-prompt {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  background: var(--surface-card);
  border-top: 1px solid var(--surface-border);
  padding: 1rem;
  padding-bottom: calc(1rem + env(safe-area-inset-bottom));
  box-shadow: var(--shadow-lg);
  z-index: 1000;
}

.prompt-content {
  display: flex;
  align-items: center;
  gap: 1rem;
  max-width: 600px;
  margin: 0 auto;
}

.app-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
}

.prompt-text {
  flex: 1;
}

.prompt-text h3 {
  margin: 0;
  font-size: 1rem;
}

.prompt-text p {
  margin: 0.25rem 0 0;
  font-size: 0.875rem;
  color: var(--text-color-secondary);
}

.prompt-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.slide-up-enter-active,
.slide-up-leave-active {
  transition: transform 0.3s, opacity 0.3s;
}

.slide-up-enter-from,
.slide-up-leave-to {
  transform: translateY(100%);
  opacity: 0;
}
</style>
```

### Notification Settings Component
```vue
<!-- components/settings/NotificationSettings.vue -->
<template>
  <div class="notification-settings">
    <h3>Notifications</h3>

    <!-- Permission Status -->
    <div class="setting-section">
      <div class="setting-header">
        <div class="setting-info">
          <span class="setting-label">Push Notifications</span>
          <span class="setting-description">
            Receive reminders for upcoming events
          </span>
        </div>

        <div v-if="!isSupported" class="not-supported">
          <Tag value="Not Supported" severity="secondary" />
        </div>
        <div v-else-if="permission === 'denied'" class="permission-denied">
          <Tag value="Blocked" severity="danger" />
          <small>Enable in browser settings</small>
        </div>
        <div v-else-if="!isEnabled">
          <Button
            label="Enable"
            size="small"
            @click="enableNotifications"
          />
        </div>
        <div v-else>
          <Tag value="Enabled" severity="success" />
        </div>
      </div>
    </div>

    <!-- Notification Preferences (when enabled) -->
    <template v-if="isEnabled">
      <Divider />

      <div class="setting-section">
        <h4>Default Reminders</h4>
        <div class="reminder-options">
          <div
            v-for="option in reminderOptions"
            :key="option.value"
            class="reminder-option"
          >
            <Checkbox
              v-model="selectedReminders"
              :inputId="`reminder-${option.value}`"
              :value="option.value"
              @change="saveReminders"
            />
            <label :for="`reminder-${option.value}`">
              {{ option.label }}
            </label>
          </div>
        </div>
      </div>

      <div class="setting-section">
        <h4>Quiet Hours</h4>
        <div class="quiet-hours">
          <div class="field-checkbox">
            <Checkbox
              v-model="quietHoursEnabled"
              inputId="quiet-hours"
              binary
              @change="saveQuietHours"
            />
            <label for="quiet-hours">Enable quiet hours</label>
          </div>

          <div v-if="quietHoursEnabled" class="time-range">
            <span>From</span>
            <Calendar
              v-model="quietHoursStart"
              timeOnly
              hourFormat="24"
              @date-select="saveQuietHours"
            />
            <span>to</span>
            <Calendar
              v-model="quietHoursEnd"
              timeOnly
              hourFormat="24"
              @date-select="saveQuietHours"
            />
          </div>
        </div>
      </div>

      <div class="setting-section">
        <h4>Notification Types</h4>
        <div class="notification-types">
          <div class="type-option">
            <div class="type-info">
              <span class="type-label">Event Reminders</span>
              <span class="type-description">
                Notifications before events start
              </span>
            </div>
            <InputSwitch
              v-model="notificationTypes.eventReminders"
              @change="saveNotificationTypes"
            />
          </div>

          <div class="type-option">
            <div class="type-info">
              <span class="type-label">Calendar Invites</span>
              <span class="type-description">
                When someone shares a calendar with you
              </span>
            </div>
            <InputSwitch
              v-model="notificationTypes.calendarInvites"
              @change="saveNotificationTypes"
            />
          </div>

          <div class="type-option">
            <div class="type-info">
              <span class="type-label">Event Changes</span>
              <span class="type-description">
                When shared events are modified
              </span>
            </div>
            <InputSwitch
              v-model="notificationTypes.eventChanges"
              @change="saveNotificationTypes"
            />
          </div>
        </div>
      </div>

      <Divider />

      <div class="setting-section">
        <Button
          label="Send Test Notification"
          severity="secondary"
          size="small"
          :loading="sendingTest"
          @click="sendTestNotification"
        />
      </div>
    </template>

    <!-- Email Fallback -->
    <Divider />

    <div class="setting-section">
      <h4>Email Notifications</h4>
      <p class="setting-description">
        Receive notifications via email when push notifications are unavailable
      </p>

      <div class="email-settings">
        <div class="field-checkbox">
          <Checkbox
            v-model="emailNotifications"
            inputId="email-notifications"
            binary
            @change="saveEmailSettings"
          />
          <label for="email-notifications">
            Enable email notifications
          </label>
        </div>

        <div v-if="emailNotifications" class="email-options">
          <div class="field-checkbox">
            <Checkbox
              v-model="emailTypes.dailyDigest"
              inputId="daily-digest"
              binary
              @change="saveEmailSettings"
            />
            <label for="daily-digest">Daily agenda digest</label>
          </div>
          <div class="field-checkbox">
            <Checkbox
              v-model="emailTypes.eventReminders"
              inputId="email-reminders"
              binary
              @change="saveEmailSettings"
            />
            <label for="email-reminders">Event reminders (1 day before)</label>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { usePushNotifications } from '~/composables/usePushNotifications'
import { useToast } from 'primevue/usetoast'

const toast = useToast()

const {
  permission,
  isSupported,
  isEnabled,
  requestPermission
} = usePushNotifications()

const selectedReminders = ref<number[]>([15, 60])
const quietHoursEnabled = ref(false)
const quietHoursStart = ref(new Date(2000, 0, 1, 22, 0))
const quietHoursEnd = ref(new Date(2000, 0, 1, 8, 0))
const emailNotifications = ref(false)
const sendingTest = ref(false)

const notificationTypes = reactive({
  eventReminders: true,
  calendarInvites: true,
  eventChanges: false
})

const emailTypes = reactive({
  dailyDigest: false,
  eventReminders: true
})

const reminderOptions = [
  { label: '5 minutes before', value: 5 },
  { label: '15 minutes before', value: 15 },
  { label: '30 minutes before', value: 30 },
  { label: '1 hour before', value: 60 },
  { label: '1 day before', value: 1440 }
]

onMounted(async () => {
  await loadSettings()
})

async function loadSettings() {
  try {
    const { data } = await useApi().get('/api/v1/users/me/notification-settings')
    selectedReminders.value = data.defaultReminders || [15, 60]
    quietHoursEnabled.value = data.quietHours?.enabled || false
    if (data.quietHours?.start) {
      const [hours, minutes] = data.quietHours.start.split(':')
      quietHoursStart.value = new Date(2000, 0, 1, parseInt(hours), parseInt(minutes))
    }
    if (data.quietHours?.end) {
      const [hours, minutes] = data.quietHours.end.split(':')
      quietHoursEnd.value = new Date(2000, 0, 1, parseInt(hours), parseInt(minutes))
    }
    Object.assign(notificationTypes, data.notificationTypes || {})
    emailNotifications.value = data.emailNotifications?.enabled || false
    Object.assign(emailTypes, data.emailNotifications?.types || {})
  } catch (error) {
    console.error('Failed to load settings:', error)
  }
}

async function enableNotifications() {
  const success = await requestPermission()
  if (success) {
    toast.add({
      severity: 'success',
      summary: 'Notifications Enabled',
      detail: 'You will now receive push notifications',
      life: 3000
    })
  }
}

async function saveReminders() {
  await saveSettings({ defaultReminders: selectedReminders.value })
}

async function saveQuietHours() {
  const format = (date: Date) =>
    `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`

  await saveSettings({
    quietHours: {
      enabled: quietHoursEnabled.value,
      start: format(quietHoursStart.value),
      end: format(quietHoursEnd.value)
    }
  })
}

async function saveNotificationTypes() {
  await saveSettings({ notificationTypes })
}

async function saveEmailSettings() {
  await saveSettings({
    emailNotifications: {
      enabled: emailNotifications.value,
      types: emailTypes
    }
  })
}

async function saveSettings(settings: any) {
  try {
    await useApi().patch('/api/v1/users/me/notification-settings', settings)
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to save settings',
      life: 5000
    })
  }
}

async function sendTestNotification() {
  sendingTest.value = true
  try {
    await useApi().post('/api/v1/notifications/test')
    toast.add({
      severity: 'success',
      summary: 'Test Sent',
      detail: 'Check for the notification',
      life: 3000
    })
  } catch (error) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to send test notification',
      life: 5000
    })
  } finally {
    sendingTest.value = false
  }
}
</script>

<style scoped>
.notification-settings {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.setting-section {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.setting-section h4 {
  margin: 0;
  font-size: 0.875rem;
  font-weight: 600;
}

.setting-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.setting-info {
  display: flex;
  flex-direction: column;
}

.setting-label {
  font-weight: 500;
}

.setting-description {
  font-size: 0.875rem;
  color: var(--text-color-secondary);
  margin: 0;
}

.permission-denied {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.25rem;
}

.permission-denied small {
  color: var(--text-color-secondary);
}

.reminder-options {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.reminder-option {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.quiet-hours {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.time-range {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.notification-types {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.type-option {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.type-info {
  display: flex;
  flex-direction: column;
}

.type-label {
  font-weight: 500;
}

.type-description {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
}

.email-settings {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.email-options {
  margin-left: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.field-checkbox {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 038 (Settings Pages)
- @vite-pwa/nuxt module
- Backend notification endpoints

## Estimation
- **Complexity:** High
- **Components:** 2 components, 1 composable, service worker config

## Notes
- VAPID keys must be generated and stored securely
- iOS Safari has limited PWA and push notification support
- Test offline functionality thoroughly
- Background sync requires HTTPS
- Consider notification batching to avoid spam
- Push notifications require backend infrastructure (web-push library)
