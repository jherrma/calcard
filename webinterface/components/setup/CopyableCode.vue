<template>
  <div class="flex items-center gap-2 bg-surface-100 dark:bg-surface-800 rounded-lg p-2">
    <code class="flex-1 text-sm font-mono break-all text-surface-900 dark:text-surface-0">{{ value }}</code>
    <Button
      icon="pi pi-copy"
      severity="secondary"
      text
      size="small"
      @click="copy"
    />
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  value: string;
}>();

const toast = useAppToast();

const copy = async () => {
  await navigator.clipboard.writeText(props.value);
  toast.success('Copied to clipboard');
};
</script>
