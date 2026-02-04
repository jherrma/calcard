<template>
  <div class="min-h-screen flex flex-col bg-slate-50">
    <header class="h-16 bg-white border-b flex items-center px-6 sticky top-0 z-10 shadow-sm">
      <div class="flex items-center gap-2">
        <i class="pi pi-calendar text-primary-500 text-2xl"></i>
        <h1 class="text-xl font-bold text-slate-800">CalCard</h1>
      </div>
      <div class="flex-1"></div>
      <div class="flex items-center gap-4">
        <Button icon="pi pi-bell" text rounded aria-label="Notifications" />
        <Avatar icon="pi pi-user" shape="circle" class="bg-primary-500 text-white" />
      </div>
    </header>

    <div class="flex flex-1 overflow-hidden">
      <aside class="w-64 bg-white border-r hidden md:flex flex-col">
        <nav class="flex-1 p-4 space-y-1">
          <NuxtLink v-for="item in menuItems" :key="item.label" :to="item.to"
            class="flex items-center gap-3 px-3 py-2 rounded-lg transition-colors"
            :class="[route.path === item.to ? 'bg-primary-50 text-primary-600 font-medium' : 'text-slate-600 hover:bg-slate-50']">
            <i :class="[item.icon, route.path === item.to ? 'text-primary-600' : 'text-slate-400']"></i>
            {{ item.label }}
          </NuxtLink>
        </nav>
      </aside>

      <main class="flex-1 overflow-y-auto p-6">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup>
const route = useRoute();

const menuItems = [
  { label: 'Dashboard', icon: 'pi pi-home', to: '/' },
  { label: 'Calendar', icon: 'pi pi-calendar', to: '/calendar' },
  { label: 'Contacts', icon: 'pi pi-address-book', to: '/contacts' },
  { label: 'Settings', icon: 'pi pi-cog', to: '/settings' },
];
</script>

<style scoped>
.router-link-active {
  /* NuxtLink active state is handled via class binding above */
}
</style>
