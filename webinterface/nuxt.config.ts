import { definePreset } from '@primeuix/themes';
import Material from '@primeuix/themes/material';
import { DARK_MODE_CLASS, DARK_MEDIA_QUERY, DEFAULT_THEME_MODE, THEME_STORAGE_KEY } from './utils/theme';

/**
 * Flash-prevention script, inlined into <head> (story 046).
 *
 * `ssr: false` means index.html paints the shell before any bundle executes, so
 * a dark-mode user would see a white page for as long as the JS takes to load
 * and mount. This runs first, synchronously, and sets the class the CSS is
 * already keyed to — so the first paint is correct and nothing has to animate.
 *
 * It imports its constants from `utils/theme.ts` rather than repeating them,
 * because a silent disagreement over the storage key would look exactly like
 * "the toggle doesn't persist". The logic still has to be duplicated (this
 * cannot import at runtime), so keep it in step with `readStoredThemeMode` +
 * `resolveTheme`: any value that is not exactly 'dark' or 'light' — including
 * junk and a missing key — is treated as 'system'.
 */
const themeInitScript = `
(function(){try{
var m=localStorage.getItem(${JSON.stringify(THEME_STORAGE_KEY)})||${JSON.stringify(DEFAULT_THEME_MODE)};
var d=m==='dark'||(m!=='light'&&window.matchMedia(${JSON.stringify(DARK_MEDIA_QUERY)}).matches);
if(d){document.documentElement.classList.add(${JSON.stringify(DARK_MODE_CLASS)});}
document.documentElement.style.colorScheme=d?'dark':'light';
}catch(e){}})();
`.trim();

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
      include: ['Button', 'InputText', 'Dialog', 'Toast', 'Menu', 'Avatar', 'DataTable', 'Column', 'Card', 'Password', 'Checkbox', 'Message', 'ProgressSpinner', 'SelectButton', 'DatePicker', 'InputSwitch', 'InputNumber', 'Textarea', 'RadioButton', 'ToggleButton', 'Select', 'ConfirmDialog', 'Tag', 'ColorPicker', 'Tabs', 'TabList', 'Tab', 'TabPanels', 'TabPanel', 'Divider', 'Accordion', 'AccordionPanel', 'AccordionHeader', 'AccordionContent'],
    },
  },

  // theme.css is unlayered so it outranks the Tailwind/PrimeVue layers above.
  css: ['primeicons/primeicons.css', '~/assets/css/theme.css'],

  tailwindcss: {
    cssPath: '~/assets/css/tailwind.css',
  },

  runtimeConfig: {
    public: {
      // Empty string = same-origin relative requests, for the single-container
      // production build where the Go server serves this SPA too (#99). Baked
      // in at build time (ssr: false), so the prod image must NOT set
      // NUXT_PUBLIC_API_BASE_URL to keep requests same-origin.
      apiBaseUrl: process.env.NUXT_PUBLIC_API_BASE_URL || '',
    },
  },

  // `pnpm dev` serves the SPA on :3000 against the Go server on :8080, so a
  // relative base won't reach the API — keep the localhost default there only.
  // ($development is a built-in Nuxt env override key applied during dev.)
  $development: {
    runtimeConfig: {
      public: {
        apiBaseUrl: process.env.NUXT_PUBLIC_API_BASE_URL || 'http://localhost:8080',
      },
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
        { rel: 'preconnect', href: 'https://fonts.googleapis.com' },
        { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' },
        { rel: 'stylesheet', href: 'https://fonts.googleapis.com/css2?family=Open+Sans:ital,wght@0,300..800;1,300..800&display=swap' },
      ],
      script: [
        // Must stay first and stay synchronous — it only helps if it runs before
        // the browser paints the shell.
        { innerHTML: themeInitScript, tagPosition: 'head' },
      ],
    },
  },

  typescript: {
    strict: true,
  },

  compatibilityDate: '2024-01-01',
});
