/**
 * Points every PrimeVue DatePicker at the user's first day of the week.
 *
 * App-wide rather than per component because the DatePicker reads the week
 * start from PrimeVue's global locale, not from a prop. Logout resets the
 * preferences store, which brings the pickers back to Sunday along with it.
 */
export default defineNuxtPlugin((nuxtApp) => {
  useDatePickerFirstDay(nuxtApp.vueApp.config.globalProperties.$primevue?.config);
});
