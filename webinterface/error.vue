<template>
  <div class="min-h-screen flex items-center justify-center bg-surface-50 dark:bg-surface-950">
    <div class="text-center p-8">
      <h1 class="text-8xl font-bold text-surface-200 dark:text-surface-800 mb-4">
        {{ error?.statusCode || 500 }}
      </h1>
      <h2 class="text-3xl font-bold text-surface-900 dark:text-surface-0 mb-4">
        {{ error?.statusCode === 404 ? 'Page Not Found' : 'Something went wrong' }}
      </h2>
      <p class="text-surface-600 dark:text-surface-400 mb-8 max-w-md mx-auto">
        {{ error?.message || 'We encountered an unexpected error. Please try again or return to the dashboard.' }}
      </p>
      <div class="flex justify-center gap-4">
        <Button
          label="Go Home"
          icon="pi pi-home"
          @click="handleError"
        />
        <Button
          label="Go Back"
          icon="pi pi-arrow-left"
          severity="secondary"
          outlined
          @click="router.back()"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  error: {
    statusCode?: number;
    message?: string;
  };
}>();

const router = useRouter();

const handleError = () => {
  clearError({ redirect: '/' });
};
</script>
