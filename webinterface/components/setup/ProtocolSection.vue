<template>
  <div class="p-4">
    <!-- No credentials: prompt to create -->
    <div v-if="credentials.length === 0" class="text-center py-12">
      <i :class="protocol === 'caldav' ? 'pi pi-calendar' : 'pi pi-id-card'" class="text-5xl text-surface-300 dark:text-surface-600 mb-4 block" />
      <p class="text-surface-600 dark:text-surface-400 mb-2">{{ noCredentialsMessage }}</p>
      <p class="text-sm text-surface-500 dark:text-surface-400 mb-6">
        Credentials provide a dedicated username and password for your {{ protocol === 'caldav' ? 'calendar' : 'contacts' }} client.
      </p>
      <Button
        :label="createLabel"
        icon="pi pi-plus"
        @click="navigateTo(createUrl)"
      />
    </div>

    <!-- Has credentials -->
    <div v-else>
      <!-- Credential selector -->
      <div class="mb-6">
        <label class="block text-sm font-medium text-surface-500 dark:text-surface-400 mb-1">Use credential</label>
        <Select
          v-model="selectedCredential"
          :options="credentialSelectOptions"
          option-label="name"
          class="w-full max-w-md"
        >
          <template #value="{ value }">
            <span v-if="value">{{ value.name }} <span class="text-surface-400 font-mono text-sm ml-1">({{ value.username }})</span></span>
          </template>
          <template #option="{ option }">
            <div v-if="option.action" class="flex items-center gap-2 text-primary-600 dark:text-primary-400">
              <i class="pi pi-plus text-sm" />
              <span>{{ option.name }}</span>
            </div>
            <div v-else>
              <span>{{ option.name }}</span>
              <span class="text-surface-400 font-mono text-sm ml-2">({{ option.username }})</span>
            </div>
          </template>
        </Select>
      </div>

      <template v-if="activeCredential">
        <!-- Connection details + QR -->
        <div class="grid md:grid-cols-2 gap-6 mb-6">
          <div class="space-y-4">
            <SetupConnectionField label="Server URL" :value="serverUrl" />
            <SetupConnectionField label="Username" :value="activeCredential.username" />
            <SetupConnectionField :label="protocol === 'caldav' ? 'CalDAV URL' : 'CardDAV URL'" :value="davUrl" />
            <div class="flex flex-wrap gap-2 pt-1">
              <Button
                label="Copy All Settings"
                icon="pi pi-copy"
                severity="secondary"
                size="small"
                @click="copySettings"
              />
            </div>
          </div>

          <div class="flex flex-col items-center justify-center">
            <div class="bg-white p-4 rounded-lg border border-surface-200 dark:border-surface-700">
              <canvas ref="qrCanvas" class="w-48 h-48" />
            </div>
            <p class="text-sm text-surface-500 dark:text-surface-400 mt-2">Scan with DAVx5</p>
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

        <!-- Client setup guides -->
        <h3 class="text-lg font-medium text-surface-900 dark:text-surface-0 mb-4">Client Setup Guides</h3>
        <Tabs :value="clientTab" @update:value="clientTab = $event as string">
          <TabList>
            <Tab value="davx5">
              <div class="flex items-center gap-2">
                <i class="pi pi-android" />
                <span>DAVx5</span>
              </div>
            </Tab>
            <Tab value="apple">
              <div class="flex items-center gap-2">
                <i class="pi pi-apple" />
                <span>Apple</span>
              </div>
            </Tab>
            <Tab value="thunderbird">
              <div class="flex items-center gap-2">
                <i class="pi pi-envelope" />
                <span>Thunderbird</span>
              </div>
            </Tab>
            <Tab value="other">
              <div class="flex items-center gap-2">
                <i class="pi pi-cog" />
                <span>Other</span>
              </div>
            </Tab>
          </TabList>
          <TabPanels>
            <TabPanel value="davx5">
              <SetupInstructionsDavx5 :server-url="serverUrl" :username="activeCredential.username" />
            </TabPanel>
            <TabPanel value="apple">
              <SetupInstructionsApple :server-url="serverUrl" :username="activeCredential.username" />
            </TabPanel>
            <TabPanel value="thunderbird">
              <SetupInstructionsThunderbird
                :caldav-url="protocol === 'caldav' ? davUrl : ''"
                :carddav-url="protocol === 'carddav' ? davUrl : ''"
                :username="activeCredential.username"
              />
            </TabPanel>
            <TabPanel value="other">
              <SetupInstructionsGeneric
                :server-url="serverUrl"
                :caldav-url="protocol === 'caldav' ? davUrl : ''"
                :carddav-url="protocol === 'carddav' ? davUrl : ''"
                :username="activeCredential.username"
              />
            </TabPanel>
          </TabPanels>
        </Tabs>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import QRCode from 'qrcode';
import type { DavCredential } from '~/types/settings';

interface SelectOption {
  name: string;
  username: string;
  action?: string;
}

const props = defineProps<{
  credentials: DavCredential[];
  serverUrl: string;
  protocol: 'caldav' | 'carddav';
  createUrl: string;
  createLabel: string;
  noCredentialsMessage: string;
}>();

const toast = useAppToast();
const clientTab = ref('davx5');
const qrCanvas = ref<HTMLCanvasElement | null>(null);

const credentialSelectOptions = computed<SelectOption[]>(() => {
  const options: SelectOption[] = props.credentials.map(c => ({
    name: c.name,
    username: c.username,
  }));
  options.push({
    name: 'Create new credential...',
    username: '',
    action: props.createUrl,
  });
  return options;
});

const selectedCredential = ref<SelectOption | null>(
  props.credentials.length > 0
    ? { name: props.credentials[0]!.name, username: props.credentials[0]!.username }
    : null
);

// When credentials change (e.g. navigating back), pick the first
watch(() => props.credentials, (creds) => {
  if (creds.length > 0 && !selectedCredential.value) {
    selectedCredential.value = { name: creds[0]!.name, username: creds[0]!.username };
  }
}, { immediate: true });

// Intercept "create new" action
let previousSelection = selectedCredential.value;
watch(selectedCredential, (newVal) => {
  if (newVal?.action) {
    navigateTo(newVal.action);
    nextTick(() => {
      selectedCredential.value = previousSelection;
    });
    return;
  }
  previousSelection = newVal;
});

const activeCredential = computed(() =>
  selectedCredential.value && !selectedCredential.value.action ? selectedCredential.value : null
);

const davUrl = computed(() => {
  if (!activeCredential.value) return '';
  const pathType = props.protocol === 'caldav' ? 'calendars' : 'addressbooks';
  return `${props.serverUrl}/dav/${activeCredential.value.username}/${pathType}/`;
});

// QR code
const generateQrCode = async () => {
  if (!qrCanvas.value) return;
  try {
    await QRCode.toCanvas(qrCanvas.value, props.serverUrl, {
      width: 192,
      margin: 1,
      color: { dark: '#000000', light: '#ffffff' },
    });
  } catch {
    // silently fail
  }
};

// Generate QR when canvas becomes available (credential selected)
watch(activeCredential, async (val) => {
  if (val) {
    await nextTick();
    generateQrCode();
  }
}, { immediate: true });

onMounted(() => {
  if (activeCredential.value) generateQrCode();
});

const downloadQrCode = async () => {
  try {
    const dataUrl = await QRCode.toDataURL(props.serverUrl, { width: 400, margin: 2 });
    const a = document.createElement('a');
    a.href = dataUrl;
    a.download = 'calcard-setup-qr.png';
    a.click();
  } catch {
    toast.error('Failed to generate QR code');
  }
};

const copySettings = async () => {
  if (!activeCredential.value) return;
  const label = props.protocol === 'caldav' ? 'CalDAV' : 'CardDAV';
  const text = `${label} Settings
================================
Server URL: ${props.serverUrl}
Username: ${activeCredential.value.username}
${label} URL: ${davUrl.value}

Note: Use the password you set when creating this credential.`;

  await navigator.clipboard.writeText(text);
  toast.success('Settings copied to clipboard');
};
</script>
