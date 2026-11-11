// @vitest-environment nuxt
import { describe, it, expect, vi } from 'vitest';
import ConfirmationService from 'primevue/confirmationservice';
import Tooltip from 'primevue/tooltip';
import primevueServices from './primevue-services';

// SCOPE: the app-wide PrimeVue services. #170 story 22 added the Tooltip
// directive (the Tasks sidebar's "Read only" lock is its first user); the
// ConfirmationService behind useConfirm() was there before.
describe('primevue-services plugin', () => {
  it('installs the confirmation service and registers v-tooltip', () => {
    const vueApp = { use: vi.fn(), directive: vi.fn() };

    (primevueServices as unknown as (app: { vueApp: typeof vueApp }) => void)({ vueApp });

    expect(vueApp.use).toHaveBeenCalledWith(ConfirmationService);
    expect(vueApp.directive).toHaveBeenCalledWith('tooltip', Tooltip);
  });
});
