# Story 047: Accessibility (a11y)

## Story
**As a** user with disabilities
**I want to** use the application with assistive technologies
**So that** I can manage my calendars and contacts effectively regardless of my abilities

## Acceptance Criteria

### Screen Reader Support
- [ ] All interactive elements have accessible names
- [ ] Images have meaningful alt text
- [ ] Icons with meaning have aria-labels
- [ ] Decorative elements marked with aria-hidden
- [ ] Form fields have associated labels
- [ ] Error messages announced to screen readers
- [ ] Dynamic content updates announced via live regions

### Keyboard Navigation
- [ ] All functionality accessible via keyboard
- [ ] Logical tab order throughout application
- [ ] Skip links for main content
- [ ] Focus trapped in modals/dialogs
- [ ] Escape key closes modals
- [ ] Arrow key navigation in menus and lists
- [ ] Visible focus indicators on all elements

### Visual Accessibility
- [ ] Color contrast meets WCAG AA (4.5:1 text, 3:1 UI)
- [ ] Information not conveyed by color alone
- [ ] Text resizable up to 200% without loss
- [ ] No content relies solely on sensory characteristics
- [ ] Animations respect reduced motion preference
- [ ] Focus indicators visible in all themes

### Semantic HTML
- [ ] Proper heading hierarchy (h1-h6)
- [ ] Landmark regions (main, nav, aside)
- [ ] Lists marked up as lists
- [ ] Tables have proper headers
- [ ] Buttons vs links used correctly
- [ ] Form groups use fieldset/legend

### ARIA Implementation
- [ ] ARIA roles used appropriately
- [ ] aria-expanded for collapsible sections
- [ ] aria-selected for selections
- [ ] aria-current for current page/date
- [ ] aria-describedby for additional context
- [ ] aria-live for dynamic updates

## Technical Details

### Accessibility Composables
```typescript
// composables/useA11y.ts
import { ref, onMounted, onUnmounted } from 'vue'

export function useReducedMotion() {
  const prefersReducedMotion = ref(false)

  onMounted(() => {
    const mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
    prefersReducedMotion.value = mediaQuery.matches

    const handler = (e: MediaQueryListEvent) => {
      prefersReducedMotion.value = e.matches
    }

    mediaQuery.addEventListener('change', handler)
    onUnmounted(() => mediaQuery.removeEventListener('change', handler))
  })

  return { prefersReducedMotion }
}

export function useFocusTrap(containerRef: Ref<HTMLElement | null>) {
  const focusableSelector = [
    'button:not([disabled])',
    'input:not([disabled])',
    'select:not([disabled])',
    'textarea:not([disabled])',
    'a[href]',
    '[tabindex]:not([tabindex="-1"])'
  ].join(', ')

  function getFocusableElements(): HTMLElement[] {
    if (!containerRef.value) return []
    return Array.from(containerRef.value.querySelectorAll(focusableSelector))
  }

  function trapFocus(event: KeyboardEvent) {
    if (event.key !== 'Tab') return

    const focusable = getFocusableElements()
    if (focusable.length === 0) return

    const first = focusable[0]
    const last = focusable[focusable.length - 1]

    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }

  function activate() {
    document.addEventListener('keydown', trapFocus)
    // Focus first element
    const focusable = getFocusableElements()
    if (focusable.length > 0) {
      focusable[0].focus()
    }
  }

  function deactivate() {
    document.removeEventListener('keydown', trapFocus)
  }

  return { activate, deactivate }
}

export function useAnnouncer() {
  const message = ref('')
  const politeness = ref<'polite' | 'assertive'>('polite')

  function announce(text: string, urgent = false) {
    message.value = ''
    politeness.value = urgent ? 'assertive' : 'polite'

    // Small delay to ensure screen readers pick up the change
    setTimeout(() => {
      message.value = text
    }, 100)
  }

  return { message, politeness, announce }
}
```

### Screen Reader Announcer Component
```vue
<!-- components/common/ScreenReaderAnnouncer.vue -->
<template>
  <div class="sr-only" aria-live="polite" aria-atomic="true">
    {{ politeMessage }}
  </div>
  <div class="sr-only" aria-live="assertive" aria-atomic="true">
    {{ assertiveMessage }}
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const politeMessage = ref('')
const assertiveMessage = ref('')

// Provide globally
provide('announce', (message: string, urgent = false) => {
  if (urgent) {
    assertiveMessage.value = ''
    setTimeout(() => { assertiveMessage.value = message }, 100)
  } else {
    politeMessage.value = ''
    setTimeout(() => { politeMessage.value = message }, 100)
  }
})
</script>

<style scoped>
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
```

### Skip Links Component
```vue
<!-- components/common/SkipLinks.vue -->
<template>
  <nav class="skip-links" aria-label="Skip links">
    <a href="#main-content" class="skip-link">
      Skip to main content
    </a>
    <a href="#main-navigation" class="skip-link">
      Skip to navigation
    </a>
  </nav>
</template>

<style scoped>
.skip-links {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 10000;
}

.skip-link {
  position: absolute;
  top: -100%;
  left: 0;
  padding: 0.75rem 1rem;
  background: var(--primary-color);
  color: white;
  font-weight: 500;
  text-decoration: none;
  border-radius: 0 0 4px 0;
  transition: top 0.2s;
}

.skip-link:focus {
  top: 0;
  outline: 2px solid var(--primary-700);
  outline-offset: 2px;
}
</style>
```

### Accessible Modal Component
```vue
<!-- components/common/AccessibleDialog.vue -->
<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="visible"
        ref="dialogRef"
        class="dialog-overlay"
        role="dialog"
        :aria-modal="true"
        :aria-labelledby="titleId"
        :aria-describedby="descriptionId"
        @click.self="dismissable && close()"
        @keydown.escape="close"
      >
        <div class="dialog-container" :style="{ maxWidth }">
          <header class="dialog-header">
            <h2 :id="titleId" class="dialog-title">
              <slot name="header">{{ header }}</slot>
            </h2>
            <button
              type="button"
              class="dialog-close"
              aria-label="Close dialog"
              @click="close"
            >
              <i class="pi pi-times" aria-hidden="true"></i>
            </button>
          </header>

          <div :id="descriptionId" class="dialog-content">
            <slot />
          </div>

          <footer v-if="$slots.footer" class="dialog-footer">
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onUnmounted } from 'vue'
import { useFocusTrap } from '~/composables/useA11y'

const props = withDefaults(defineProps<{
  visible: boolean
  header?: string
  maxWidth?: string
  dismissable?: boolean
}>(), {
  maxWidth: '500px',
  dismissable: true
})

const emit = defineEmits(['update:visible', 'close'])

const dialogRef = ref<HTMLElement | null>(null)
const previousActiveElement = ref<HTMLElement | null>(null)

const titleId = `dialog-title-${Math.random().toString(36).slice(2)}`
const descriptionId = `dialog-desc-${Math.random().toString(36).slice(2)}`

const { activate: activateFocusTrap, deactivate: deactivateFocusTrap } = useFocusTrap(dialogRef)

watch(() => props.visible, async (visible) => {
  if (visible) {
    previousActiveElement.value = document.activeElement as HTMLElement
    document.body.style.overflow = 'hidden'
    await nextTick()
    activateFocusTrap()
  } else {
    deactivateFocusTrap()
    document.body.style.overflow = ''
    // Restore focus to previous element
    if (previousActiveElement.value) {
      previousActiveElement.value.focus()
    }
  }
})

onUnmounted(() => {
  deactivateFocusTrap()
  document.body.style.overflow = ''
})

function close() {
  emit('update:visible', false)
  emit('close')
}
</script>

<style scoped>
.dialog-overlay {
  position: fixed;
  inset: 0;
  background: var(--surface-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1rem;
  z-index: 1000;
}

.dialog-container {
  background: var(--surface-card);
  border-radius: 8px;
  box-shadow: var(--shadow-lg);
  width: 100%;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
}

.dialog-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1.5rem;
  border-bottom: 1px solid var(--surface-border);
}

.dialog-title {
  margin: 0;
  font-size: 1.25rem;
}

.dialog-close {
  background: none;
  border: none;
  padding: 0.5rem;
  cursor: pointer;
  color: var(--text-color-secondary);
  border-radius: 4px;
  transition: background-color 0.2s;
}

.dialog-close:hover {
  background: var(--surface-hover);
  color: var(--text-color);
}

.dialog-close:focus-visible {
  outline: 2px solid var(--primary-color);
  outline-offset: 2px;
}

.dialog-content {
  padding: 1.5rem;
  overflow-y: auto;
  flex: 1;
}

.dialog-footer {
  padding: 1rem 1.5rem;
  border-top: 1px solid var(--surface-border);
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
}

/* Transitions */
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.2s;
}

.modal-enter-active .dialog-container,
.modal-leave-active .dialog-container {
  transition: transform 0.2s;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}

.modal-enter-from .dialog-container,
.modal-leave-to .dialog-container {
  transform: scale(0.95);
}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  .modal-enter-active,
  .modal-leave-active,
  .modal-enter-active .dialog-container,
  .modal-leave-active .dialog-container {
    transition: none;
  }
}
</style>
```

### Accessible Calendar Day Component
```vue
<!-- components/calendar/AccessibleCalendarDay.vue -->
<template>
  <button
    type="button"
    class="calendar-day"
    :class="{
      today: isToday,
      selected: isSelected,
      'other-month': isOtherMonth,
      'has-events': events.length > 0
    }"
    :aria-label="ariaLabel"
    :aria-pressed="isSelected"
    :aria-current="isToday ? 'date' : undefined"
    :tabindex="isFocusable ? 0 : -1"
    @click="$emit('select', date)"
    @keydown="handleKeydown"
  >
    <span class="day-number">{{ day }}</span>
    <span v-if="events.length > 0" class="event-indicator" aria-hidden="true">
      <span
        v-for="(event, index) in visibleEvents"
        :key="index"
        class="event-dot"
        :style="{ backgroundColor: event.color }"
      ></span>
    </span>
    <span v-if="events.length > 0" class="sr-only">
      {{ events.length }} {{ events.length === 1 ? 'event' : 'events' }}
    </span>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { format, isToday as checkIsToday, isSameMonth } from 'date-fns'

interface CalendarEvent {
  id: string
  title: string
  color: string
}

const props = defineProps<{
  date: Date
  currentMonth: Date
  selectedDate?: Date
  events: CalendarEvent[]
  isFocusable: boolean
}>()

const emit = defineEmits(['select', 'navigate'])

const day = computed(() => props.date.getDate())
const isToday = computed(() => checkIsToday(props.date))
const isSelected = computed(() =>
  props.selectedDate &&
  format(props.date, 'yyyy-MM-dd') === format(props.selectedDate, 'yyyy-MM-dd')
)
const isOtherMonth = computed(() => !isSameMonth(props.date, props.currentMonth))
const visibleEvents = computed(() => props.events.slice(0, 3))

const ariaLabel = computed(() => {
  const dateStr = format(props.date, 'EEEE, MMMM d, yyyy')
  const eventCount = props.events.length
  const todayStr = isToday.value ? ', Today' : ''
  const eventStr = eventCount > 0
    ? `, ${eventCount} ${eventCount === 1 ? 'event' : 'events'}`
    : ''
  return `${dateStr}${todayStr}${eventStr}`
})

function handleKeydown(event: KeyboardEvent) {
  const keyActions: Record<string, string> = {
    ArrowLeft: 'prev-day',
    ArrowRight: 'next-day',
    ArrowUp: 'prev-week',
    ArrowDown: 'next-week',
    Home: 'start-of-week',
    End: 'end-of-week',
    PageUp: 'prev-month',
    PageDown: 'next-month'
  }

  if (keyActions[event.key]) {
    event.preventDefault()
    emit('navigate', keyActions[event.key])
  }
}
</script>

<style scoped>
.calendar-day {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 0.5rem;
  background: none;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  min-height: 60px;
  transition: background-color 0.2s;
}

.calendar-day:hover {
  background: var(--surface-hover);
}

.calendar-day:focus-visible {
  outline: 2px solid var(--primary-color);
  outline-offset: 2px;
}

.calendar-day.today .day-number {
  background: var(--primary-color);
  color: white;
  border-radius: 50%;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.calendar-day.selected {
  background: var(--primary-100);
}

.calendar-day.other-month {
  color: var(--text-color-secondary);
}

.event-indicator {
  display: flex;
  gap: 2px;
  margin-top: 4px;
}

.event-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
```

### Accessible Form Components
```vue
<!-- components/form/AccessibleInput.vue -->
<template>
  <div class="form-field" :class="{ 'has-error': hasError }">
    <label :for="inputId" class="form-label">
      {{ label }}
      <span v-if="required" class="required" aria-hidden="true">*</span>
      <span v-if="required" class="sr-only">(required)</span>
    </label>

    <div class="input-wrapper">
      <span v-if="$slots.prefix" class="input-prefix" aria-hidden="true">
        <slot name="prefix" />
      </span>

      <input
        :id="inputId"
        ref="inputRef"
        v-bind="$attrs"
        :value="modelValue"
        :type="type"
        :required="required"
        :aria-invalid="hasError"
        :aria-describedby="describedBy"
        class="form-input"
        @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      />

      <span v-if="$slots.suffix" class="input-suffix" aria-hidden="true">
        <slot name="suffix" />
      </span>
    </div>

    <p v-if="hint && !hasError" :id="hintId" class="form-hint">
      {{ hint }}
    </p>

    <p v-if="hasError" :id="errorId" class="form-error" role="alert">
      <i class="pi pi-exclamation-circle" aria-hidden="true"></i>
      {{ error }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: string
  label: string
  type?: string
  required?: boolean
  hint?: string
  error?: string
}>(), {
  type: 'text',
  required: false
})

defineEmits(['update:modelValue'])

const inputRef = ref<HTMLInputElement | null>(null)

const inputId = `input-${Math.random().toString(36).slice(2)}`
const hintId = `hint-${inputId}`
const errorId = `error-${inputId}`

const hasError = computed(() => !!props.error)

const describedBy = computed(() => {
  const ids = []
  if (props.hint && !hasError.value) ids.push(hintId)
  if (hasError.value) ids.push(errorId)
  return ids.length > 0 ? ids.join(' ') : undefined
})

// Expose focus method
defineExpose({
  focus: () => inputRef.value?.focus()
})
</script>

<style scoped>
.form-field {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.form-label {
  font-weight: 500;
  font-size: 0.875rem;
  color: var(--text-color);
}

.required {
  color: var(--red-500);
  margin-left: 0.125rem;
}

.input-wrapper {
  display: flex;
  align-items: center;
  background: var(--surface-card);
  border: 1px solid var(--surface-border);
  border-radius: 6px;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.input-wrapper:focus-within {
  border-color: var(--primary-color);
  box-shadow: 0 0 0 3px var(--primary-100);
}

.has-error .input-wrapper {
  border-color: var(--red-500);
}

.has-error .input-wrapper:focus-within {
  box-shadow: 0 0 0 3px rgba(239, 68, 68, 0.2);
}

.form-input {
  flex: 1;
  padding: 0.625rem 0.75rem;
  border: none;
  background: none;
  font-size: 1rem;
  color: var(--text-color);
  outline: none;
}

.form-input::placeholder {
  color: var(--text-color-secondary);
}

.input-prefix,
.input-suffix {
  padding: 0 0.75rem;
  color: var(--text-color-secondary);
}

.form-hint {
  font-size: 0.75rem;
  color: var(--text-color-secondary);
  margin: 0;
}

.form-error {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
  color: var(--red-500);
  margin: 0;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
```

### Global Accessibility Styles
```css
/* assets/css/accessibility.css */

/* Skip link styles */
.sr-only:not(:focus):not(:active) {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

/* Focus visible for all interactive elements */
:focus-visible {
  outline: 2px solid var(--primary-color);
  outline-offset: 2px;
}

/* Remove focus ring for mouse users */
:focus:not(:focus-visible) {
  outline: none;
}

/* High contrast mode support */
@media (prefers-contrast: high) {
  :root {
    --surface-border: currentColor;
  }

  button,
  input,
  select,
  textarea {
    border: 2px solid currentColor;
  }
}

/* Reduced motion */
@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}

/* Ensure minimum touch target size */
button,
a,
input[type="checkbox"],
input[type="radio"],
select {
  min-height: 44px;
  min-width: 44px;
}

/* Link distinction */
a:not([class]) {
  text-decoration: underline;
  text-underline-offset: 2px;
}

a:not([class]):hover {
  text-decoration-thickness: 2px;
}
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 034 (Calendar Views) - calendar accessibility
- Story 036 (Contact List UI) - list accessibility

## Estimation
- **Complexity:** Medium-High
- **Components:** 5 components, 1 composable, CSS

## Notes
- Test with NVDA, VoiceOver, and JAWS
- Use axe-core for automated testing
- Follow WAI-ARIA Authoring Practices
- Calendar navigation follows grid pattern
- All images need alt text reviewed
- Color contrast should be verified with tools
