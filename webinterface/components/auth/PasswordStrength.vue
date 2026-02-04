<template>
  <div class="mt-2">
    <div class="flex gap-1 h-1.5 mb-1.5">
      <div
        v-for="i in 4"
        :key="i"
        class="flex-1 rounded-full transition-colors duration-300"
        :class="i <= strength ? strengthColors[strength] : 'bg-surface-200 dark:bg-surface-700'"
      />
    </div>
    <div class="flex justify-between items-center">
      <span class="text-xs font-medium" :class="strengthTextColors[strength]">
        {{ strengthLabels[strength] || 'Enter password' }}
      </span>
      <span v-if="strength > 0" class="text-xs text-surface-500">
        {{ strengthPercentage }}%
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  password: string;
}>();

const strengthLabels = ["", "Weak", "Fair", "Good", "Strong"];
const strengthColors = [
  "",
  "bg-red-500",
  "bg-orange-500",
  "bg-yellow-500",
  "bg-green-500",
];
const strengthTextColors = [
  "",
  "text-red-500",
  "text-orange-500",
  "text-yellow-500",
  "text-green-500",
];

const strength = computed(() => {
  const password = props.password;
  if (!password) return 0;

  let score = 0;
  if (password.length >= 8) score++;
  if (/[a-z]/.test(password) && /[A-Z]/.test(password)) score++;
  if (/\d/.test(password)) score++;
  if (/[^a-zA-Z0-9]/.test(password)) score++;

  return score;
});

const strengthPercentage = computed(() => {
  return (strength.value / 4) * 100;
});
</script>
