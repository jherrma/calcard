// @vitest-environment nuxt
import { afterEach, describe, expect, it } from 'vitest';
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import TaskCheck from './TaskCheck.vue';

// SCOPE (#170 story 24, spec §7.3/§7.5/§8): the completion control. A real
// <button role="checkbox"> with aria-checked and the two labels; click, Space
// and Enter emit `toggle` exactly once; disabled is aria-disabled + a reason
// in aria-describedby (and the tooltip), still focusable, emitting nothing;
// pending swaps the ring for a spinner and emits nothing; the tick is the
// 180ms stroke-dashoffset draw, instant under reduced motion. Mounted for
// real: v-tooltip is PrimeVue's directive, its text is `el.$_ptooltipValue`.

enableAutoUnmount(afterEach);

type Props = InstanceType<typeof TaskCheck>['$props'];

function mountCheck(props: Partial<Props> = {}) {
  return mount(TaskCheck, {
    props: { checked: false, taskTitle: 'Renew passport', ...props },
    attachTo: document.body,
  });
}

type Wrapper = ReturnType<typeof mountCheck>;

const button = (wrapper: Wrapper) => wrapper.get('button');
const toggles = (wrapper: Wrapper) => wrapper.emitted('toggle')?.length ?? 0;

interface TooltipTarget extends HTMLElement {
  $_ptooltipValue?: string;
}

/** Dispatches a key event the way a browser does and says whether its default was prevented. */
function press(wrapper: Wrapper, type: 'keydown' | 'keyup', key: string, init: KeyboardEventInit = {}): boolean {
  const event = new KeyboardEvent(type, { key, bubbles: true, cancelable: true, ...init });
  button(wrapper).element.dispatchEvent(event);
  return event.defaultPrevented;
}

describe('TaskCheck — semantics', () => {
  it('is a button with role checkbox, unchecked, labelled "Complete: <title>"', () => {
    const wrapper = mountCheck();
    const el = button(wrapper);

    expect(el.element.tagName).toBe('BUTTON');
    expect(el.attributes('type')).toBe('button');
    expect(el.attributes('role')).toBe('checkbox');
    expect(el.attributes('aria-checked')).toBe('false');
    expect(el.attributes('aria-label')).toBe('Complete: Renew passport');
    expect(el.attributes('aria-disabled')).toBeUndefined();
    expect(el.attributes('aria-describedby')).toBeUndefined();
    expect(el.attributes('aria-busy')).toBeUndefined();
  });

  it('is checked and labelled "Mark as not done: <title>" for a completed task', () => {
    const wrapper = mountCheck({ checked: true });

    expect(button(wrapper).attributes('aria-checked')).toBe('true');
    expect(button(wrapper).attributes('aria-label')).toBe('Mark as not done: Renew passport');
  });

  it('follows the checked prop and the title', async () => {
    const wrapper = mountCheck();
    await wrapper.setProps({ checked: true, taskTitle: 'Send invoice 1042' });

    expect(button(wrapper).attributes('aria-checked')).toBe('true');
    expect(button(wrapper).attributes('aria-label')).toBe('Mark as not done: Send invoice 1042');
  });

  it('has a 44x44 hit area around the 24px ring', () => {
    const wrapper = mountCheck();

    expect(wrapper.classes()).toEqual(expect.arrayContaining(['h-6', 'w-6']));
    expect(button(wrapper).classes()).toEqual(expect.arrayContaining(['h-11', 'w-11']));
    expect(wrapper.get('[data-test="check-ring"]').classes()).toEqual(
      expect.arrayContaining(['h-6', 'w-6', 'rounded-full', 'border-2']),
    );
  });

  it('draws the ring in surface-500 / dark surface-400 (3:1 for a control) and fills it with the accent when checked', async () => {
    const wrapper = mountCheck();
    const ring = () => wrapper.get('[data-test="check-ring"]').classes();

    expect(ring()).toEqual(expect.arrayContaining(['border-surface-500', 'dark:border-surface-400']));
    expect(ring()).not.toContain('border-surface-400');
    expect(ring()).not.toContain('bg-primary-600');

    await wrapper.setProps({ checked: true });
    expect(ring()).toEqual(
      expect.arrayContaining(['border-primary-600', 'bg-primary-600', 'dark:border-primary-400', 'dark:bg-primary-400']),
    );
  });

  it('shows the project focus ring around the visible circle, not around the larger hit area', () => {
    const wrapper = mountCheck();

    expect(button(wrapper).classes()).toContain('focus:outline-none');
    expect(wrapper.get('[data-test="check-ring"]').classes()).toEqual(
      expect.arrayContaining(['group-focus-visible:ring-2', 'group-focus-visible:ring-primary-500']),
    );
  });
});

describe('TaskCheck — toggling', () => {
  it('emits toggle once per click', async () => {
    const wrapper = mountCheck();
    await button(wrapper).trigger('click');
    await button(wrapper).trigger('click');

    expect(toggles(wrapper)).toBe(2);
    expect(wrapper.emitted('toggle')![0]).toEqual([]);
  });

  it.each([
    ['Enter', 'Enter'],
    ['Space', ' '],
  ])('emits toggle once on %s and prevents the button from also clicking', (_name, key) => {
    const wrapper = mountCheck();

    expect(press(wrapper, 'keydown', key)).toBe(true);
    expect(toggles(wrapper)).toBe(1);
    // The browser clicks a button on Space's keyup: that default is cancelled too.
    if (key === ' ') expect(press(wrapper, 'keyup', key)).toBe(true);
    expect(toggles(wrapper)).toBe(1);
  });

  it('ignores the auto-repeat of a held key and other keys', () => {
    const wrapper = mountCheck();

    press(wrapper, 'keydown', 'Enter');
    press(wrapper, 'keydown', 'Enter', { repeat: true });
    press(wrapper, 'keydown', ' ', { repeat: true });
    expect(press(wrapper, 'keydown', 'a')).toBe(false);
    expect(press(wrapper, 'keydown', 'Tab')).toBe(false);

    expect(toggles(wrapper)).toBe(1);
  });
});

describe('TaskCheck — disabled', () => {
  const REASON = 'Repeating tasks can be completed in your task app';

  it('is aria-disabled with the reason in aria-describedby and in the tooltip, and stays focusable', () => {
    const wrapper = mountCheck({ disabledReason: REASON });
    const el = button(wrapper);

    expect(el.attributes('aria-disabled')).toBe('true');
    const describedBy = el.attributes('aria-describedby');
    expect(describedBy).toBeTruthy();
    const reason = wrapper.get(`[id="${describedBy}"]`);
    expect(reason.text()).toBe(REASON);
    expect(reason.classes()).toContain('sr-only');
    // Not the `disabled` attribute: that would take it out of the tab order,
    // and the reason with it.
    expect(el.attributes('disabled')).toBeUndefined();
    expect(el.attributes('tabindex')).toBeUndefined();
    expect((el.element as TooltipTarget).$_ptooltipValue).toBe(REASON);
    expect(el.classes()).toContain('cursor-not-allowed');
  });

  it('emits nothing on click, Enter or Space', async () => {
    const wrapper = mountCheck({ disabledReason: REASON });

    await button(wrapper).trigger('click');
    press(wrapper, 'keydown', 'Enter');
    press(wrapper, 'keydown', ' ');
    press(wrapper, 'keyup', ' ');

    expect(toggles(wrapper)).toBe(0);
  });

  it('keeps aria-checked of a completed task it cannot untick', () => {
    const wrapper = mountCheck({ checked: true, disabledReason: 'This list is read only' });

    expect(button(wrapper).attributes('aria-checked')).toBe('true');
    expect(button(wrapper).attributes('aria-label')).toBe('Mark as not done: Renew passport');
  });

  it('has no reason, no aria-disabled and no tooltip when it can be ticked', () => {
    const wrapper = mountCheck({ disabledReason: null });

    expect(button(wrapper).attributes('aria-disabled')).toBeUndefined();
    expect(wrapper.find('[data-test="check-reason"]').exists()).toBe(false);
    // An empty value leaves the directive unbound: nothing to show on hover.
    expect((button(wrapper).element as TooltipTarget).$_ptooltipValue).toBeUndefined();
  });

  it('gives each check of a list its own reason id', () => {
    // useId() is unique per app, so both checks go into one.
    const Pair = defineComponent({
      setup: () => () =>
        h('ul', [
          h(TaskCheck, { checked: false, taskTitle: 'A', disabledReason: REASON }),
          h(TaskCheck, { checked: false, taskTitle: 'B', disabledReason: REASON }),
        ]),
    });
    const wrapper = mount(Pair);
    const ids = wrapper.findAll('button').map((b) => b.attributes('aria-describedby'));

    expect(ids[0]).toBeTruthy();
    expect(ids[0]).not.toBe(ids[1]);
    expect(wrapper.get(`[id="${ids[1]}"]`).text()).toBe(REASON);
  });
});

describe('TaskCheck — pending', () => {
  it('replaces the ring with a spinner, is aria-busy and emits nothing', async () => {
    const wrapper = mountCheck({ checked: true, pending: true });

    expect(wrapper.find('[data-test="check-spinner"]').exists()).toBe(true);
    expect(wrapper.get('[data-test="check-spinner"]').classes()).toContain('motion-safe:animate-spin');
    expect(wrapper.find('[data-test="check-ring"]').exists()).toBe(false);
    expect(button(wrapper).attributes('aria-busy')).toBe('true');
    // The optimistic state is still what the checkbox says.
    expect(button(wrapper).attributes('aria-checked')).toBe('true');

    await button(wrapper).trigger('click');
    press(wrapper, 'keydown', 'Enter');
    press(wrapper, 'keydown', ' ');
    expect(toggles(wrapper)).toBe(0);

    await wrapper.setProps({ pending: false });
    expect(wrapper.find('[data-test="check-spinner"]').exists()).toBe(false);
    expect(button(wrapper).attributes('aria-busy')).toBeUndefined();
    await button(wrapper).trigger('click');
    expect(toggles(wrapper)).toBe(1);
  });
});

describe('TaskCheck — completion motion', () => {
  const mark = (wrapper: Wrapper) => wrapper.get('[data-test="check-mark"]');

  it('hides the check stroke (dash offset 1 of a normalised path) until checked', () => {
    const wrapper = mountCheck();

    expect(mark(wrapper).attributes('pathLength')).toBe('1');
    const style = (mark(wrapper).element as SVGPathElement).style;
    expect(style.strokeDasharray).toBe('1');
    expect(style.strokeDashoffset).toBe('1');
  });

  it('draws the stroke on in place over 180ms when ticked (same element, so the transition runs)', async () => {
    const wrapper = mountCheck();
    const before = mark(wrapper).element;

    await wrapper.setProps({ checked: true });

    expect(mark(wrapper).element).toBe(before);
    expect((mark(wrapper).element as SVGPathElement).style.strokeDashoffset).toBe('0');
    expect(mark(wrapper).classes()).toEqual(
      expect.arrayContaining(['transition-[stroke-dashoffset]', 'duration-[180ms]']),
    );
    expect(wrapper.get('[data-test="check-ring"]').classes()).toEqual(
      expect.arrayContaining(['transition-colors', 'duration-[180ms]']),
    );
  });

  it('is instant under prefers-reduced-motion: both transitions are switched off', () => {
    const wrapper = mountCheck();

    expect(mark(wrapper).classes()).toContain('motion-reduce:transition-none');
    expect(wrapper.get('[data-test="check-ring"]').classes()).toContain('motion-reduce:transition-none');
  });
});

describe('TaskCheck — roving tabindex (story 25)', () => {
  it('is in the Tab order by default and leaves it when not tabbable, still clickable', async () => {
    expect(button(mountCheck()).attributes('tabindex')).toBeUndefined();

    const wrapper = mountCheck({ tabbable: false });
    expect(button(wrapper).attributes('tabindex')).toBe('-1');
    await button(wrapper).trigger('click');
    expect(toggles(wrapper)).toBe(1);

    await wrapper.setProps({ tabbable: true });
    expect(button(wrapper).attributes('tabindex')).toBeUndefined();
  });
});
