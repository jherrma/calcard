// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import HighlightText from './HighlightText.vue';

// SCOPE: only behaviors that are CURRENTLY correct. HighlightText.vue has a known
// entity-corruption bug fixed separately in #121 — entity-fragment cases (e.g.
// searching "amp", or a term containing "&") are intentionally NOT tested here.

describe('HighlightText', () => {
  it('wraps a matching term in <mark>, case-insensitively, preserving original casing', () => {
    const wrapper = mount(HighlightText, {
      props: { text: 'Hello World', highlight: 'world' },
    });
    const html = wrapper.html();
    expect(html).toContain('<mark');
    // The captured match keeps the source casing ("World"), not the query casing.
    expect(html).toContain('>World</mark>');
  });

  it('escapes HTML before injecting <mark> so vCard text cannot smuggle in markup', () => {
    const wrapper = mount(HighlightText, {
      props: { text: '<script>alert', highlight: 'alert' },
    });
    const html = wrapper.html();
    // The angle brackets stay escaped...
    expect(html).toContain('&lt;script&gt;');
    // ...no real <script> element is emitted...
    expect(html).not.toContain('<script');
    // ...and the term is still highlighted.
    expect(html).toContain('>alert</mark>');
  });

  it('short-circuits with no <mark> when highlight is an empty string', () => {
    const wrapper = mount(HighlightText, {
      props: { text: 'Hello', highlight: '' },
    });
    expect(wrapper.html()).not.toContain('<mark');
    expect(wrapper.text()).toBe('Hello');
  });

  it('short-circuits with no <mark> when highlight is absent', () => {
    const wrapper = mount(HighlightText, {
      props: { text: 'Hello' },
    });
    expect(wrapper.html()).not.toContain('<mark');
    expect(wrapper.text()).toBe('Hello');
  });
});
