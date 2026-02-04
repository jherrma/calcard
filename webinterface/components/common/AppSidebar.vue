<template>
  <aside
    :class="[
      'fixed inset-y-0 left-0 z-30 w-64 bg-surface-0 dark:bg-surface-900 shadow-xl lg:shadow-none border-r border-surface-200 dark:border-surface-800 transform transition-transform duration-300 ease-in-out lg:translate-x-0 flex flex-col',
      open ? 'translate-x-0' : '-translate-x-full'
    ]"
  >
    <!-- Logo -->
    <div class="flex items-center justify-between h-16 px-6 border-b border-surface-200 dark:border-surface-800">
      <NuxtLink to="/" class="flex items-center gap-3" @click="$emit('close')">
        <div class="w-8 h-8 rounded bg-primary-600 flex items-center justify-center text-white font-bold text-lg">
          C
        </div>
        <span class="text-xl font-bold text-surface-900 dark:text-surface-0">CalCard</span>
      </NuxtLink>
      <button
        class="lg:hidden p-2 -mr-2 rounded-md text-surface-500 hover:bg-surface-100 dark:hover:bg-surface-800"
        @click="$emit('close')"
      >
        <i class="pi pi-times" />
      </button>
    </div>

    <!-- Navigation -->
    <nav class="flex-1 px-4 py-6 space-y-1 overflow-y-auto">
      <NuxtLink
        v-for="item in navigation"
        :key="item.to"
        :to="item.to"
        @click="$emit('close')"
        custom
        v-slot="{ href, navigate, isActive, isExactActive }"
      >
        <a
          :href="href"
          @click="navigate"
          :class="[
            'flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200',
            (isActive && item.to !== '/') || (item.to === '/' && isExactActive)
              ? 'bg-primary-50 dark:bg-primary-900/20 text-primary-700 dark:text-primary-300'
              : 'text-surface-700 dark:text-surface-300 hover:bg-surface-100 dark:hover:bg-surface-800'
          ]"
        >
          <i :class="[item.icon, 'text-lg']" />
          {{ item.label }}
          <span
            v-if="item.badge"
            class="ml-auto bg-primary-100 dark:bg-primary-900 text-primary-700 dark:text-primary-300 text-xs px-2 py-0.5 rounded-full font-semibold"
          >
            {{ item.badge }}
          </span>
        </a>
      </NuxtLink>
    </nav>

    <!-- Bottom section -->
    <div class="p-4 border-t border-surface-200 dark:border-surface-800">
      <NuxtLink
        to="/settings"
        @click="$emit('close')"
        :class="[
          'flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors',
          isActive('/settings')
            ? 'bg-primary-50 dark:bg-primary-900/20 text-primary-700 dark:text-primary-300'
            : 'text-surface-700 dark:text-surface-300 hover:bg-surface-100 dark:hover:bg-surface-800'
        ]"
      >
        <i class="pi pi-cog text-lg" />
        Settings
      </NuxtLink>
    </div>
  </aside>
</template>

<script setup lang="ts">
defineProps<{
  open: boolean;
}>();

defineEmits<{
  close: [];
}>();

const route = useRoute();

const navigation = [
  { to: '/calendar', label: 'Calendar', icon: 'pi pi-calendar' },
  { to: '/contacts', label: 'Contacts', icon: 'pi pi-users' },
];

const isActive = (path: string) => {
  return route.path.startsWith(path);
};
</script>
