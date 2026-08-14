// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { ref, nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import ThemeToggle from './ThemeToggle.vue';
import type { ThemeMode } from '~/utils/theme';

// SCOPE: the toggle's own behaviour — what it renders, what it announces and how
// it responds to the keyboard. useTheme() is mocked out: it is a singleton that
// initializes once per module and would otherwise carry state between these
// tests, and its own behaviour is covered in composables/useTheme.spec.ts.
//
// The a11y assertions are not decoration. Three options where one is already
// active is a radio group, not a list of commands, so the roles and aria-checked
// are the only way a screen-reader user learns which theme is on.

const { setTheme } = vi.hoisted(() => ({ setTheme: vi.fn() }));

const themeMode = ref<ThemeMode>('system');
const isDark = ref(false);

// The outer factory runs at import time, but the object literal is only built
// when the component calls useTheme() during setup — by which point the refs
// above exist.
mockNuxtImport('useTheme', () => () => ({ themeMode, isDark, setTheme }));

function mountToggle() {
  return mount(ThemeToggle, { attachTo: document.body });
}

const trigger = (wrapper: ReturnType<typeof mountToggle>) => wrapper.get('button[aria-haspopup="menu"]');
const items = (wrapper: ReturnType<typeof mountToggle>) => wrapper.findAll('[role="menuitemradio"]');

beforeEach(() => {
  themeMode.value = 'system';
  isDark.value = false;
  setTheme.mockClear();
});

afterEach(() => {
  document.body.innerHTML = '';
});

describe('ThemeToggle — trigger', () => {
  it('starts closed, with no menu in the DOM', () => {
    const wrapper = mountToggle();
    expect(trigger(wrapper).attributes('aria-expanded')).toBe('false');
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
  });

  it('names the current mode in its accessible label', () => {
    themeMode.value = 'dark';
    const wrapper = mountToggle();
    // There is no visible text and no tooltip, so the label is the entire
    // announcement a screen reader gets.
    expect(trigger(wrapper).attributes('aria-label')).toContain('Dark');
  });

  it('shows the desktop icon under system, naming the mode rather than the outcome', () => {
    // Whether it is currently dark is visible from the page; whether it will
    // follow the device is not, so that is what the icon spends itself on.
    isDark.value = true;
    const wrapper = mountToggle();
    expect(trigger(wrapper).find('i').classes()).toContain('pi-desktop');
  });

  it.each([
    ['dark' as ThemeMode, true, 'pi-moon'],
    ['light' as ThemeMode, false, 'pi-sun'],
  ])('shows %s as %s', (mode, dark, icon) => {
    themeMode.value = mode;
    isDark.value = dark;
    const wrapper = mountToggle();
    expect(trigger(wrapper).find('i').classes()).toContain(icon);
  });
});

describe('ThemeToggle — menu', () => {
  it('opens with all three options as radio items', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');

    expect(trigger(wrapper).attributes('aria-expanded')).toBe('true');
    const options = items(wrapper);
    expect(options).toHaveLength(3);
    expect(options.map(o => o.text())).toEqual(['Light', 'Dark', 'System']);
  });

  it('marks exactly the active option as checked', async () => {
    themeMode.value = 'dark';
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');

    expect(items(wrapper).map(o => o.attributes('aria-checked'))).toEqual(['false', 'true', 'false']);
  });

  it('focuses the active option on open, so Enter re-selects rather than changes', async () => {
    themeMode.value = 'dark';
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');
    await nextTick();

    expect(document.activeElement).toBe(items(wrapper)[1]!.element);
  });

  it('selects a theme and closes', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');
    await items(wrapper)[0]!.trigger('click');

    expect(setTheme).toHaveBeenCalledWith('light');
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
  });

  it('closes on a click outside without changing anything', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await nextTick();

    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
    expect(setTheme).not.toHaveBeenCalled();
  });

  it('closes again when the trigger is clicked a second time', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');
    await trigger(wrapper).trigger('click');
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
  });
});

describe('ThemeToggle — keyboard', () => {
  it('opens on ArrowDown from the trigger', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('keydown', { key: 'ArrowDown' });
    expect(wrapper.find('[role="menu"]').exists()).toBe(true);
  });

  it('walks the options with the arrow keys, wrapping at both ends', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');
    await nextTick();
    const options = items(wrapper);

    // Opens on System (index 2, the active mode); down wraps to the top.
    await options[2]!.trigger('keydown', { key: 'ArrowDown' });
    expect(document.activeElement).toBe(options[0]!.element);

    // Up from the top wraps to the bottom.
    await options[0]!.trigger('keydown', { key: 'ArrowUp' });
    expect(document.activeElement).toBe(options[2]!.element);
  });

  it('jumps to the ends with Home and End', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');
    await nextTick();
    const options = items(wrapper);

    await options[1]!.trigger('keydown', { key: 'Home' });
    expect(document.activeElement).toBe(options[0]!.element);

    await options[0]!.trigger('keydown', { key: 'End' });
    expect(document.activeElement).toBe(options[2]!.element);
  });

  it('closes on Escape and hands focus back to the trigger', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');
    await nextTick();

    await items(wrapper)[0]!.trigger('keydown', { key: 'Escape' });
    await nextTick();

    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
    // Dropping focus to <body> would strand a keyboard user at the top of the
    // page with no idea where they were.
    expect(document.activeElement).toBe(trigger(wrapper).element);
    expect(setTheme).not.toHaveBeenCalled();
  });

  it('closes on Tab so focus does not leave an orphaned menu open', async () => {
    const wrapper = mountToggle();
    await trigger(wrapper).trigger('click');
    await nextTick();

    await items(wrapper)[0]!.trigger('keydown', { key: 'Tab' });
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
  });
});
