<template>
  <div class="space-y-6 pt-4">
    <Message severity="info" :closable="false">
      Thunderbird has built-in CalDAV support. For CardDAV (contacts), you'll need the
      <strong>TbSync</strong> and <strong>Provider for CalDAV &amp; CardDAV</strong> add-ons.
    </Message>

    <Tabs :value="activeTab" @update:value="activeTab = $event as string">
      <TabList>
        <Tab value="calendar">Calendars (CalDAV)</Tab>
        <Tab value="contacts">Contacts (CardDAV)</Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="calendar">
          <ol class="space-y-4 pt-4">
            <SetupStep :number="1" title="Open Calendar Tab">
              Click the <strong>Calendar</strong> icon in the left sidebar or press <code class="bg-surface-100 dark:bg-surface-800 px-1.5 py-0.5 rounded text-sm">Ctrl+Shift+C</code>.
            </SetupStep>

            <SetupStep :number="2" title="Create New Calendar">
              Right-click in the calendar list and select <strong>New Calendar</strong>.
            </SetupStep>

            <SetupStep :number="3" title="Select Network Calendar">
              Choose <strong>On the Network</strong> and click <strong>Next</strong>.
            </SetupStep>

            <SetupStep :number="4" title="Enter CalDAV URL">
              <div class="space-y-2">
                <p>Select <strong>CalDAV</strong> format and enter the URL:</p>
                <SetupCopyableCode :value="caldavUrl" />
                <p>
                  <strong>Username:</strong>
                  <code class="bg-surface-100 dark:bg-surface-800 px-2 py-1 rounded text-surface-900 dark:text-surface-0">{{ username }}</code>
                </p>
              </div>
            </SetupStep>

            <SetupStep :number="5" title="Authenticate">
              When prompted, enter your <strong>credentials password</strong> and check "Use Password Manager" to save it.
            </SetupStep>

            <SetupStep :number="6" title="Select Calendars">
              Choose which calendars to subscribe to and click <strong>Subscribe</strong>.
            </SetupStep>
          </ol>
        </TabPanel>

        <TabPanel value="contacts">
          <ol class="space-y-4 pt-4">
            <SetupStep :number="1" title="Install TbSync Add-ons">
              <p class="mb-2">Install two add-ons from the Thunderbird Add-on Manager:</p>
              <ul class="list-disc list-inside space-y-1">
                <li><strong>TbSync</strong></li>
                <li><strong>Provider for CalDAV &amp; CardDAV</strong></li>
              </ul>
            </SetupStep>

            <SetupStep :number="2" title="Open TbSync">
              Go to <strong>Tools</strong> &rarr; <strong>Synchronization Settings (TbSync)</strong>.
            </SetupStep>

            <SetupStep :number="3" title="Add Account">
              Click <strong>Account actions</strong> &rarr; <strong>Add new account</strong> &rarr; <strong>CalDAV &amp; CardDAV</strong>.
            </SetupStep>

            <SetupStep :number="4" title="Configure Connection">
              <div class="space-y-2">
                <p>Select <strong>Manual configuration</strong> and enter:</p>
                <p><strong>CardDAV server URL:</strong></p>
                <SetupCopyableCode :value="carddavUrl" />
                <p>
                  <strong>Username:</strong>
                  <code class="bg-surface-100 dark:bg-surface-800 px-2 py-1 rounded text-surface-900 dark:text-surface-0">{{ username }}</code>
                </p>
                <p><strong>Password:</strong> Your Credentials Password</p>
              </div>
            </SetupStep>

            <SetupStep :number="5" title="Enable Sync">
              Select the address books you want to sync and enable synchronization.
            </SetupStep>
          </ol>
        </TabPanel>
      </TabPanels>
    </Tabs>

  </div>
</template>

<script setup lang="ts">
defineProps<{
  caldavUrl: string;
  carddavUrl: string;
  username: string;
}>();

const activeTab = ref('calendar');
</script>
