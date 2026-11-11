// @vitest-environment nuxt
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import TaskGroupHeader from './TaskGroupHeader.vue';

// SCOPE (#170 story 25, spec §7.4/§7.5/§8): a status group's heading. A real
// h2 whose accessible name carries the count ("Needs action, 9 tasks") while
// the eye sees "Needs action 9"; 13px/600; sticky in the scroll area; the
// collapsible ones (Completed, Cancelled) are a <button aria-expanded
// aria-controls>; Cancelled is muted.

type Props = InstanceType<typeof TaskGroupHeader>['$props'];

function mountHeader(props: Partial<Props> = {}, attrs: Record<string, string> = {}) {
  return mount(TaskGroupHeader, { props: { label: 'Needs action', count: 9, ...props }, attrs });
}

describe('TaskGroupHeader', () => {
  it('is an h2 named with its count, the number alone visible', () => {
    const wrapper = mountHeader();
    const h2 = wrapper.get('h2');

    expect(h2.text()).toBe('Needs action, 9 tasks');
    expect(wrapper.get('[data-test="group-count"]').text()).toBe('9');
    // ", " and " tasks" are for screen readers only.
    expect(wrapper.findAll('.sr-only').map((s) => s.element.textContent)).toEqual([', ', ' tasks']);
    expect(h2.find('button').exists()).toBe(false);
  });

  it('says "1 task" for one', () => {
    expect(mountHeader({ count: 1 }).get('h2').text()).toBe('Needs action, 1 task');
  });

  it('uses the plan type and stays at the top of the scroll area', () => {
    const classes = mountHeader().get('h2').classes();

    expect(classes).toEqual(
      expect.arrayContaining(['text-[13px]', 'font-semibold', 'sticky', 'top-0', 'bg-surface-0', 'dark:bg-surface-900']),
    );
    expect(classes).not.toContain('uppercase');
  });

  it('takes the id the list gives it (the <ul> is labelled by it)', () => {
    expect(mountHeader({}, { id: 'g-needs-action-heading' }).get('h2').attributes('id')).toBe('g-needs-action-heading');
  });

  it('a collapsible heading is a disclosure button inside the h2', async () => {
    const wrapper = mountHeader({ label: 'Completed', count: 12, collapsible: true, expanded: false, controls: 'g-completed' });
    const button = wrapper.get('h2 > button');

    expect(button.attributes('type')).toBe('button');
    expect(button.attributes('aria-expanded')).toBe('false');
    expect(button.attributes('aria-controls')).toBe('g-completed');
    expect(button.text()).toBe('Completed, 12 tasks');
    expect(wrapper.get('[data-test="group-chevron"]').classes()).toContain('pi-chevron-right');
    expect(wrapper.get('[data-test="group-chevron"]').attributes('aria-hidden')).toBe('true');
    expect(button.classes()).toEqual(expect.arrayContaining(['focus-visible:ring-2', 'focus-visible:ring-primary-500']));

    await button.trigger('click');
    expect(wrapper.emitted('toggle')).toHaveLength(1);

    await wrapper.setProps({ expanded: true });
    expect(button.attributes('aria-expanded')).toBe('true');
    expect(wrapper.get('[data-test="group-chevron"]').classes()).toContain('pi-chevron-down');
  });

  it('muted: quiet ink for the heading', () => {
    const muted = mountHeader({ label: 'Cancelled', muted: true }).get('h2');
    const plain = mountHeader().get('h2');

    expect(muted.classes()).toEqual(expect.arrayContaining(['text-surface-500', 'dark:text-surface-400']));
    expect(muted.classes()).not.toContain('text-surface-900');
    expect(plain.classes()).toEqual(expect.arrayContaining(['text-surface-900', 'dark:text-surface-100']));
  });
});
