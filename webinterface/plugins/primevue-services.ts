import ConfirmationService from 'primevue/confirmationservice';
import Tooltip from 'primevue/tooltip';

export default defineNuxtPlugin((nuxtApp) => {
  nuxtApp.vueApp.use(ConfirmationService);
  // v-tooltip (first used by the Tasks section, #170). @primevue/nuxt-module's
  // auto-import also resolves `v-tooltip` in templates at build time, but only
  // there: registering it here makes it available app-wide whatever the module
  // options say. A tooltip shows on hover only and is not linked to its
  // element for screen readers, so every use also carries the same words as
  // visually-hidden text or an aria-label.
  nuxtApp.vueApp.directive('tooltip', Tooltip);
});
