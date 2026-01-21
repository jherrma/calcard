# Story 040: Client Setup Instructions Page

## Title
Implement Setup Instructions and Help Page

## Description
As a user, I want to view setup instructions for various CalDAV/CardDAV clients so that I can configure my devices to sync with the server.

## Related Acceptance Criteria

| ID | Criterion |
|----|-----------|
| UI-7.2.1 | Web UI provides DAVx5 setup instructions |
| UI-7.2.2 | Web UI provides Apple device setup instructions |
| UI-7.2.3 | Web UI provides Thunderbird setup instructions |
| UI-7.2.4 | Server URL is clearly displayed for copying |

## Acceptance Criteria

### Setup Page

- [ ] Route: `/setup`
- [ ] Accessible from header user menu
- [ ] Overview section with user's connection info
- [ ] Tab or accordion for each client type
- [ ] Personalized with user's username and server URL

### Connection Information Section

- [ ] Server URL with copy button
- [ ] Username with copy button
- [ ] CalDAV URL with copy button
- [ ] CardDAV URL with copy button
- [ ] QR code for quick DAVx5 setup
- [ ] Link to app passwords page

### DAVx5 Instructions

- [ ] Step-by-step instructions with screenshots/icons
- [ ] Server URL pre-filled for user
- [ ] Link to Google Play and F-Droid
- [ ] Notes about app passwords
- [ ] Troubleshooting tips

### Apple (iOS/macOS) Instructions

- [ ] Separate instructions for iOS and macOS
- [ ] Step-by-step with settings paths
- [ ] Server URL pre-filled
- [ ] Notes about CalDAV vs CardDAV accounts
- [ ] Certificate trust instructions (if self-signed)

### Thunderbird Instructions

- [ ] Step-by-step instructions
- [ ] Separate for calendar and contacts
- [ ] Server URL pre-filled
- [ ] Notes about built-in vs TbSync add-on

### Other Clients

- [ ] Generic CalDAV/CardDAV setup guide
- [ ] Links to client documentation
- [ ] Common settings explained

### Quick Actions

- [ ] "Create App Password" button (links to settings)
- [ ] "Download QR Code" button
- [ ] "Copy All Settings" button (copies formatted text)

## Technical Notes

### Setup Page
```vue
<!-- pages/setup/index.vue -->
<template>
  <div class="max-w-4xl mx-auto">
    <h1 class="text-2xl font-semibold mb-6">Setup Your Devices</h1>

    <!-- Quick Info Card -->
    <div class="bg-white rounded-lg shadow p-6 mb-6">
      <h2 class="text-lg font-medium mb-4">Your Connection Details</h2>

      <div class="grid md:grid-cols-2 gap-6">
        <div class="space-y-4">
          <ConnectionField label="Server URL" :value="serverUrl" />
          <ConnectionField label="Username" :value="username" />
          <ConnectionField label="CalDAV URL" :value="caldavUrl" />
          <ConnectionField label="CardDAV URL" :value="carddavUrl" />
        </div>

        <div class="flex flex-col items-center justify-center">
          <div class="bg-white p-4 rounded-lg border">
            <img :src="qrCodeUrl" alt="Setup QR Code" class="w-48 h-48" />
          </div>
          <p class="text-sm text-gray-500 mt-2">Scan with DAVx5</p>
          <Button
            label="Download QR Code"
            icon="pi pi-download"
            severity="secondary"
            size="small"
            class="mt-2"
            @click="downloadQrCode"
          />
        </div>
      </div>

      <Divider />

      <div class="flex flex-wrap gap-2">
        <Button
          label="Create App Password"
          icon="pi pi-key"
          severity="secondary"
          @click="router.push('/settings/app-passwords')"
        />
        <Button
          label="Copy All Settings"
          icon="pi pi-copy"
          severity="secondary"
          @click="copyAllSettings"
        />
      </div>
    </div>

    <!-- Client Instructions -->
    <div class="bg-white rounded-lg shadow">
      <TabView>
        <TabPanel>
          <template #header>
            <div class="flex items-center gap-2">
              <img src="/icons/davx5.png" alt="DAVx5" class="w-5 h-5" />
              <span>DAVx5 (Android)</span>
            </div>
          </template>
          <SetupInstructionsDavx5 :server-url="serverUrl" :username="username" />
        </TabPanel>

        <TabPanel>
          <template #header>
            <div class="flex items-center gap-2">
              <i class="pi pi-apple" />
              <span>Apple (iOS/macOS)</span>
            </div>
          </template>
          <SetupInstructionsApple :server-url="serverUrl" :username="username" />
        </TabPanel>

        <TabPanel>
          <template #header>
            <div class="flex items-center gap-2">
              <img src="/icons/thunderbird.png" alt="Thunderbird" class="w-5 h-5" />
              <span>Thunderbird</span>
            </div>
          </template>
          <SetupInstructionsThunderbird
            :caldav-url="caldavUrl"
            :carddav-url="carddavUrl"
            :username="username"
          />
        </TabPanel>

        <TabPanel header="Other Clients">
          <SetupInstructionsGeneric
            :server-url="serverUrl"
            :caldav-url="caldavUrl"
            :carddav-url="carddavUrl"
            :username="username"
          />
        </TabPanel>
      </TabView>
    </div>
  </div>
</template>

<script setup lang="ts">
definePageMeta({
  middleware: 'auth',
});

const authStore = useAuthStore();
const router = useRouter();
const config = useRuntimeConfig();
const toast = useAppToast();

const username = computed(() => authStore.user?.username || '');
const serverUrl = computed(() => config.public.apiBaseUrl);
const caldavUrl = computed(() => `${serverUrl.value}/dav/calendars/${username.value}/`);
const carddavUrl = computed(() => `${serverUrl.value}/dav/addressbooks/${username.value}/`);
const qrCodeUrl = computed(() => `${serverUrl.value}/api/v1/users/me/setup/qr`);

const downloadQrCode = async () => {
  const response = await fetch(qrCodeUrl.value, {
    headers: {
      Authorization: `Bearer ${authStore.accessToken}`,
    },
  });
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'caldav-setup-qr.png';
  a.click();
  URL.revokeObjectURL(url);
};

const copyAllSettings = async () => {
  const text = `CalDAV/CardDAV Server Settings
================================
Server URL: ${serverUrl.value}
Username: ${username.value}

CalDAV URL (Calendars): ${caldavUrl.value}
CardDAV URL (Contacts): ${carddavUrl.value}

Note: Use an App Password instead of your main account password.
Create one at: ${serverUrl.value}/settings/app-passwords`;

  await navigator.clipboard.writeText(text);
  toast.success('Settings copied to clipboard');
};
</script>
```

### Connection Field Component
```vue
<!-- components/setup/ConnectionField.vue -->
<template>
  <div>
    <label class="block text-sm font-medium text-gray-500 mb-1">{{ label }}</label>
    <div class="flex gap-2">
      <InputText :value="value" readonly class="flex-1 font-mono text-sm" />
      <Button icon="pi pi-copy" severity="secondary" @click="copy" />
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  label: string;
  value: string;
}>();

const toast = useAppToast();

const copy = async () => {
  await navigator.clipboard.writeText(props.value);
  toast.success(`${props.label} copied`);
};
</script>
```

### DAVx5 Instructions Component
```vue
<!-- components/setup/SetupInstructionsDavx5.vue -->
<template>
  <div class="space-y-6">
    <Message severity="info" :closable="false">
      DAVx5 is the recommended app for syncing calendars and contacts on Android.
    </Message>

    <div class="flex gap-4 mb-6">
      <a
        href="https://play.google.com/store/apps/details?id=at.bitfire.davdroid"
        target="_blank"
        class="inline-flex items-center gap-2 px-4 py-2 bg-gray-100 rounded-lg hover:bg-gray-200"
      >
        <img src="/icons/google-play.svg" alt="Google Play" class="h-6" />
        <span>Google Play</span>
      </a>
      <a
        href="https://f-droid.org/packages/at.bitfire.davdroid/"
        target="_blank"
        class="inline-flex items-center gap-2 px-4 py-2 bg-gray-100 rounded-lg hover:bg-gray-200"
      >
        <img src="/icons/f-droid.svg" alt="F-Droid" class="h-6" />
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
        <CopyableCode :value="serverUrl" />
      </SetupStep>

      <SetupStep :number="5" title="Enter Credentials">
        <p class="mb-2">
          <strong>Username:</strong>
          <code class="bg-gray-100 px-2 py-1 rounded ml-2">{{ username }}</code>
        </p>
        <p class="mb-2">
          <strong>Password:</strong> Your App Password
        </p>
        <Message severity="warn" :closable="false" class="mt-2">
          Do NOT use your main account password.
          <NuxtLink to="/settings/app-passwords" class="underline">
            Create an App Password
          </NuxtLink>
          first.
        </Message>
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
      <h3 class="font-medium mb-2">Quick Setup with QR Code</h3>
      <p class="text-sm text-gray-600 mb-4">
        In DAVx5, you can also scan the QR code shown above to automatically fill in the server URL.
        You'll still need to enter your username and app password.
      </p>
    </div>

    <Divider />

    <div>
      <h3 class="font-medium mb-2">Troubleshooting</h3>
      <Accordion>
        <AccordionTab header="Connection failed">
          <ul class="list-disc list-inside text-sm space-y-1">
            <li>Make sure you're using an App Password, not your main password</li>
            <li>Check that the server URL is correct</li>
            <li>Verify your internet connection</li>
            <li>If using self-signed certificates, accept the certificate in DAVx5</li>
          </ul>
        </AccordionTab>
        <AccordionTab header="Calendars/Contacts not syncing">
          <ul class="list-disc list-inside text-sm space-y-1">
            <li>Check that the calendars/address books are selected in DAVx5</li>
            <li>Try a manual sync by tapping the sync button</li>
            <li>Check Android's battery optimization settings for DAVx5</li>
          </ul>
        </AccordionTab>
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
```

### Apple Instructions Component
```vue
<!-- components/setup/SetupInstructionsApple.vue -->
<template>
  <div class="space-y-6">
    <TabView>
      <TabPanel header="iOS (iPhone/iPad)">
        <ol class="space-y-4">
          <SetupStep :number="1" title="Open Settings">
            Go to <strong>Settings</strong> → <strong>Calendar</strong> → <strong>Accounts</strong>
            (or <strong>Settings</strong> → <strong>Contacts</strong> → <strong>Accounts</strong> for contacts).
          </SetupStep>

          <SetupStep :number="2" title="Add Account">
            Tap <strong>Add Account</strong> → <strong>Other</strong>.
          </SetupStep>

          <SetupStep :number="3" title="Select Account Type">
            <p>For calendars: Tap <strong>Add CalDAV Account</strong></p>
            <p>For contacts: Tap <strong>Add CardDAV Account</strong></p>
          </SetupStep>

          <SetupStep :number="4" title="Enter Server Details">
            <div class="space-y-2">
              <p><strong>Server:</strong></p>
              <CopyableCode :value="serverUrl" />
              <p><strong>User Name:</strong> <code class="bg-gray-100 px-2 py-1 rounded">{{ username }}</code></p>
              <p><strong>Password:</strong> Your App Password</p>
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

      <TabPanel header="macOS">
        <ol class="space-y-4">
          <SetupStep :number="1" title="Open System Settings">
            Go to <strong>System Settings</strong> (or System Preferences) → <strong>Internet Accounts</strong>.
          </SetupStep>

          <SetupStep :number="2" title="Add Account">
            Click <strong>Add Account</strong> → <strong>Add Other Account</strong>.
          </SetupStep>

          <SetupStep :number="3" title="Select Account Type">
            <p>For calendars: Select <strong>CalDAV Account</strong></p>
            <p>For contacts: Select <strong>CardDAV Account</strong></p>
          </SetupStep>

          <SetupStep :number="4" title="Configure">
            <div class="space-y-2">
              <p><strong>Account Type:</strong> Advanced</p>
              <p><strong>Server Address:</strong></p>
              <CopyableCode :value="serverUrl" />
              <p><strong>User Name:</strong> <code class="bg-gray-100 px-2 py-1 rounded">{{ username }}</code></p>
              <p><strong>Password:</strong> Your App Password</p>
            </div>
          </SetupStep>

          <SetupStep :number="5" title="Sign In">
            Click <strong>Sign In</strong> and select which calendars/contacts to sync.
          </SetupStep>
        </ol>
      </TabPanel>
    </TabView>

    <Message severity="warn" :closable="false">
      <strong>Important:</strong> Use an
      <NuxtLink to="/settings/app-passwords" class="underline">App Password</NuxtLink>,
      not your main account password.
    </Message>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  serverUrl: string;
  username: string;
}>();
</script>
```

### Setup Step Component
```vue
<!-- components/setup/SetupStep.vue -->
<template>
  <li class="flex gap-4">
    <div
      class="flex-shrink-0 w-8 h-8 bg-primary-100 text-primary-700 rounded-full flex items-center justify-center font-semibold"
    >
      {{ number }}
    </div>
    <div class="flex-1">
      <h4 class="font-medium mb-1">{{ title }}</h4>
      <div class="text-sm text-gray-600">
        <slot />
      </div>
    </div>
  </li>
</template>

<script setup lang="ts">
defineProps<{
  number: number;
  title: string;
}>();
</script>
```

### Copyable Code Component
```vue
<!-- components/setup/CopyableCode.vue -->
<template>
  <div class="flex items-center gap-2 bg-gray-100 rounded-lg p-2">
    <code class="flex-1 text-sm font-mono break-all">{{ value }}</code>
    <Button
      icon="pi pi-copy"
      severity="secondary"
      text
      size="small"
      @click="copy"
    />
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  value: string;
}>();

const toast = useAppToast();

const copy = async () => {
  await navigator.clipboard.writeText(props.value);
  toast.success('Copied to clipboard');
};
</script>
```

### Generic Instructions Component
```vue
<!-- components/setup/SetupInstructionsGeneric.vue -->
<template>
  <div class="space-y-6">
    <p class="text-gray-600">
      Most CalDAV/CardDAV clients can connect using the following information.
      Refer to your client's documentation for specific setup steps.
    </p>

    <div class="bg-gray-50 rounded-lg p-4 space-y-3">
      <h3 class="font-medium">Connection Settings</h3>

      <div class="grid gap-3">
        <div>
          <span class="text-sm text-gray-500">Server URL / Base URL:</span>
          <CopyableCode :value="serverUrl" />
        </div>

        <div>
          <span class="text-sm text-gray-500">CalDAV URL (for calendars):</span>
          <CopyableCode :value="caldavUrl" />
        </div>

        <div>
          <span class="text-sm text-gray-500">CardDAV URL (for contacts):</span>
          <CopyableCode :value="carddavUrl" />
        </div>

        <div>
          <span class="text-sm text-gray-500">Username:</span>
          <CopyableCode :value="username" />
        </div>

        <div>
          <span class="text-sm text-gray-500">Password:</span>
          <p class="text-sm">
            Use an <NuxtLink to="/settings/app-passwords" class="text-primary-600 underline">App Password</NuxtLink>
          </p>
        </div>
      </div>
    </div>

    <div>
      <h3 class="font-medium mb-3">Well-Known URLs</h3>
      <p class="text-sm text-gray-600 mb-2">
        Some clients use well-known URLs for auto-discovery:
      </p>
      <div class="space-y-2">
        <CopyableCode :value="`${serverUrl}/.well-known/caldav`" />
        <CopyableCode :value="`${serverUrl}/.well-known/carddav`" />
      </div>
    </div>

    <div>
      <h3 class="font-medium mb-3">Other Clients</h3>
      <ul class="space-y-2 text-sm">
        <li>
          <strong>Evolution (Linux):</strong>
          File → New → Calendar → CalDAV
        </li>
        <li>
          <strong>GNOME Calendar:</strong>
          Settings → Online Accounts → Other
        </li>
        <li>
          <strong>Windows 10/11 Mail:</strong>
          Add Account → iCloud (use CalDAV URL)
        </li>
        <li>
          <strong>eM Client:</strong>
          Menu → Accounts → Add Account → CalDAV/CardDAV
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
```

## Definition of Done

- [ ] Setup page displays user's connection details
- [ ] All URLs have copy buttons
- [ ] QR code displayed and downloadable
- [ ] "Copy All Settings" copies formatted text
- [ ] DAVx5 instructions complete with steps
- [ ] Apple iOS instructions complete
- [ ] Apple macOS instructions complete
- [ ] Thunderbird instructions complete
- [ ] Generic instructions for other clients
- [ ] Links to app passwords page
- [ ] Troubleshooting sections included
- [ ] Store links for mobile apps
- [ ] Responsive design
- [ ] All code examples show user's actual values
