export default defineNuxtRouteMiddleware(async () => {
  const authStore = useAuthStore();

  // Initialize auth if not already done
  if (authStore.isLoading) {
    await authStore.initAuth();
  }

  // If already authenticated, redirect to the dashboard (the app's landing page
  // since story 042; "/" used to just bounce to /calendar).
  if (authStore.isAuthenticated) {
    return navigateTo("/");
  }
});
