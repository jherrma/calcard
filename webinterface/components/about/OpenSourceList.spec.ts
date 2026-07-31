// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import OpenSourceList from './OpenSourceList.vue';
import HighlightText from '~/components/common/HighlightText.vue';
import type { OpenSourcePackage } from '~/types/about';

// SCOPE: how the list renders the filtered result (story 101) — the incremental
// "Show N more" cap and its reset on a new filter term. PrimeVue pieces are
// stubbed: we assert list behaviour, not PrimeVue internals.
const stubs = {
  Message: { template: '<div class="stub-message"><slot /></div>' },
  Tag: { props: ['value'], template: '<span class="stub-tag">{{ value }}</span>' },
  Button: { props: ['label'], template: '<button class="stub-button">{{ label }}</button>' },
  CommonSkeletonList: { template: '<div class="stub-skeleton" />' },
  CommonHighlightText: HighlightText,
};

function packages(n: number, prefix = 'pkg'): OpenSourcePackage[] {
  return Array.from({ length: n }, (_, i) => ({
    name: `${prefix}-${i}`,
    version: `1.0.${i}`,
    license: 'MIT',
    url: `https://example.com/${prefix}-${i}`,
  }));
}

function mountList(props: {
  packages: OpenSourcePackage[];
  filter?: string;
  loading?: boolean;
  error?: string | null;
}) {
  return mount(OpenSourceList, {
    props: { title: 'Backend', icon: 'pi pi-server', ...props },
    global: { stubs },
  });
}

describe('AboutOpenSourceList', () => {
  it('renders only the packages matching the filter, each linking to its repository', () => {
    const wrapper = mountList({
      packages: [...packages(2, 'alpha'), ...packages(1, 'beta')],
      filter: 'beta',
    });

    const links = wrapper.findAll('a');
    expect(links).toHaveLength(1);
    expect(links[0]!.attributes('href')).toBe('https://example.com/beta-0');
    expect(links[0]!.attributes('rel')).toBe('noopener noreferrer');
    expect(wrapper.text()).toContain('1 of 3');
  });

  it('highlights the matched part of the name', () => {
    const wrapper = mountList({ packages: packages(1, 'gorm'), filter: 'orm' });
    expect(wrapper.html()).toContain('<mark');
  });

  it('caps the initial render and grows it on demand', async () => {
    const wrapper = mountList({ packages: packages(230) });

    expect(wrapper.findAll('li')).toHaveLength(100);
    expect(wrapper.find('.stub-button').text()).toBe('Show 130 more');

    await wrapper.find('.stub-button').trigger('click');
    expect(wrapper.findAll('li')).toHaveLength(200);

    await wrapper.find('.stub-button').trigger('click');
    expect(wrapper.findAll('li')).toHaveLength(230);
    expect(wrapper.find('.stub-button').exists()).toBe(false);
  });

  it('resets the cap when the filter changes', async () => {
    const wrapper = mountList({ packages: packages(230) });
    await wrapper.find('.stub-button').trigger('click');
    expect(wrapper.findAll('li')).toHaveLength(200);

    await wrapper.setProps({ filter: 'pkg' });
    expect(wrapper.findAll('li')).toHaveLength(100);
  });

  it('shows the error instead of the list, and a skeleton while loading', () => {
    const failed = mountList({ packages: [], error: 'boom' });
    expect(failed.find('.stub-message').text()).toBe('boom');
    expect(failed.findAll('li')).toHaveLength(0);

    const busy = mountList({ packages: [], loading: true });
    expect(busy.find('.stub-skeleton').exists()).toBe(true);
  });

  it('explains an undetected license instead of implying the package is unlicensed', () => {
    const wrapper = mountList({
      packages: [{ name: 'mystery', version: '1.0.0', license: 'unknown', url: 'https://example.com/mystery' }],
    });
    expect(wrapper.find('.stub-tag').text()).toBe('unknown');
  });
});
