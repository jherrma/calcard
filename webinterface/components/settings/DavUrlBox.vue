<template>
  <div class="bg-surface-0 dark:bg-surface-900 rounded-xl border border-surface-200 dark:border-surface-800 p-4 mb-6">
    <h3 class="font-medium text-surface-900 dark:text-surface-0 mb-3">Connecting a client</h3>

    <!-- Without the username every URL would be /dav//…, which a client
         accepts and then fails on with no hint why. Say so instead. -->
    <Message v-if="!username" severity="warn" :closable="false">
      Your account username could not be loaded, so the DAV URLs cannot be shown. Reload the page to try again.
    </Message>

    <template v-else>
      <div v-for="field in fields" :key="field.id" class="flex flex-col gap-2 mb-3">
        <label :for="field.id" class="text-sm font-medium text-surface-700 dark:text-surface-300">{{ field.label }}</label>
        <div class="flex gap-2">
          <InputText
            :id="field.id"
            :model-value="field.value"
            readonly
            class="w-full font-mono text-sm"
            @focus="selectAll"
          />
          <Button
            icon="pi pi-copy"
            severity="secondary"
            :aria-label="`Copy ${field.label}`"
            @click="copy(field.value, `${field.label} copied`)"
          />
        </div>
      </div>

      <p class="text-sm text-surface-500 dark:text-surface-400 mb-2">
        <template v-if="variant === 'app-passwords'">
          Sign in with your account username (or e-mail address) and the app password. An app
          password only works for the protocols it was created for.
        </template>
        <template v-else>
          Sign in with the username and password of one of the credentials below, or with your
          account username <span class="font-mono">{{ username }}</span> and an app password. The URL
          contains your account username whichever login you use.
        </template>
      </p>
      <p class="text-sm text-surface-500 dark:text-surface-400">
        Clients that find the URLs themselves (DAVx5, Apple) usually only need the server address
        <span class="font-mono break-all">{{ origin }}</span>.
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
// The URLs a DAV client is configured with, at the top of the CalDAV / CardDAV
// credential pages and the app-password page. The username comes from the auth
// store, which /users/me and the login response fill.

const props = defineProps<{
  variant: 'caldav' | 'carddav' | 'app-passwords';
}>();

interface Field {
  id: string;
  label: string;
  value: string;
}

const config = useRuntimeConfig();
const authStore = useAuthStore();
const copy = useCopyToClipboard();

const username = computed(() => authStore.user?.username ?? '');

// Client-only page (ssr: false), so window is always available.
const origin = computed(() => davOrigin(config.public.apiBaseUrl as string, window.location.origin));

const caldavUrl = computed(() => davHomeUrl(origin.value, username.value, 'caldav'));
const carddavUrl = computed(() => davHomeUrl(origin.value, username.value, 'carddav'));

const fields = computed<Field[]>(() => {
  const caldav = { id: 'dav-url-caldav', label: 'CalDAV URL', value: caldavUrl.value };
  const carddav = { id: 'dav-url-carddav', label: 'CardDAV URL', value: carddavUrl.value };
  switch (props.variant) {
    case 'caldav':
      return [caldav];
    case 'carddav':
      return [carddav];
    default:
      // An app password logs in with the account username, so it is shown too.
      return [{ id: 'dav-account-username', label: 'Username', value: username.value }, caldav, carddav];
  }
});

const selectAll = (event: FocusEvent) => {
  (event.target as HTMLInputElement | null)?.select();
};
</script>
