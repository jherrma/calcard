export default defineNuxtRouteMiddleware(async (to) => {
  const authStore = useAuthStore();

  // Initialize auth if not already done
  if (authStore.isLoading) {
    await authStore.initAuth();
  }

  // If not authenticated and trying to access a protected route
  if (!authStore.isAuthenticated && !to.path.startsWith("/auth")) {
    return navigateTo("/auth/login");
  }
});
