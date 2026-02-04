export default defineNuxtRouteMiddleware(async () => {
  const authStore = useAuthStore();

  // Initialize auth if not already done
  if (authStore.isLoading) {
    await authStore.initAuth();
  }

  // If already authenticated, redirect to calendar (main app)
  if (authStore.isAuthenticated) {
    return navigateTo("/calendar");
  }
});
