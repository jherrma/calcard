<template>
  <div class="max-w-4xl mx-auto">
    <h1 class="text-2xl font-semibold text-surface-900 dark:text-surface-0 mb-6">Setup Your Devices</h1>

    <div class="bg-surface-0 dark:bg-surface-900 rounded-xl shadow-md border border-surface-200 dark:border-surface-800">
      <CommonLoadingSpinner v-if="loadingCredentials" />
      <Tabs v-else :value="protocolTab" @update:value="protocolTab = $event as string">
        <TabList>
          <Tab value="calendars">
            <div class="flex items-center gap-2">
              <i class="pi pi-calendar" />
              <span>Calendars (CalDAV)</span>
            </div>
          </Tab>
          <Tab value="contacts">
            <div class="flex items-center gap-2">
              <i class="pi pi-id-card" />
              <span>Contacts (CardDAV)</span>
            </div>
          </Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="calendars">
            <SetupProtocolSection
              :credentials="caldavCredentials"
              :server-url="serverUrl"
              protocol="caldav"
              create-url="/settings/caldav-credentials"
              create-label="Create CalDAV Credential"
              no-credentials-message="You need CalDAV credentials to sync calendars with external clients."
            />
          </TabPanel>
          <TabPanel value="contacts">
            <SetupProtocolSection
              :credentials="carddavCredentials"
              :server-url="serverUrl"
              protocol="carddav"
              create-url="/settings/carddav-credentials"
              create-label="Create CardDAV Credential"
              no-credentials-message="You need CardDAV credentials to sync contacts with external clients."
            />
          </TabPanel>
        </TabPanels>
      </Tabs>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { DavCredentialListResponse } from '~/types/settings';

definePageMeta({
  middleware: 'auth',
  layout: 'default',
});

const api = useApi();

const protocolTab = ref('calendars');
const loadingCredentials = ref(true);
const caldavCredentials = ref<import('~/types/settings').DavCredential[]>([]);
const carddavCredentials = ref<import('~/types/settings').DavCredential[]>([]);

const serverUrl = computed(() => {
  const config = useRuntimeConfig();
  return (config.public.apiBaseUrl as string) || window.location.origin;
});

onMounted(async () => {
  loadingCredentials.value = true;
  try {
    const [caldavRes, carddavRes] = await Promise.all([
      api<DavCredentialListResponse>('/api/v1/caldav-credentials'),
      api<DavCredentialListResponse>('/api/v1/carddav-credentials'),
    ]);
    caldavCredentials.value = caldavRes.credentials || [];
    carddavCredentials.value = carddavRes.credentials || [];
  } catch {
    // Silently fail
  } finally {
    loadingCredentials.value = false;
  }
});
</script>
