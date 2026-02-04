<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h2 class="text-2xl font-bold text-slate-800">Welcome to CalCard</h2>
      <div class="flex gap-2">
        <Button label="New Event" icon="pi pi-plus" />
        <Button label="New Contact" icon="pi pi-user-plus" severity="secondary" />
      </div>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      <Card>
        <template #title>Upcoming Events</template>
        <template #content>
          <p class="text-slate-600">No upcoming events for today.</p>
        </template>
      </Card>

      <Card>
        <template #title>Recent Contacts</template>
        <template #content>
          <p class="text-slate-600">No recent contacts found.</p>
        </template>
      </Card>

      <Card>
        <template #title>Server Status</template>
        <template #content>
          <div class="flex items-center gap-2">
            <div class="w-3 h-3 rounded-full bg-green-500"></div>
            <span class="text-slate-600">Backend Connected</span>
          </div>
        </template>
      </Card>
    </div>
  </div>
</template>

<script setup>
definePageMeta({
  layout: 'default'
});
</script>
