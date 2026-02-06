<template>
  <div class="min-h-screen bg-surface-50 dark:bg-surface-950 flex flex-col justify-center py-12 sm:px-6 lg:px-8">
    <div class="sm:mx-auto sm:w-full sm:max-w-md text-center">
      <h1 class="text-4xl font-bold text-primary-600 dark:text-primary-400 mb-2">CalCard</h1>
      <p class="text-surface-600 dark:text-surface-400">Sync your life, beautifully.</p>
    </div>

    <div class="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
      <div class="bg-surface-0 dark:bg-surface-900 py-8 px-4 shadow-lg border border-surface-200 dark:border-surface-800 sm:rounded-2xl sm:px-10">
        <slot />
      </div>
    </div>

    <div class="mt-8 text-center text-sm text-surface-500 dark:text-surface-500">
      &copy; {{ new Date().getFullYear() }} CalCard. All rights reserved.
    </div>
  </div>
</template>

<style scoped>
/* Glassmorphism effect if desired can be added here */
</style>
