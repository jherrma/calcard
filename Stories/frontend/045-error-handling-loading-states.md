# Story 045: Error Handling & Loading States

## Story
**As a** user
**I want to** see clear feedback when actions are in progress or when errors occur
**So that** I understand the system state and can take appropriate action

## Acceptance Criteria

### Loading States
- [ ] Skeleton loaders for initial page loads
- [ ] Inline loading spinners for button actions
- [ ] Progress bars for long operations (import/export)
- [ ] Loading overlay for full-page transitions
- [ ] Disabled state for buttons during loading
- [ ] Loading text with elapsed time for long operations

### Error Handling
- [ ] Global error boundary catches unhandled errors
- [ ] Friendly error page with retry option
- [ ] Toast notifications for API errors
- [ ] Inline error messages for form validation
- [ ] Network connectivity indicator
- [ ] Automatic retry for transient failures

### Empty States
- [ ] Illustrated empty states for lists
- [ ] Actionable empty states with CTA buttons
- [ ] Contextual help in empty states
- [ ] Different empty states for search vs no data

### Offline Support
- [ ] Offline indicator in header
- [ ] Queue actions when offline
- [ ] Sync when back online
- [ ] Warning before destructive actions offline

### User Feedback
- [ ] Success toasts for completed actions
- [ ] Warning dialogs for destructive actions
- [ ] Progress feedback for multi-step operations
- [ ] Undo option for recent actions

## Technical Details

### Error Boundary Component
```vue
<template>
  <div v-if="error" class="error-boundary">
    <div class="error-content">
      <img src="/images/error-illustration.svg" alt="Error" class="error-image" />
      <h1>Something went wrong</h1>
      <p class="error-message">{{ errorMessage }}</p>

      <div class="error-actions">
        <Button
          label="Try Again"
          icon="pi pi-refresh"
          @click="retry"
        />
        <Button
          label="Go Home"
          icon="pi pi-home"
          severity="secondary"
          @click="goHome"
        />
      </div>

      <div v-if="showDetails" class="error-details">
        <h4>Error Details</h4>
        <pre>{{ error.stack }}</pre>
      </div>

      <Button
        :label="showDetails ? 'Hide Details' : 'Show Details'"
        link
        size="small"
        @click="showDetails = !showDetails"
      />
    </div>
  </div>
  <slot v-else />
</template>

<script setup lang="ts">
import { ref, onErrorCaptured } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()

const error = ref<Error | null>(null)
const errorMessage = ref('')
const showDetails = ref(false)

onErrorCaptured((err, instance, info) => {
  error.value = err
  errorMessage.value = getErrorMessage(err)

  // Log to error tracking service
  console.error('Error captured:', err, info)

  return false // Prevent error propagation
})

function getErrorMessage(err: Error): string {
  if (err.message.includes('Network')) {
    return 'Unable to connect to the server. Please check your internet connection.'
  }
  if (err.message.includes('401')) {
    return 'Your session has expired. Please log in again.'
  }
  if (err.message.includes('403')) {
    return 'You don\'t have permission to access this resource.'
  }
  if (err.message.includes('404')) {
    return 'The requested resource was not found.'
  }
  return 'An unexpected error occurred. Please try again.'
}

function retry() {
  error.value = null
  router.go(0) // Refresh the current route
}

function goHome() {
  error.value = null
  router.push('/')
}
</script>

<style scoped>
.error-boundary {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
  padding: 2rem;
}

.error-content {
  text-align: center;
  max-width: 500px;
}

.error-image {
  width: 200px;
  height: auto;
  margin-bottom: 1.5rem;
  opacity: 0.8;
}

h1 {
  margin: 0 0 0.5rem;
  color: var(--text-color);
}

.error-message {
  color: var(--text-color-secondary);
  margin-bottom: 1.5rem;
}

.error-actions {
  display: flex;
  gap: 0.75rem;
  justify-content: center;
  margin-bottom: 1rem;
}

.error-details {
  text-align: left;
  margin-top: 1rem;
  padding: 1rem;
  background: var(--surface-ground);
  border-radius: 8px;
  overflow-x: auto;
}

.error-details pre {
  font-size: 0.75rem;
  margin: 0;
  white-space: pre-wrap;
}
</style>
```

### Loading Components
```vue
<!-- components/common/SkeletonCard.vue -->
<template>
  <div class="skeleton-card">
    <Skeleton v-if="showImage" class="skeleton-image" />
    <div class="skeleton-content">
      <Skeleton width="70%" height="1.25rem" class="mb-2" />
      <Skeleton width="90%" class="mb-2" />
      <Skeleton width="50%" />
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  showImage?: boolean
}>()
</script>

<style scoped>
.skeleton-card {
  padding: 1rem;
  background: var(--surface-card);
  border-radius: 8px;
  border: 1px solid var(--surface-border);
}

.skeleton-image {
  height: 150px;
  margin-bottom: 1rem;
}

.skeleton-content {
  display: flex;
  flex-direction: column;
}

.mb-2 {
  margin-bottom: 0.5rem;
}
</style>
```

```vue
<!-- components/common/LoadingOverlay.vue -->
<template>
  <Transition name="fade">
    <div v-if="visible" class="loading-overlay">
      <div class="loading-content">
        <ProgressSpinner
          v-if="!showProgress"
          strokeWidth="3"
        />
        <ProgressBar
          v-else
          :value="progress"
          :showValue="true"
        />
        <p v-if="message" class="loading-message">{{ message }}</p>
        <p v-if="showElapsed" class="loading-elapsed">
          {{ elapsedTime }}
        </p>
        <Button
          v-if="cancellable"
          label="Cancel"
          severity="secondary"
          size="small"
          @click="$emit('cancel')"
        />
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'

const props = withDefaults(defineProps<{
  visible: boolean
  message?: string
  progress?: number
  showProgress?: boolean
  cancellable?: boolean
  showElapsed?: boolean
}>(), {
  showProgress: false,
  cancellable: false,
  showElapsed: false
})

defineEmits(['cancel'])

const startTime = ref<number | null>(null)
const elapsed = ref(0)
let timer: NodeJS.Timer | null = null

const elapsedTime = computed(() => {
  const seconds = Math.floor(elapsed.value / 1000)
  const minutes = Math.floor(seconds / 60)
  if (minutes > 0) {
    return `${minutes}m ${seconds % 60}s`
  }
  return `${seconds}s`
})

watch(() => props.visible, (visible) => {
  if (visible) {
    startTime.value = Date.now()
    timer = setInterval(() => {
      elapsed.value = Date.now() - startTime.value!
    }, 1000)
  } else {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
    elapsed.value = 0
    startTime.value = null
  }
})

onUnmounted(() => {
  if (timer) {
    clearInterval(timer)
  }
})
</script>

<style scoped>
.loading-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9999;
}

.loading-content {
  background: var(--surface-card);
  padding: 2rem;
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1rem;
  min-width: 250px;
}

.loading-message {
  margin: 0;
  color: var(--text-color);
  font-weight: 500;
}

.loading-elapsed {
  margin: 0;
  color: var(--text-color-secondary);
  font-size: 0.875rem;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
```

### Empty State Component
```vue
<!-- components/common/EmptyState.vue -->
<template>
  <div class="empty-state" :class="{ compact }">
    <img
      v-if="image"
      :src="image"
      :alt="title"
      class="empty-image"
    />
    <i v-else-if="icon" :class="icon" class="empty-icon"></i>

    <h3 v-if="title" class="empty-title">{{ title }}</h3>
    <p v-if="description" class="empty-description">{{ description }}</p>

    <div v-if="$slots.default" class="empty-actions">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  title?: string
  description?: string
  icon?: string
  image?: string
  compact?: boolean
}>()
</script>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 3rem 2rem;
  text-align: center;
}

.empty-state.compact {
  padding: 1.5rem 1rem;
}

.empty-image {
  width: 180px;
  height: auto;
  margin-bottom: 1.5rem;
  opacity: 0.8;
}

.compact .empty-image {
  width: 100px;
}

.empty-icon {
  font-size: 4rem;
  color: var(--text-color-secondary);
  opacity: 0.5;
  margin-bottom: 1rem;
}

.compact .empty-icon {
  font-size: 2.5rem;
}

.empty-title {
  margin: 0 0 0.5rem;
  color: var(--text-color);
  font-size: 1.25rem;
}

.compact .empty-title {
  font-size: 1rem;
}

.empty-description {
  margin: 0 0 1.5rem;
  color: var(--text-color-secondary);
  max-width: 400px;
}

.compact .empty-description {
  margin-bottom: 1rem;
  font-size: 0.875rem;
}

.empty-actions {
  display: flex;
  gap: 0.75rem;
}
</style>
```

### Network Status Composable
```typescript
// composables/useNetworkStatus.ts
import { ref, onMounted, onUnmounted } from 'vue'

export function useNetworkStatus() {
  const isOnline = ref(navigator.onLine)
  const wasOffline = ref(false)

  function handleOnline() {
    isOnline.value = true
    if (wasOffline.value) {
      // Trigger sync when back online
      window.dispatchEvent(new CustomEvent('app:online'))
    }
    wasOffline.value = false
  }

  function handleOffline() {
    isOnline.value = false
    wasOffline.value = true
  }

  onMounted(() => {
    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)
  })

  onUnmounted(() => {
    window.removeEventListener('online', handleOnline)
    window.removeEventListener('offline', handleOffline)
  })

  return {
    isOnline,
    wasOffline
  }
}
```

### Offline Indicator Component
```vue
<!-- components/common/OfflineIndicator.vue -->
<template>
  <Transition name="slide">
    <div v-if="!isOnline" class="offline-indicator">
      <i class="pi pi-wifi"></i>
      <span>You're offline. Some features may not work.</span>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { useNetworkStatus } from '~/composables/useNetworkStatus'

const { isOnline } = useNetworkStatus()
</script>

<style scoped>
.offline-indicator {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  padding: 0.75rem;
  background: var(--yellow-500);
  color: var(--yellow-900);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  font-weight: 500;
  z-index: 9998;
}

.slide-enter-active,
.slide-leave-active {
  transition: transform 0.3s;
}

.slide-enter-from,
.slide-leave-to {
  transform: translateY(100%);
}
</style>
```

### API Error Handler Plugin
```typescript
// plugins/api-error-handler.ts
import { useToast } from 'primevue/usetoast'

export default defineNuxtPlugin((nuxtApp) => {
  const toast = useToast()

  // Global API error handler
  nuxtApp.hook('vue:error', (error: any) => {
    console.error('Global error:', error)
  })

  // Axios interceptor for API errors
  const api = nuxtApp.$api as any

  api.interceptors.response.use(
    (response: any) => response,
    (error: any) => {
      const status = error.response?.status
      const message = error.response?.data?.error || error.message

      // Don't show toast for cancelled requests
      if (error.code === 'ERR_CANCELED') {
        return Promise.reject(error)
      }

      // Handle specific error codes
      switch (status) {
        case 401:
          // Redirect to login
          navigateTo('/auth/login')
          toast.add({
            severity: 'warn',
            summary: 'Session Expired',
            detail: 'Please log in again',
            life: 5000
          })
          break

        case 403:
          toast.add({
            severity: 'error',
            summary: 'Access Denied',
            detail: 'You don\'t have permission for this action',
            life: 5000
          })
          break

        case 404:
          toast.add({
            severity: 'error',
            summary: 'Not Found',
            detail: message,
            life: 5000
          })
          break

        case 422:
          // Validation errors - handled by forms
          break

        case 429:
          toast.add({
            severity: 'warn',
            summary: 'Too Many Requests',
            detail: 'Please wait before trying again',
            life: 5000
          })
          break

        case 500:
        case 502:
        case 503:
          toast.add({
            severity: 'error',
            summary: 'Server Error',
            detail: 'Something went wrong. Please try again later.',
            life: 5000
          })
          break

        default:
          if (!navigator.onLine) {
            toast.add({
              severity: 'warn',
              summary: 'Offline',
              detail: 'Please check your internet connection',
              life: 5000
            })
          } else {
            toast.add({
              severity: 'error',
              summary: 'Error',
              detail: message || 'An unexpected error occurred',
              life: 5000
            })
          }
      }

      return Promise.reject(error)
    }
  )
})
```

### Undo Action Composable
```typescript
// composables/useUndo.ts
import { ref } from 'vue'
import { useToast } from 'primevue/usetoast'

interface UndoAction {
  id: string
  message: string
  undo: () => Promise<void>
  timeout: NodeJS.Timer
}

export function useUndo() {
  const toast = useToast()
  const pendingActions = ref<Map<string, UndoAction>>(new Map())

  function showUndoToast(
    id: string,
    message: string,
    undoFn: () => Promise<void>,
    duration: number = 5000
  ) {
    // Clear existing action with same id
    const existing = pendingActions.value.get(id)
    if (existing) {
      clearTimeout(existing.timeout)
    }

    const timeout = setTimeout(() => {
      pendingActions.value.delete(id)
    }, duration)

    pendingActions.value.set(id, {
      id,
      message,
      undo: undoFn,
      timeout
    })

    toast.add({
      severity: 'success',
      summary: message,
      detail: 'Click to undo',
      life: duration,
      group: 'undo',
      data: { id }
    })
  }

  async function performUndo(id: string) {
    const action = pendingActions.value.get(id)
    if (!action) return

    clearTimeout(action.timeout)
    pendingActions.value.delete(id)

    try {
      await action.undo()
      toast.add({
        severity: 'info',
        summary: 'Undone',
        detail: 'Action has been undone',
        life: 3000
      })
    } catch (error) {
      toast.add({
        severity: 'error',
        summary: 'Undo Failed',
        detail: 'Could not undo the action',
        life: 5000
      })
    }
  }

  return {
    showUndoToast,
    performUndo
  }
}
```

### Usage Example in Calendar Store
```typescript
// Example usage in stores/calendars.ts
async function deleteEvent(eventId: string) {
  const { showUndoToast } = useUndo()
  const event = events.value.find(e => e.id === eventId)
  if (!event) return

  // Optimistically remove
  events.value = events.value.filter(e => e.id !== eventId)

  try {
    await api.delete(`/api/v1/events/${eventId}`)

    showUndoToast(
      `delete-event-${eventId}`,
      'Event deleted',
      async () => {
        // Restore event on undo
        const { data } = await api.post('/api/v1/events', event)
        events.value.push(data)
      }
    )
  } catch (error) {
    // Restore on error
    events.value.push(event)
    throw error
  }
}
```

## Dependencies
- Story 031 (Frontend Project Setup)
- PrimeVue Toast component
- PrimeVue Skeleton component

## Estimation
- **Complexity:** Medium
- **Components:** 5 components, 3 composables, 1 plugin

## Notes
- Error tracking integration (Sentry, etc.) can be added later
- Offline support is basic - full PWA support could be a separate story
- Consider implementing optimistic updates for better UX
- Undo functionality works best for recent, reversible actions
- Loading states should match brand design guidelines
