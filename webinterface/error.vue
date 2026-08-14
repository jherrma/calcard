<template>
  <div class="min-h-screen flex items-center justify-center bg-surface-50 dark:bg-surface-950">
    <div class="text-center p-8">
      <!--
        surface-500 rather than the 200/800 this used to be: at 1.2:1 light and
        1.3:1 dark the status code was not de-emphasized, it was invisible. The
        heading below still carries the meaning, but the number is the part
        someone reads out when reporting a problem (story 046 contrast pass).
      -->
      <h1 class="text-8xl font-bold text-surface-500 dark:text-surface-500 mb-4">
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
