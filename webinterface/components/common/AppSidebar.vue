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
        <div class="w-8 h-8 rounded-xl bg-primary-600 flex items-center justify-center text-white font-bold text-lg">
          C
        </div>
        <span class="text-xl font-bold text-surface-900 dark:text-surface-0">CalCard</span>
      </NuxtLink>
      <button
        class="lg:hidden p-2 -mr-2 rounded-md text-surface-500 dark:text-surface-400 hover:bg-surface-100 dark:hover:bg-surface-800"
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
          :href="href ?? undefined"
          @click="navigate"
          :class="[
            'flex items-center gap-3 px-3 py-2.5 rounded-full text-sm font-medium transition-all duration-200',
            (isActive && item.to !== '/') || (item.to === '/' && isExactActive)
              ? 'bg-primary-50 dark:bg-primary-900/20 text-primary-700 dark:text-primary-300'
              : 'text-surface-700 dark:text-surface-300 hover:bg-surface-100 dark:hover:bg-surface-800'
          ]"
        >
          <i :class="[item.icon, 'text-lg']" />
          {{ item.label }}
          <!-- badgeHint: the space before it is an expression, a literal one is trimmed by the compiler. -->
          <span
            v-if="item.badge"
            class="ml-auto bg-primary-100 dark:bg-primary-900 text-primary-700 dark:text-primary-300 text-xs px-2 py-0.5 rounded-full font-semibold"
            :title="item.badgeHint ? `${item.badge} ${item.badgeHint}` : undefined"
          >
            {{ item.badge }}<span v-if="item.badgeHint" class="sr-only">{{ ' ' + item.badgeHint }}</span>
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
          'flex items-center gap-3 px-3 py-2.5 rounded-full text-sm font-medium transition-colors',
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
import { useTasksStore } from '~/stores/tasks';

defineProps<{
  open: boolean;
}>();

defineEmits<{
  close: [];
}>();

const route = useRoute();
const tasksStore = useTasksStore();

interface NavigationItem {
  to: string;
  label: string;
  icon: string;
  badge?: string;
  /** What the badge counts, in words: read after the number by screen readers, shown on hover. */
  badgeHint?: string;
}

const navigation = computed<NavigationItem[]>(() => {
  // The Tasks badge is the server's overdue count from the task summary (#170);
  // no badge at all while it is 0 or not loaded yet.
  const overdue = tasksStore.overdueCount;
  return [
    // '/' is the dashboard (story 042). The active-class expression above already
    // special-cases it with isExactActive so it doesn't light up on every route.
    { to: '/', label: 'Dashboard', icon: 'pi pi-th-large' },
    { to: '/calendar', label: 'Calendar', icon: 'pi pi-calendar' },
    {
      to: '/tasks',
      label: 'Tasks',
      icon: 'pi pi-check-square',
      ...(overdue > 0 ? { badge: String(overdue), badgeHint: 'overdue' } : {}),
    },
    { to: '/contacts', label: 'Contacts', icon: 'pi pi-users' },
  ];
});

const isActive = (path: string) => {
  return route.path.startsWith(path);
};

/**
 * Local midnight that starts the day after `now`, as epoch ms. Built from the
 * date parts rather than by adding 24 h, so a 23 h or 25 h DST day still lands
 * on 00:00 (or on the first valid time where a zone skips midnight).
 */
function nextLocalMidnight(now: Date): number {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1).getTime();
}

let midnightTimer: ReturnType<typeof setTimeout> | null = null;

/**
 * "Overdue" is relative to the local day, so the badge must roll over at
 * midnight even when nothing else asks for the summary: one timer, armed for
 * the next local midnight, refetches and arms itself for the one after.
 */
function armMidnightRefresh() {
  const midnight = nextLocalMidnight(new Date());
  midnightTimer = setTimeout(() => {
    // A timer can fire while the wall clock still says yesterday (the clock was
    // set back after arming). Asking then would count against yesterday again,
    // so it only re-arms for the same midnight.
    if (Date.now() >= midnight) void tasksStore.fetchSummary();
    armMidnightRefresh();
  }, midnight - Date.now());
}

// The sidebar is part of every signed-in layout, so this is the one place that
// keeps the badge current on every page. Task mutations refetch the summary in
// the store; this covers the first paint and the day rolling over.
onMounted(() => {
  void tasksStore.fetchSummary();
  armMidnightRefresh();
});

onBeforeUnmount(() => {
  if (midnightTimer) clearTimeout(midnightTimer);
  midnightTimer = null;
});
</script>
