# Story 048: Keyboard Navigation

## Story
**As a** power user or user who prefers keyboard
**I want to** navigate and perform actions using only the keyboard
**So that** I can work more efficiently without switching to the mouse

## Acceptance Criteria

### Global Shortcuts
- [ ] `Cmd/Ctrl + K` opens global search
- [ ] `Cmd/Ctrl + N` creates new event (on calendar) or contact (on contacts)
- [ ] `Cmd/Ctrl + S` saves current form
- [ ] `Escape` closes dialogs and cancels actions
- [ ] `?` shows keyboard shortcuts help (when not in input)
- [ ] `G + C` goes to calendar
- [ ] `G + A` goes to contacts (address book)
- [ ] `G + S` goes to settings
- [ ] `G + H` goes to home/dashboard

### Calendar Navigation
- [ ] Arrow keys navigate between days
- [ ] `T` jumps to today
- [ ] `M` switches to month view
- [ ] `W` switches to week view
- [ ] `D` switches to day view
- [ ] `[` / `]` navigates to previous/next period
- [ ] `Enter` opens selected day or event
- [ ] `Delete/Backspace` deletes selected event (with confirmation)

### Contact List Navigation
- [ ] `J/K` or `↓/↑` moves through contact list
- [ ] `Enter` opens selected contact
- [ ] `/` focuses search input
- [ ] Letter keys jump to contacts starting with that letter
- [ ] `E` edits selected contact
- [ ] `Delete` deletes selected contact (with confirmation)

### Form Navigation
- [ ] `Tab` moves between form fields
- [ ] `Shift + Tab` moves backwards
- [ ] `Enter` submits form (when appropriate)
- [ ] `Escape` cancels/closes form
- [ ] `Space` toggles checkboxes and buttons

### Visual Feedback
- [ ] Keyboard shortcuts displayed in tooltips
- [ ] Current focused element clearly visible
- [ ] Shortcut hints in menus and buttons
- [ ] Help modal lists all shortcuts

## Technical Details

### Keyboard Shortcuts Composable
```typescript
// composables/useKeyboardShortcuts.ts
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'

interface Shortcut {
  key: string
  modifiers?: ('ctrl' | 'meta' | 'shift' | 'alt')[]
  description: string
  action: () => void
  scope?: string
  when?: () => boolean
}

const registeredShortcuts = ref<Map<string, Shortcut>>(new Map())
const activeScopes = ref<Set<string>>(new Set(['global']))
const isInputFocused = ref(false)

export function useKeyboardShortcuts() {
  const router = useRouter()

  function getShortcutKey(shortcut: Shortcut): string {
    const parts = []
    if (shortcut.modifiers?.includes('ctrl')) parts.push('ctrl')
    if (shortcut.modifiers?.includes('meta')) parts.push('meta')
    if (shortcut.modifiers?.includes('shift')) parts.push('shift')
    if (shortcut.modifiers?.includes('alt')) parts.push('alt')
    parts.push(shortcut.key.toLowerCase())
    return parts.join('+')
  }

  function register(shortcut: Shortcut) {
    const key = getShortcutKey(shortcut)
    registeredShortcuts.value.set(key, {
      ...shortcut,
      scope: shortcut.scope || 'global'
    })
  }

  function unregister(shortcut: Partial<Shortcut>) {
    const key = getShortcutKey(shortcut as Shortcut)
    registeredShortcuts.value.delete(key)
  }

  function setScope(scope: string, active: boolean) {
    if (active) {
      activeScopes.value.add(scope)
    } else {
      activeScopes.value.delete(scope)
    }
  }

  function handleKeyDown(event: KeyboardEvent) {
    // Track input focus
    const target = event.target as HTMLElement
    isInputFocused.value = ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) ||
      target.isContentEditable

    // Build key string
    const parts = []
    if (event.ctrlKey) parts.push('ctrl')
    if (event.metaKey) parts.push('meta')
    if (event.shiftKey) parts.push('shift')
    if (event.altKey) parts.push('alt')
    parts.push(event.key.toLowerCase())
    const keyString = parts.join('+')

    // Find matching shortcut
    const shortcut = registeredShortcuts.value.get(keyString)
    if (!shortcut) return

    // Check scope
    if (shortcut.scope && !activeScopes.value.has(shortcut.scope)) return

    // Check condition
    if (shortcut.when && !shortcut.when()) return

    // Don't trigger shortcuts when typing in inputs (unless it has modifiers)
    if (isInputFocused.value && !shortcut.modifiers?.length) return

    event.preventDefault()
    shortcut.action()
  }

  // Register default global shortcuts
  function registerDefaults() {
    // Navigation shortcuts (G + key sequence)
    let gPressed = false
    let gTimeout: NodeJS.Timer | null = null

    register({
      key: 'g',
      description: 'Start go-to sequence',
      action: () => {
        gPressed = true
        gTimeout = setTimeout(() => { gPressed = false }, 1000)
      },
      when: () => !isInputFocused.value
    })

    register({
      key: 'c',
      description: 'Go to calendar',
      action: () => {
        if (gPressed) {
          router.push('/calendar')
          gPressed = false
        }
      },
      when: () => gPressed && !isInputFocused.value
    })

    register({
      key: 'a',
      description: 'Go to contacts',
      action: () => {
        if (gPressed) {
          router.push('/contacts')
          gPressed = false
        }
      },
      when: () => gPressed && !isInputFocused.value
    })

    register({
      key: 's',
      description: 'Go to settings',
      action: () => {
        if (gPressed) {
          router.push('/settings')
          gPressed = false
        }
      },
      when: () => gPressed && !isInputFocused.value
    })

    register({
      key: 'h',
      description: 'Go to home',
      action: () => {
        if (gPressed) {
          router.push('/')
          gPressed = false
        }
      },
      when: () => gPressed && !isInputFocused.value
    })

    // Help shortcut
    register({
      key: '?',
      modifiers: ['shift'],
      description: 'Show keyboard shortcuts',
      action: () => {
        window.dispatchEvent(new CustomEvent('show-shortcuts-help'))
      },
      when: () => !isInputFocused.value
    })
  }

  onMounted(() => {
    document.addEventListener('keydown', handleKeyDown)
    registerDefaults()
  })

  onUnmounted(() => {
    document.removeEventListener('keydown', handleKeyDown)
  })

  return {
    register,
    unregister,
    setScope,
    registeredShortcuts,
    activeScopes,
    isInputFocused
  }
}
```

### Keyboard Shortcuts Help Dialog
```vue
<!-- components/common/KeyboardShortcutsHelp.vue -->
<template>
  <Dialog
    v-model:visible="visible"
    header="Keyboard Shortcuts"
    :modal="true"
    :style="{ width: '600px', maxHeight: '80vh' }"
    class="shortcuts-dialog"
  >
    <div class="shortcuts-content">
      <div
        v-for="group in shortcutGroups"
        :key="group.name"
        class="shortcut-group"
      >
        <h3>{{ group.name }}</h3>
        <div class="shortcut-list">
          <div
            v-for="shortcut in group.shortcuts"
            :key="shortcut.keys"
            class="shortcut-item"
          >
            <span class="shortcut-description">{{ shortcut.description }}</span>
            <kbd class="shortcut-keys">
              <template v-for="(key, index) in shortcut.keys.split('+')" :key="key">
                <span v-if="index > 0" class="plus">+</span>
                <span class="key">{{ formatKey(key) }}</span>
              </template>
            </kbd>
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <p class="shortcut-tip">
        Press <kbd>?</kbd> anywhere to show this help
      </p>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const visible = ref(false)

const shortcutGroups = [
  {
    name: 'Global',
    shortcuts: [
      { keys: 'mod+k', description: 'Open search' },
      { keys: 'mod+n', description: 'Create new item' },
      { keys: 'mod+s', description: 'Save' },
      { keys: 'esc', description: 'Close dialog / Cancel' },
      { keys: '?', description: 'Show this help' }
    ]
  },
  {
    name: 'Navigation',
    shortcuts: [
      { keys: 'g then h', description: 'Go to Home' },
      { keys: 'g then c', description: 'Go to Calendar' },
      { keys: 'g then a', description: 'Go to Contacts' },
      { keys: 'g then s', description: 'Go to Settings' }
    ]
  },
  {
    name: 'Calendar',
    shortcuts: [
      { keys: 't', description: 'Jump to today' },
      { keys: 'm', description: 'Month view' },
      { keys: 'w', description: 'Week view' },
      { keys: 'd', description: 'Day view' },
      { keys: '[', description: 'Previous period' },
      { keys: ']', description: 'Next period' },
      { keys: '←↑↓→', description: 'Navigate days' },
      { keys: 'enter', description: 'Open selected event' },
      { keys: 'delete', description: 'Delete selected event' }
    ]
  },
  {
    name: 'Contacts',
    shortcuts: [
      { keys: 'j', description: 'Next contact' },
      { keys: 'k', description: 'Previous contact' },
      { keys: '/', description: 'Focus search' },
      { keys: 'enter', description: 'Open selected contact' },
      { keys: 'e', description: 'Edit selected contact' },
      { keys: 'delete', description: 'Delete selected contact' }
    ]
  }
]

function formatKey(key: string): string {
  const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0
  const keyMap: Record<string, string> = {
    mod: isMac ? '⌘' : 'Ctrl',
    ctrl: isMac ? '⌃' : 'Ctrl',
    alt: isMac ? '⌥' : 'Alt',
    shift: '⇧',
    enter: '↵',
    esc: 'Esc',
    delete: isMac ? '⌫' : 'Del',
    tab: '⇥',
    space: '␣'
  }
  return keyMap[key.toLowerCase()] || key.toUpperCase()
}

function handleShowShortcuts() {
  visible.value = true
}

onMounted(() => {
  window.addEventListener('show-shortcuts-help', handleShowShortcuts)
})

onUnmounted(() => {
  window.removeEventListener('show-shortcuts-help', handleShowShortcuts)
})
</script>

<style scoped>
.shortcuts-content {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
}

.shortcut-group h3 {
  margin: 0 0 0.75rem;
  font-size: 0.875rem;
  font-weight: 600;
  text-transform: uppercase;
  color: var(--text-color-secondary);
}

.shortcut-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.shortcut-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.5rem 0;
}

.shortcut-description {
  color: var(--text-color);
}

.shortcut-keys {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}

.shortcut-keys .key {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 24px;
  padding: 0.25rem 0.5rem;
  background: var(--surface-ground);
  border: 1px solid var(--surface-border);
  border-radius: 4px;
  font-family: inherit;
  font-size: 0.75rem;
  font-weight: 500;
}

.shortcut-keys .plus {
  color: var(--text-color-secondary);
  font-size: 0.75rem;
}

.shortcut-tip {
  text-align: center;
  color: var(--text-color-secondary);
  font-size: 0.875rem;
  margin: 0;
}

.shortcut-tip kbd {
  background: var(--surface-ground);
  border: 1px solid var(--surface-border);
  border-radius: 4px;
  padding: 0.125rem 0.375rem;
  font-family: inherit;
  font-size: 0.75rem;
}
</style>
```

### Calendar Keyboard Navigation
```vue
<!-- components/calendar/CalendarKeyboardNav.vue -->
<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useKeyboardShortcuts } from '~/composables/useKeyboardShortcuts'
import { useCalendarStore } from '~/stores/calendars'
import { addDays, addWeeks, addMonths, startOfWeek, endOfWeek } from 'date-fns'

const props = defineProps<{
  selectedDate: Date
  view: 'month' | 'week' | 'day'
}>()

const emit = defineEmits([
  'update:selectedDate',
  'update:view',
  'navigate',
  'openEvent',
  'deleteEvent'
])

const { register, unregister, setScope } = useKeyboardShortcuts()
const calendarStore = useCalendarStore()

const focusedEventIndex = ref(-1)

const eventsOnSelectedDate = computed(() => {
  return calendarStore.getEventsForDate(props.selectedDate)
})

// Navigation shortcuts
onMounted(() => {
  setScope('calendar', true)

  register({
    key: 'ArrowLeft',
    description: 'Previous day',
    scope: 'calendar',
    action: () => {
      emit('update:selectedDate', addDays(props.selectedDate, -1))
    }
  })

  register({
    key: 'ArrowRight',
    description: 'Next day',
    scope: 'calendar',
    action: () => {
      emit('update:selectedDate', addDays(props.selectedDate, 1))
    }
  })

  register({
    key: 'ArrowUp',
    description: 'Previous week',
    scope: 'calendar',
    action: () => {
      if (focusedEventIndex.value > 0) {
        focusedEventIndex.value--
      } else {
        emit('update:selectedDate', addWeeks(props.selectedDate, -1))
      }
    }
  })

  register({
    key: 'ArrowDown',
    description: 'Next week',
    scope: 'calendar',
    action: () => {
      if (focusedEventIndex.value < eventsOnSelectedDate.value.length - 1) {
        focusedEventIndex.value++
      } else {
        emit('update:selectedDate', addWeeks(props.selectedDate, 1))
      }
    }
  })

  register({
    key: 't',
    description: 'Today',
    scope: 'calendar',
    action: () => {
      emit('update:selectedDate', new Date())
    }
  })

  register({
    key: 'm',
    description: 'Month view',
    scope: 'calendar',
    action: () => {
      emit('update:view', 'month')
    }
  })

  register({
    key: 'w',
    description: 'Week view',
    scope: 'calendar',
    action: () => {
      emit('update:view', 'week')
    }
  })

  register({
    key: 'd',
    description: 'Day view',
    scope: 'calendar',
    action: () => {
      emit('update:view', 'day')
    }
  })

  register({
    key: '[',
    description: 'Previous period',
    scope: 'calendar',
    action: () => {
      const amount = props.view === 'month' ? 1 : props.view === 'week' ? 1 : 1
      const fn = props.view === 'month' ? addMonths : props.view === 'week' ? addWeeks : addDays
      emit('update:selectedDate', fn(props.selectedDate, -amount))
    }
  })

  register({
    key: ']',
    description: 'Next period',
    scope: 'calendar',
    action: () => {
      const amount = props.view === 'month' ? 1 : props.view === 'week' ? 1 : 1
      const fn = props.view === 'month' ? addMonths : props.view === 'week' ? addWeeks : addDays
      emit('update:selectedDate', fn(props.selectedDate, amount))
    }
  })

  register({
    key: 'Home',
    description: 'Start of week',
    scope: 'calendar',
    action: () => {
      emit('update:selectedDate', startOfWeek(props.selectedDate))
    }
  })

  register({
    key: 'End',
    description: 'End of week',
    scope: 'calendar',
    action: () => {
      emit('update:selectedDate', endOfWeek(props.selectedDate))
    }
  })

  register({
    key: 'Enter',
    description: 'Open event',
    scope: 'calendar',
    action: () => {
      if (focusedEventIndex.value >= 0) {
        emit('openEvent', eventsOnSelectedDate.value[focusedEventIndex.value])
      }
    }
  })

  register({
    key: 'Delete',
    description: 'Delete event',
    scope: 'calendar',
    action: () => {
      if (focusedEventIndex.value >= 0) {
        emit('deleteEvent', eventsOnSelectedDate.value[focusedEventIndex.value])
      }
    }
  })

  register({
    key: 'Backspace',
    description: 'Delete event',
    scope: 'calendar',
    action: () => {
      if (focusedEventIndex.value >= 0) {
        emit('deleteEvent', eventsOnSelectedDate.value[focusedEventIndex.value])
      }
    }
  })

  register({
    key: 'n',
    modifiers: ['meta'],
    description: 'New event',
    scope: 'calendar',
    action: () => {
      emit('openEvent', null) // null means create new
    }
  })

  register({
    key: 'n',
    modifiers: ['ctrl'],
    description: 'New event',
    scope: 'calendar',
    action: () => {
      emit('openEvent', null)
    }
  })
})

onUnmounted(() => {
  setScope('calendar', false)
})

// Reset focused event when date changes
watch(() => props.selectedDate, () => {
  focusedEventIndex.value = -1
})

// Expose for parent component
defineExpose({
  focusedEventIndex
})
</script>

<template>
  <!-- This is a renderless component that just handles keyboard navigation -->
  <slot :focusedEventIndex="focusedEventIndex" />
</template>
```

### Contact List Keyboard Navigation
```typescript
// composables/useListKeyboardNav.ts
import { ref, computed, watch } from 'vue'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'

interface UseListKeyboardNavOptions {
  items: Ref<any[]>
  scope: string
  onSelect?: (item: any) => void
  onEdit?: (item: any) => void
  onDelete?: (item: any) => void
  getItemId?: (item: any) => string
}

export function useListKeyboardNav(options: UseListKeyboardNavOptions) {
  const { register, setScope } = useKeyboardShortcuts()

  const selectedIndex = ref(-1)
  const selectedItem = computed(() =>
    selectedIndex.value >= 0 ? options.items.value[selectedIndex.value] : null
  )

  function selectNext() {
    if (selectedIndex.value < options.items.value.length - 1) {
      selectedIndex.value++
      scrollToSelected()
    }
  }

  function selectPrevious() {
    if (selectedIndex.value > 0) {
      selectedIndex.value--
      scrollToSelected()
    }
  }

  function selectFirst() {
    if (options.items.value.length > 0) {
      selectedIndex.value = 0
      scrollToSelected()
    }
  }

  function selectLast() {
    if (options.items.value.length > 0) {
      selectedIndex.value = options.items.value.length - 1
      scrollToSelected()
    }
  }

  function scrollToSelected() {
    if (selectedItem.value && options.getItemId) {
      const id = options.getItemId(selectedItem.value)
      const element = document.getElementById(`list-item-${id}`)
      element?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }

  // Register shortcuts
  onMounted(() => {
    setScope(options.scope, true)

    register({
      key: 'j',
      description: 'Next item',
      scope: options.scope,
      action: selectNext
    })

    register({
      key: 'ArrowDown',
      description: 'Next item',
      scope: options.scope,
      action: selectNext
    })

    register({
      key: 'k',
      description: 'Previous item',
      scope: options.scope,
      action: selectPrevious
    })

    register({
      key: 'ArrowUp',
      description: 'Previous item',
      scope: options.scope,
      action: selectPrevious
    })

    register({
      key: 'Home',
      description: 'First item',
      scope: options.scope,
      action: selectFirst
    })

    register({
      key: 'End',
      description: 'Last item',
      scope: options.scope,
      action: selectLast
    })

    register({
      key: 'Enter',
      description: 'Open item',
      scope: options.scope,
      action: () => {
        if (selectedItem.value && options.onSelect) {
          options.onSelect(selectedItem.value)
        }
      }
    })

    register({
      key: 'e',
      description: 'Edit item',
      scope: options.scope,
      action: () => {
        if (selectedItem.value && options.onEdit) {
          options.onEdit(selectedItem.value)
        }
      }
    })

    register({
      key: 'Delete',
      description: 'Delete item',
      scope: options.scope,
      action: () => {
        if (selectedItem.value && options.onDelete) {
          options.onDelete(selectedItem.value)
        }
      }
    })
  })

  onUnmounted(() => {
    setScope(options.scope, false)
  })

  // Reset selection when items change
  watch(() => options.items.value, () => {
    if (selectedIndex.value >= options.items.value.length) {
      selectedIndex.value = Math.max(0, options.items.value.length - 1)
    }
  })

  return {
    selectedIndex,
    selectedItem,
    selectNext,
    selectPrevious,
    selectFirst,
    selectLast
  }
}
```

## Dependencies
- Story 031 (Frontend Project Setup)
- Story 034 (Calendar Views)
- Story 036 (Contact List UI)
- Story 044 (Global Search) - search shortcut

## Estimation
- **Complexity:** Medium
- **Components:** 2 components, 2 composables

## Notes
- Shortcuts should not conflict with browser defaults
- Mac uses Cmd, Windows/Linux uses Ctrl
- Focus management is critical for accessibility
- Consider vim-style shortcuts for power users
- Shortcuts should be discoverable (tooltips, help modal)
- Test with various keyboard layouts
