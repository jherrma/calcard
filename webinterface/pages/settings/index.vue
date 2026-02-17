<script setup lang="ts">
definePageMeta({
  layout: 'settings',
  middleware: 'auth',
});

navigateTo('/settings/profile', { replace: true });
</script>
