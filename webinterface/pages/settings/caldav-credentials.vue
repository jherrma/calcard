<template>
  <SettingsDavCredentialManager
    title="CalDAV Credentials"
    description="Manage credentials for CalDAV clients like Thunderbird, Apple Calendar, or DAVx5 to sync your calendars."
    api-endpoint="/api/v1/caldav-credentials"
    icon="pi pi-calendar"
  />
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});
</script>
