<template>
  <section>
    <header class="flex items-center justify-between gap-3 mb-3">
      <h3 class="text-lg font-medium text-surface-900 dark:text-surface-0">
        <i :class="[icon, 'mr-2 text-primary-500']" />{{ title }}
      </h3>
      <span class="text-xs text-surface-500 dark:text-surface-400 whitespace-nowrap">
        {{ countLabel }}
      </span>
    </header>

    <Message v-if="error" severity="error" :closable="false" class="mb-3">{{ error }}</Message>

    <CommonSkeletonList v-else-if="loading" :count="4" />

    <p
      v-else-if="!filtered.length"
      class="text-sm text-surface-500 dark:text-surface-400 py-4"
    >
      {{ packages.length ? 'No package matches this filter.' : 'No packages listed.' }}
    </p>

    <template v-else>
      <ul class="divide-y divide-surface-200 dark:divide-surface-800">
        <li
          v-for="pkg in visible"
          :key="`${pkg.name}@${pkg.version}`"
          class="flex items-center gap-3 py-2"
        >
          <div class="min-w-0 flex-1">
            <a
              :href="pkg.url"
              target="_blank"
              rel="noopener noreferrer"
              :title="pkg.url"
              class="text-sm font-medium text-primary-700 dark:text-primary-300 hover:underline break-all"
            >
              <CommonHighlightText :text="pkg.name" :highlight="filter" />
              <i class="pi pi-external-link text-[0.65rem] ml-1 align-baseline" />
            </a>
            <p class="text-xs text-surface-500 dark:text-surface-400">{{ pkg.version }}</p>
          </div>
          <Tag
            :value="pkg.license"
            :severity="pkg.license === UNKNOWN_LICENSE ? 'warn' : 'secondary'"
            :title="pkg.license === UNKNOWN_LICENSE ? unknownHint : pkg.license"
          />
        </li>
      </ul>

      <Button
        v-if="hasMore"
        :label="`Show ${remaining} more`"
        icon="pi pi-chevron-down"
        text
        size="small"
        class="mt-2"
        @click="showMore"
      />
    </template>
  </section>
</template>

<script setup lang="ts">
import { UNKNOWN_LICENSE, type OpenSourcePackage } from '~/types/about';

const props = withDefaults(
  defineProps<{
    title: string;
    icon: string;
    packages: OpenSourcePackage[];
    filter?: string;
    loading?: boolean;
    error?: string | null;
  }>(),
  { filter: '', loading: false, error: null },
);

// The npm closure alone is ~600 entries; rendering all of them at once is
// visible jank for no benefit, so grow the list on demand instead.
const PAGE_SIZE = 100;
const limit = ref(PAGE_SIZE);

const filtered = computed(() => filterOpenSourcePackages(props.packages, props.filter));
const visible = computed(() => filtered.value.slice(0, limit.value));
const remaining = computed(() => Math.max(filtered.value.length - limit.value, 0));
const hasMore = computed(() => remaining.value > 0);

const countLabel = computed(() => {
  if (props.loading) return '';
  const total = props.packages.length;
  if (props.filter.trim() && filtered.value.length !== total) {
    return `${filtered.value.length} of ${total}`;
  }
  return `${total} ${total === 1 ? 'package' : 'packages'}`;
});

const unknownHint =
  'License could not be determined automatically — this does not mean the package is unlicensed.';

function showMore() {
  limit.value += PAGE_SIZE;
}

// A new filter starts a new (short) result list; keeping a grown limit would
// otherwise render hundreds of rows for a one-character query.
watch(
  () => props.filter,
  () => {
    limit.value = PAGE_SIZE;
  },
);
</script>
