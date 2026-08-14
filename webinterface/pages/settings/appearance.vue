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
        <small class="text-surface-500 dark:text-surface-400">{{ currentOption.hint }}</small>
      </div>

      <!--
        Only meaningful under `system`, where the mode alone does not tell you
        what you are looking at.
      -->
      <div
        v-if="themeMode === 'system'"
        class="flex items-center gap-2 text-sm text-surface-600 dark:text-surface-400"
      >
        <i :class="[isDark ? 'pi pi-moon' : 'pi pi-sun', 'text-surface-500 dark:text-surface-400']" aria-hidden="true" />
        <span>Your device is currently asking for the {{ isDark ? 'dark' : 'light' }} theme.</span>
      </div>

      <p class="text-sm text-surface-500 dark:text-surface-400 border-t border-surface-200 dark:border-surface-800 pt-4">
        There is no Save button — the theme changes as you pick it. It is remembered in this
        browser rather than on your account, so each device you sign in from keeps its own
        setting.
      </p>
    </div>

    <!-- Accent colour -->
    <div
      class="mt-6 bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-6 space-y-6"
    >
      <div class="flex flex-col gap-3">
        <span id="accent-label" class="text-sm font-medium text-surface-700 dark:text-surface-300">
          Accent colour
        </span>

        <div role="radiogroup" aria-labelledby="accent-label" class="flex flex-wrap gap-2">
          <button
            v-for="preset in ACCENT_PRESETS"
            :key="preset.value"
            type="button"
            role="radio"
            :aria-checked="preset.value === accentColor"
            :aria-label="preset.name"
            :disabled="saving"
            class="w-9 h-9 rounded-full flex items-center justify-center transition-transform hover:scale-110 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-surface-400 dark:focus:ring-offset-surface-900 disabled:opacity-50"
            :style="{ backgroundColor: preset.value }"
            @click="choose(preset.value)"
          >
            <!-- The tick is the only non-colour cue, so it must not be the ONLY
                 cue: aria-checked above carries it for anyone who cannot see
                 either the swatch or the mark on it. -->
            <i v-if="preset.value === accentColor" class="pi pi-check text-white text-sm" aria-hidden="true" />
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-2">
        <label for="accent-hex" class="text-sm font-medium text-surface-700 dark:text-surface-300">
          Custom colour
        </label>
        <div class="flex items-center gap-3">
          <ColorPicker
            v-model="pickerValue"
            input-id="accent-picker"
            :disabled="saving"
            aria-label="Pick a custom accent colour"
            @update:model-value="onPickerChange"
          />
          <InputText
            id="accent-hex"
            v-model="hexInput"
            class="w-36 font-mono"
            placeholder="#3b82f6"
            :invalid="hexInput !== '' && normalizeAccentColor(hexInput) === null"
            :disabled="saving"
            @keyup.enter="applyHexInput"
            @blur="applyHexInput"
          />
          <Button
            v-if="!isDefaultAccent"
            type="button"
            label="Reset"
            severity="secondary"
            text
            :disabled="saving"
            @click="choose(DEFAULT_ACCENT_COLOR)"
          />
        </div>
        <small class="text-surface-500 dark:text-surface-400">
          Six hex digits, with or without the <code>#</code>. Unlike the theme, this is saved to
          your account and follows you to every device.
        </small>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Story 046. The header toggle is the fast path; this page exists because
 * Settings is where people go looking for a preference, because it has room to
 * say what the header cannot (which theme `system` currently resolves to), and
 * because the accent colour has no home in the header at all.
 *
 * The two settings persist DIFFERENTLY and the copy says so: the theme is
 * device-local, the accent is a server preference on the account.
 */
import { usePreferencesStore } from '~/stores/preferences';

definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

const { themeMode, isDark } = useTheme();
const { accentColor, isDefaultAccent, setAccentColor } = useAccentColor();
const preferencesStore = usePreferencesStore();
const toast = useAppToast();

const currentOption = computed(
  () => THEME_OPTIONS.find(o => o.value === themeMode.value) ?? THEME_OPTIONS[0]!,
);

const saving = ref(false);
// PrimeVue's ColorPicker works in bare hex, with no leading '#'.
const pickerValue = ref(accentColor.value.slice(1));
const hexInput = ref(accentColor.value);

// The accent can change from under this page — the preferences load resolving,
// or another tab — so the two inputs follow the applied value rather than only
// being seeded from it.
watch(accentColor, (hex) => {
  pickerValue.value = hex.slice(1);
  hexInput.value = hex;
});

async function choose(input: string) {
  const hex = normalizeAccentColor(input);
  // Re-picking the current colour would otherwise spend a request to write the
  // value that is already stored.
  if (!hex || hex === accentColor.value) return;

  saving.value = true;
  try {
    await setAccentColor(hex);
    toast.success('Accent colour saved');
  } catch (e: any) {
    // setAccentColor has already rolled the UI back to the stored colour, so
    // what is on screen still matches what the server holds.
    toast.error(e?.data?.message || preferencesStore.error || 'Failed to save the accent colour');
  } finally {
    saving.value = false;
  }
}

function onPickerChange(value: string | undefined) {
  if (value) void choose(`#${value}`);
}

function applyHexInput() {
  const hex = normalizeAccentColor(hexInput.value);
  if (!hex) {
    // Snap back rather than leave an unparseable string sitting in a field that
    // no longer describes what the user is looking at.
    hexInput.value = accentColor.value;
    return;
  }
  void choose(hex);
}

// The accent lives on the server, so this page needs the preference map even if
// nothing else on the route has asked for it yet. ensureLoaded never rejects.
onMounted(() => {
  void preferencesStore.ensureLoaded();
});
</script>
