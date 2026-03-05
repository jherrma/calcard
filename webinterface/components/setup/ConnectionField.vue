<template>
  <div>
    <label class="block text-sm font-medium text-surface-500 dark:text-surface-400 mb-1">{{ label }}</label>
    <div class="flex gap-2">
      <InputText :model-value="value" readonly class="flex-1 font-mono text-sm" />
      <Button icon="pi pi-copy" severity="secondary" @click="copy" />
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  label: string;
  value: string;
}>();

const toast = useAppToast();

const copy = async () => {
  await navigator.clipboard.writeText(props.value);
  toast.success(`${props.label} copied`);
};
</script>
