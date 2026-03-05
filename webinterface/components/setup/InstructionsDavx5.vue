<template>
  <div class="space-y-6 pt-4">
    <Message severity="info" :closable="false">
      DAVx5 is the recommended app for syncing calendars and contacts on Android.
    </Message>

    <div class="flex flex-wrap gap-3 mb-6">
      <a
        href="https://play.google.com/store/apps/details?id=at.bitfire.davdroid"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex items-center gap-2 px-4 py-2 bg-surface-100 dark:bg-surface-800 rounded-lg hover:bg-surface-200 dark:hover:bg-surface-700 transition-colors text-surface-700 dark:text-surface-200"
      >
        <i class="pi pi-google text-lg" />
        <span>Google Play</span>
      </a>
      <a
        href="https://f-droid.org/packages/at.bitfire.davdroid/"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex items-center gap-2 px-4 py-2 bg-surface-100 dark:bg-surface-800 rounded-lg hover:bg-surface-200 dark:hover:bg-surface-700 transition-colors text-surface-700 dark:text-surface-200"
      >
        <i class="pi pi-android text-lg" />
        <span>F-Droid</span>
      </a>
    </div>

    <ol class="space-y-4">
      <SetupStep :number="1" title="Install DAVx5">
        Download and install DAVx5 from Google Play or F-Droid.
      </SetupStep>

      <SetupStep :number="2" title="Add Account">
        Open DAVx5 and tap the <strong>+</strong> button to add a new account.
      </SetupStep>

      <SetupStep :number="3" title="Select Login Type">
        Choose <strong>"Login with URL and user name"</strong>.
      </SetupStep>

      <SetupStep :number="4" title="Enter Server URL">
        <p class="mb-2">Enter the following URL:</p>
        <SetupCopyableCode :value="serverUrl" />
      </SetupStep>

      <SetupStep :number="5" title="Enter Credentials">
        <p class="mb-2">
          <strong>Username:</strong>
          <code class="bg-surface-100 dark:bg-surface-800 px-2 py-1 rounded ml-2 text-surface-900 dark:text-surface-0">{{ username }}</code>
        </p>
        <p>
          <strong>Password:</strong> Your Credentials Password
        </p>
      </SetupStep>

      <SetupStep :number="6" title="Select Data to Sync">
        Choose which calendars and address books to sync with your device.
      </SetupStep>

      <SetupStep :number="7" title="Start Sync">
        Tap the sync button or wait for automatic synchronization.
      </SetupStep>
    </ol>

    <Divider />

    <div>
      <h3 class="font-medium text-surface-900 dark:text-surface-0 mb-2">Quick Setup with QR Code</h3>
      <p class="text-sm text-surface-600 dark:text-surface-400">
        In DAVx5, you can also scan the QR code shown above to automatically fill in the server URL.
        You'll still need to enter your username and credentials password.
      </p>
    </div>

    <Divider />

    <div>
      <h3 class="font-medium text-surface-900 dark:text-surface-0 mb-3">Troubleshooting</h3>
      <Accordion :value="[]" multiple>
        <AccordionPanel value="connection">
          <AccordionHeader>Connection failed</AccordionHeader>
          <AccordionContent>
            <ul class="list-disc list-inside text-sm space-y-1 text-surface-600 dark:text-surface-400">
              <li>Make sure you're using the correct credentials password</li>
              <li>Check that the server URL is correct</li>
              <li>Verify your internet connection</li>
              <li>If using self-signed certificates, accept the certificate in DAVx5</li>
            </ul>
          </AccordionContent>
        </AccordionPanel>
        <AccordionPanel value="sync">
          <AccordionHeader>Calendars/Contacts not syncing</AccordionHeader>
          <AccordionContent>
            <ul class="list-disc list-inside text-sm space-y-1 text-surface-600 dark:text-surface-400">
              <li>Check that the calendars/address books are selected in DAVx5</li>
              <li>Try a manual sync by tapping the sync button</li>
              <li>Check Android's battery optimization settings for DAVx5</li>
            </ul>
          </AccordionContent>
        </AccordionPanel>
      </Accordion>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  serverUrl: string;
  username: string;
}>();
</script>
