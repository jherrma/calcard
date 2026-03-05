<template>
  <div class="space-y-6 pt-4">
    <Tabs :value="activeTab" @update:value="activeTab = $event as string">
      <TabList>
        <Tab value="ios">iOS (iPhone/iPad)</Tab>
        <Tab value="macos">macOS</Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="ios">
          <ol class="space-y-4 pt-4">
            <SetupStep :number="1" title="Open Settings">
              Go to <strong>Settings</strong> &rarr; <strong>Calendar</strong> &rarr; <strong>Accounts</strong>
              (or <strong>Settings</strong> &rarr; <strong>Contacts</strong> &rarr; <strong>Accounts</strong> for contacts).
            </SetupStep>

            <SetupStep :number="2" title="Add Account">
              Tap <strong>Add Account</strong> &rarr; <strong>Other</strong>.
            </SetupStep>

            <SetupStep :number="3" title="Select Account Type">
              <p>For calendars: Tap <strong>Add CalDAV Account</strong></p>
              <p>For contacts: Tap <strong>Add CardDAV Account</strong></p>
            </SetupStep>

            <SetupStep :number="4" title="Enter Server Details">
              <div class="space-y-2">
                <p><strong>Server:</strong></p>
                <SetupCopyableCode :value="serverUrl" />
                <p>
                  <strong>User Name:</strong>
                  <code class="bg-surface-100 dark:bg-surface-800 px-2 py-1 rounded text-surface-900 dark:text-surface-0">{{ username }}</code>
                </p>
                <p><strong>Password:</strong> Your Credentials Password</p>
              </div>
            </SetupStep>

            <SetupStep :number="5" title="Verify and Save">
              Tap <strong>Next</strong> to verify the account, then tap <strong>Save</strong>.
            </SetupStep>
          </ol>

          <Message severity="info" :closable="false" class="mt-4">
            You need to add CalDAV and CardDAV accounts separately on iOS.
          </Message>
        </TabPanel>

        <TabPanel value="macos">
          <ol class="space-y-4 pt-4">
            <SetupStep :number="1" title="Open System Settings">
              Go to <strong>System Settings</strong> (or System Preferences) &rarr; <strong>Internet Accounts</strong>.
            </SetupStep>

            <SetupStep :number="2" title="Add Account">
              Click <strong>Add Account</strong> &rarr; <strong>Add Other Account</strong>.
            </SetupStep>

            <SetupStep :number="3" title="Select Account Type">
              <p>For calendars: Select <strong>CalDAV Account</strong></p>
              <p>For contacts: Select <strong>CardDAV Account</strong></p>
            </SetupStep>

            <SetupStep :number="4" title="Configure">
              <div class="space-y-2">
                <p><strong>Account Type:</strong> Advanced</p>
                <p><strong>Server Address:</strong></p>
                <SetupCopyableCode :value="serverUrl" />
                <p>
                  <strong>User Name:</strong>
                  <code class="bg-surface-100 dark:bg-surface-800 px-2 py-1 rounded text-surface-900 dark:text-surface-0">{{ username }}</code>
                </p>
                <p><strong>Password:</strong> Your Credentials Password</p>
              </div>
            </SetupStep>

            <SetupStep :number="5" title="Sign In">
              Click <strong>Sign In</strong> and select which calendars/contacts to sync.
            </SetupStep>
          </ol>
        </TabPanel>
      </TabPanels>
    </Tabs>

  </div>
</template>

<script setup lang="ts">
defineProps<{
  serverUrl: string;
  username: string;
}>();

const activeTab = ref('ios');
</script>
