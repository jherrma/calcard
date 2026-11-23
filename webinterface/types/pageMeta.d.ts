// Custom keys of `definePageMeta`, typed by augmenting Nuxt's PageMeta.
declare module '#app' {
  interface PageMeta {
    /**
     * Keep the app sidebar closed at every width, as on a phone. The header's
     * menu button then opens it as an overlay. For pages that need the width.
     */
    hideSidebar?: boolean;
  }
}

export {};
