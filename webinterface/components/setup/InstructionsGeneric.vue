<template>
  <div class="space-y-6 pt-4">
    <p class="text-surface-600 dark:text-surface-400">
      Most CalDAV/CardDAV clients can connect using the following information.
      Refer to your client's documentation for specific setup steps.
    </p>

    <div class="bg-surface-50 dark:bg-surface-800 rounded-lg p-4 space-y-3">
      <h3 class="font-medium text-surface-900 dark:text-surface-0">Connection Settings</h3>

      <div class="grid gap-3">
        <div>
          <span class="text-sm text-surface-500 dark:text-surface-400">Server URL / Base URL:</span>
          <SetupCopyableCode :value="serverUrl" />
        </div>

        <div>
          <span class="text-sm text-surface-500 dark:text-surface-400">CalDAV URL (for calendars):</span>
          <SetupCopyableCode :value="caldavUrl" />
        </div>

        <div>
          <span class="text-sm text-surface-500 dark:text-surface-400">CardDAV URL (for contacts):</span>
          <SetupCopyableCode :value="carddavUrl" />
        </div>

        <div>
          <span class="text-sm text-surface-500 dark:text-surface-400">Username:</span>
          <SetupCopyableCode :value="username" />
        </div>

        <div>
          <span class="text-sm text-surface-500 dark:text-surface-400">Password:</span>
          <p class="text-sm text-surface-700 dark:text-surface-300">Your Credentials Password</p>
        </div>
      </div>
    </div>

    <div>
      <h3 class="font-medium text-surface-900 dark:text-surface-0 mb-3">Well-Known URLs</h3>
      <p class="text-sm text-surface-600 dark:text-surface-400 mb-2">
        Some clients use well-known URLs for auto-discovery:
      </p>
      <div class="space-y-2">
        <SetupCopyableCode :value="`${serverUrl}/.well-known/caldav`" />
        <SetupCopyableCode :value="`${serverUrl}/.well-known/carddav`" />
      </div>
    </div>

    <Divider />

    <div>
      <h3 class="font-medium text-surface-900 dark:text-surface-0 mb-3">Other Clients</h3>
      <ul class="space-y-2 text-sm text-surface-700 dark:text-surface-300">
        <li>
          <strong>Evolution (Linux):</strong>
          File &rarr; New &rarr; Calendar &rarr; CalDAV
        </li>
        <li>
          <strong>GNOME Calendar:</strong>
          Settings &rarr; Online Accounts &rarr; Other
        </li>
        <li>
          <strong>Windows 10/11 Mail:</strong>
          Add Account &rarr; iCloud (use CalDAV URL)
        </li>
        <li>
          <strong>eM Client:</strong>
          Menu &rarr; Accounts &rarr; Add Account &rarr; CalDAV/CardDAV
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  serverUrl: string;
  caldavUrl: string;
  carddavUrl: string;
  username: string;
}>();
</script>
