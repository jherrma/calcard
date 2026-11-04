// @vitest-environment nuxt
import { describe, expect, it } from 'vitest';
import { mount } from '@vue/test-utils';
import { defineComponent, h } from 'vue';
import TaskEmptyState from './TaskEmptyState.vue';
import type { TaskEmptyVariant } from '~/types/tasks';

// SCOPE (#170 story 25, spec §7.5, DECISIONS 4): the four empty states, with
// the spec's exact copy, one action each (none for an empty list: the
// quick-add takes the focus there, story 26) and decorative marks only.

const ButtonStub = defineComponent({
  name: 'Button',
  props: { label: { type: String, default: '' } },
  emits: ['click'],
  setup(props, { emit }) {
    return () => h('button', { type: 'button', onClick: (e: Event) => emit('click', e) }, props.label);
  },
});

function mountState(variant: TaskEmptyVariant) {
  return mount(TaskEmptyState, { props: { variant }, global: { stubs: { Button: ButtonStub } } });
}

describe('TaskEmptyState', () => {
  it.each([
    ['no-lists', 'No task lists yet', 'Create a list to start adding tasks. Task apps on your phone will find it too.', 'Create task list', 'create-list'],
    ['empty', 'Nothing to do here', null, null, null],
    ['no-match', 'No tasks match', null, 'Clear filters', 'clear-filters'],
    ['all-done', 'All caught up', null, 'Show completed', 'show-completed'],
  ] as const)('%s: "%s"', async (variant, heading, text, action, event) => {
    const wrapper = mountState(variant);

    expect(wrapper.get('h2').text()).toBe(heading);
    const body = wrapper.find('[data-test="empty-text"]');
    if (text) expect(body.text()).toBe(text);
    else expect(body.exists()).toBe(false);

    const button = wrapper.find('[data-test="empty-action"]');
    if (!action) {
      expect(button.exists()).toBe(false);
      return;
    }
    expect(button.text()).toBe(action);
    await button.trigger('click');
    expect(Object.keys(wrapper.emitted())).toContain(event);
    expect(wrapper.emitted(event)).toHaveLength(1);
  });

  it('"Show completed" is a link-style button in the accent ink', () => {
    const link = mountState('all-done').get('[data-test="empty-action"]');

    expect(link.element.tagName).toBe('BUTTON');
    expect(link.classes()).toEqual(expect.arrayContaining(['text-primary-700', 'dark:text-primary-300', 'hover:underline']));
  });

  it.each(['no-lists', 'empty', 'no-match', 'all-done'] as const)('%s: the mark is decorative', (variant) => {
    expect(mountState(variant).get('[data-test="empty-mark"]').attributes('aria-hidden')).toBe('true');
  });

  it('the empty and all-done marks echo the completion control: a hollow ring and a filled tick', () => {
    expect(mountState('empty').get('[data-test="empty-mark"]').classes()).toEqual(
      expect.arrayContaining(['rounded-full', 'border-dashed']),
    );
    const done = mountState('all-done').get('[data-test="empty-mark"]');
    expect(done.classes()).toEqual(expect.arrayContaining(['rounded-full', 'bg-primary-600', 'dark:bg-primary-400']));
    expect(done.find('path').exists()).toBe(true);
  });
});
