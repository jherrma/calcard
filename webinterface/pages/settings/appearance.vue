<template>
  <div>
    <h2 class="text-2xl font-bold text-surface-900 dark:text-surface-0 mb-6">Appearance</h2>

    <div
      class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-6 space-y-6"
    >
      <div class="flex flex-col gap-2">
        <label id="theme-mode-label" class="text-sm font-medium text-surface-700 dark:text-surface-300">
          Theme
        </label>
        <SelectButton
          v-model="themeMode"
          :options="THEME_OPTIONS"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="theme-mode-label"
        />
        <small class="text-surface-500">{{ currentOption.hint }}</small>
      </div>

      <!--
        Only meaningful under `system`, where the mode alone does not tell you
        what you are looking at.
      -->
      <div
        v-if="themeMode === 'system'"
        class="flex items-center gap-2 text-sm text-surface-600 dark:text-surface-400"
      >
        <i :class="[isDark ? 'pi pi-moon' : 'pi pi-sun', 'text-surface-500']" aria-hidden="true" />
        <span>Your device is currently asking for the {{ isDark ? 'dark' : 'light' }} theme.</span>
      </div>

      <p class="text-sm text-surface-500 border-t border-surface-200 dark:border-surface-800 pt-4">
        There is no Save button — the theme changes as you pick it. It is remembered in this
        browser rather than on your account, so each device you sign in from keeps its own
        setting.
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Story 046. The header toggle is the fast path; this page exists because
 * Settings is where people go looking for a preference, and because it has room
 * to say what the header cannot: which theme `system` currently resolves to, and
 * that the choice does not follow the account across devices.
 */
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const { themeMode, isDark } = useTheme();

const currentOption = computed(
  () => THEME_OPTIONS.find(o => o.value === themeMode.value) ?? THEME_OPTIONS[0]!,
);
</script>
