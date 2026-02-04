# CalCard Frontend

Nuxt 3 web interface for the CalDAV/CardDAV server.

## Setup

1.  **Install dependencies**

    ```bash
    pnpm install
    ```

2.  **Environment Variables**
    Copy `.env.example` to `.env` and adjust the `NUXT_PUBLIC_API_BASE_URL`.

3.  **Development**

    ```bash
    pnpm run dev
    ```

4.  **Build**
    ```bash
    pnpm run build
    ```

## Technology Stack

- **Framework**: Nuxt 3 (Vue 3)
- **State Management**: Pinia
- **UI Components**: PrimeVue (Material Theme)
- **Styling**: Tailwind CSS
- **Icons**: PrimeIcons
- **Calendar**: FullCalendar
