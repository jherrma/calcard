<template>
  <div ref="rootRef" class="relative">
    <button
      ref="triggerRef"
      type="button"
      class="p-2 rounded-full text-surface-500 hover:text-surface-700 dark:hover:text-surface-200 hover:bg-surface-100 dark:hover:bg-surface-800 transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500"
      :aria-label="triggerLabel"
      aria-haspopup="menu"
      :aria-expanded="open"
      @click="open ? closeMenu() : openMenu()"
      @keydown.down.prevent="openMenu()"
    >
      <i :class="[triggerIcon, 'text-lg block']" aria-hidden="true" />
    </button>

    <Transition
      enter-active-class="transition ease-out duration-100"
      enter-from-class="transform opacity-0 scale-95"
      enter-to-class="transform opacity-100 scale-100"
      leave-active-class="transition ease-in duration-75"
      leave-from-class="transform opacity-100 scale-100"
      leave-to-class="transform opacity-0 scale-95"
    >
      <div
        v-if="open"
        role="menu"
        aria-label="Theme"
        class="absolute right-0 mt-2 w-44 bg-white dark:bg-surface-900 rounded-lg shadow-lg border border-surface-200 dark:border-surface-700 py-1 z-30"
      >
        <button
          v-for="(option, index) in THEME_OPTIONS"
          :key="option.value"
          :ref="el => setItemRef(el, index)"
          type="button"
          role="menuitemradio"
          :aria-checked="option.value === themeMode"
          class="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-surface-700 dark:text-surface-200 hover:bg-surface-100 dark:hover:bg-surface-800 focus:outline-none focus:bg-surface-100 dark:focus:bg-surface-800"
          @click="choose(option.value)"
          @keydown="onItemKeydown($event, index)"
        >
          <i :class="[option.icon, 'text-surface-500']" aria-hidden="true" />
          <span class="flex-1 text-left">{{ option.label }}</span>
          <i
            v-if="option.value === themeMode"
            class="pi pi-check text-xs text-primary-600 dark:text-primary-400"
            aria-hidden="true"
          />
        </button>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import type { ComponentPublicInstance } from 'vue';
import type { ThemeMode } from '~/utils/theme';

/**
 * Light / dark / system switcher for the app shells (story 046).
 *
 * Hand-rolled rather than a PrimeVue `Menu` for two reasons: it matches the user
 * menu sitting next to it in AppHeader, and the three options are a radio group
 * (`menuitemradio` + `aria-checked`) rather than a list of commands — which a
 * screen reader needs in order to announce which theme is already active.
 */

const { themeMode, isDark, setTheme } = useTheme();

const open = ref(false);
const rootRef = ref<HTMLElement | null>(null);
const triggerRef = ref<HTMLButtonElement | null>(null);

// Plain array, not a ref: it is only ever read imperatively to move focus, and
// the option list is static, so there is nothing for reactivity to do.
const itemRefs: (HTMLButtonElement | null)[] = [];
const setItemRef = (el: Element | ComponentPublicInstance | null, index: number) => {
  itemRefs[index] = (el as HTMLButtonElement | null) ?? null;
};

const currentOption = computed(
  () => THEME_OPTIONS.find(o => o.value === themeMode.value) ?? THEME_OPTIONS[0]!,
);

/**
 * In `system` mode the icon names the mode rather than the outcome, because
 * that is the fact the user cannot otherwise recover: whether it is currently
 * dark is visible from the page itself, whether it will follow the device is not.
 */
const triggerIcon = computed(() => {
  if (themeMode.value === 'system') return 'pi pi-desktop';
  return isDark.value ? 'pi pi-moon' : 'pi pi-sun';
});

// Spelled out for screen readers, which get no icon and no tooltip.
const triggerLabel = computed(() => `Theme: ${currentOption.value.label}. Change theme`);

const focusItem = (index: number) => {
  const count = THEME_OPTIONS.length;
  const target = ((index % count) + count) % count;
  itemRefs[target]?.focus();
};

const openMenu = async () => {
  open.value = true;
  await nextTick();
  // Land on the active option so Enter re-selects it rather than silently
  // changing the theme out from under someone arrowing through.
  focusItem(THEME_OPTIONS.findIndex(o => o.value === themeMode.value));
};

const closeMenu = (returnFocus = false) => {
  open.value = false;
  if (returnFocus) triggerRef.value?.focus();
};

const choose = (mode: ThemeMode) => {
  setTheme(mode);
  closeMenu(true);
};

const onItemKeydown = (event: KeyboardEvent, index: number) => {
  switch (event.key) {
    case 'ArrowDown':
      event.preventDefault();
      focusItem(index + 1);
      break;
    case 'ArrowUp':
      event.preventDefault();
      focusItem(index - 1);
      break;
    case 'Home':
      event.preventDefault();
      focusItem(0);
      break;
    case 'End':
      event.preventDefault();
      focusItem(THEME_OPTIONS.length - 1);
      break;
    case 'Escape':
      event.preventDefault();
      closeMenu(true);
      break;
    case 'Tab':
      // Let focus leave normally, but do not leave an orphaned menu behind it.
      closeMenu();
      break;
  }
};

const handleClickOutside = (event: MouseEvent) => {
  if (rootRef.value && !rootRef.value.contains(event.target as Node)) {
    open.value = false;
  }
};

onMounted(() => {
  document.addEventListener('click', handleClickOutside);
});

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside);
});
</script>
