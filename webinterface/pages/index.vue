<template>
  <div />
</template>

<script setup lang="ts">
definePageMeta({ middleware: 'auth' });
// The app's home is the calendar; "/" exists only as an entry point. Redirect
// during setup (before the page renders) so no placeholder flashes; the `auth`
// middleware still sends logged-out visitors to /auth/login first.
await navigateTo('/calendar', { replace: true });
</script>
