<template>
  <SettingsDavCredentialManager
    title="CardDAV Credentials"
    description="Manage credentials for CardDAV clients like Thunderbird, Apple Contacts, or DAVx5 to sync your contacts."
    api-endpoint="/api/v1/carddav-credentials"
    icon="pi pi-id-card"
  />
</template>

<script setup lang="ts">
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});
</script>
