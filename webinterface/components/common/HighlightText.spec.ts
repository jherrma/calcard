// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import HighlightText from './HighlightText.vue';

// SCOPE: highlight/escape behavior of HighlightText.vue, INCLUDING the
// entity-corruption bug fixed in #121. Before the fix, the component ran the
// highlight regex over already-HTML-escaped text (and HTML-escaped the search
// term), so a term matching an entity fragment (e.g. "amp", or the "a" inside
// "&amp;") injected a <mark> INSIDE the entity and severed it. The regression
// tests below assert the entity stays intact; they FAIL on the pre-fix code.

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

  // --- #121 regression: entity-fragment corruption ---
  // These distinguish buggy-vs-fixed: on the pre-fix code the regex ran over the
  // already-escaped "&amp;" and injected a <mark> inside it, severing the entity.

  it('does not corrupt an & entity when the term matches an entity fragment ("amp")', () => {
    // "Tom & Jerry" contains no literal "amp"; the only "amp" the buggy code saw
    // was inside the escaped "&amp;". The fixed code matches the RAW text, finds
    // nothing, and leaves the entity intact with no <mark>.
    const wrapper = mount(HighlightText, {
      props: { text: 'Tom & Jerry', highlight: 'amp' },
    });
    const html = wrapper.html();
    // The & survives as a single intact &amp; entity...
    expect(html).toContain('Tom &amp; Jerry');
    // ...no <mark> was injected inside it...
    expect(html).not.toContain('<mark');
    // ...and the rendered text is the real ampersand, not a severed "&amp;".
    expect(wrapper.text()).toBe('Tom & Jerry');
  });

  it('marks only the raw letter, not the same letter inside an & entity ("a")', () => {
    // "Tom & apple": the fixed code marks only the "a" in "apple". The buggy code
    // also matched the "a" in the escaped "&amp;", producing a second <mark> that
    // severed the entity.
    const wrapper = mount(HighlightText, {
      props: { text: 'Tom & apple', highlight: 'a' },
    });
    const html = wrapper.html();
    // Exactly one match is marked (the "a" in "apple").
    expect(html.match(/<mark/g)?.length).toBe(1);
    expect(html).toContain('<mark class="bg-yellow-200 dark:bg-yellow-800 rounded px-0.5">a</mark>pple');
    // The & entity is untouched (no <mark> spliced into it).
    expect(html).toContain('Tom &amp; ');
    // Rendered text is intact — the real "&", not a literal "&amp;".
    expect(wrapper.text()).toBe('Tom & apple');
  });

  it('escapes markup inside the MATCHED substring too (XSS non-regression)', () => {
    // The matched (odd-index) piece is attacker-influenceable vCard data, so it
    // must be HTML-escaped inside the <mark> just like the surrounding text.
    const wrapper = mount(HighlightText, {
      props: { text: 'x<script>y', highlight: '<script>' },
    });
    const html = wrapper.html();
    // The matched payload is escaped inside the mark...
    expect(html).toContain('<mark class="bg-yellow-200 dark:bg-yellow-800 rounded px-0.5">&lt;script&gt;</mark>');
    // ...and no live <script> element is emitted.
    expect(html).not.toContain('<script');
    expect(wrapper.text()).toBe('x<script>y');
  });
});
