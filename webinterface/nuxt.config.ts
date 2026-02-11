import { definePreset } from '@primeuix/themes';
import Material from '@primeuix/themes/material';

const MyPreset = definePreset(Material, {
    semantic: {
        primary: {
            50: '{blue.50}',
            100: '{blue.100}',
            200: '{blue.200}',
            300: '{blue.300}',
            400: '{blue.400}',
            500: '{blue.500}',
            600: '{blue.600}',
            700: '{blue.700}',
            800: '{blue.800}',
            900: '{blue.900}',
            950: '{blue.950}'
        },
        borderRadius: {
            sm: '8px',
            md: '12px',
            lg: '16px',
            xl: '24px'
        }
    },
    components: {
        card: {
            root: {
                borderRadius: '{borderRadius.xl}',
                shadow: '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05)'
            }
        },
        button: {
            root: {
                borderRadius: '2rem' // Pill shape for buttons
            }
        },
        inputtext: {
            root: {
                borderRadius: '{borderRadius.md}'
            }
        }
    }
});

// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  devtools: { enabled: true },

  ssr: false, // SPA mode

  modules: [
    '@pinia/nuxt',
    '@primevue/nuxt-module',
    '@nuxtjs/tailwindcss',
    '@vueuse/nuxt',
  ],

  primevue: {
    options: {
      theme: {
        preset: MyPreset,
        options: {
            darkModeSelector: '.dark-mode',
            cssLayer: {
                name: 'primevue',
                order: 'tailwind-base, primevue, tailwind-utilities'
            }
        }
      },
    },
    components: {
      include: ['Button', 'InputText', 'Dialog', 'Toast', 'Menu', 'Avatar', 'DataTable', 'Column', 'Card', 'Password', 'Checkbox', 'Message', 'ProgressSpinner', 'SelectButton', 'DatePicker', 'InputSwitch', 'InputNumber', 'Textarea', 'RadioButton', 'ToggleButton', 'Select', 'ConfirmDialog'],
    },
  },

  tailwindcss: {
    cssPath: '~/assets/css/tailwind.css',
  },

  runtimeConfig: {
    public: {
      apiBaseUrl: process.env.NUXT_PUBLIC_API_BASE_URL || 'http://localhost:8080',
    },
  },

  app: {
    head: {
      title: 'CalDAV Server',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
      link: [
        { rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' },
      ],
    },
  },

  typescript: {
    strict: true,
  },

  compatibilityDate: '2024-01-01',
});
