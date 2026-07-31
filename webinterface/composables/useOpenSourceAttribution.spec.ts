// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import { filterOpenSourcePackages, useOpenSourceAttribution } from './useOpenSourceAttribution';
import type { OpenSourcePackage } from '~/types/about';

// SCOPE: the name filter (story 101) and the two independent manifest loads.
const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
mockNuxtImport('useApi', () => () => apiMock);

function pkg(name: string, version = '1.0.0', license = 'MIT'): OpenSourcePackage {
  return { name, version, license, url: `https://example.com/${name}` };
}

const packages: OpenSourcePackage[] = [
  pkg('github.com/gofiber/fiber/v3', 'v3.0.2'),
  pkg('golang.org/x/crypto', 'v0.45.0', 'BSD-3-Clause'),
  pkg('@fullcalendar/core', '6.1.21'),
  pkg('Vue', '3.5.40'),
];

describe('filterOpenSourcePackages', () => {
  it('returns every package for an empty or whitespace-only term', () => {
    expect(filterOpenSourcePackages(packages, '')).toHaveLength(4);
    expect(filterOpenSourcePackages(packages, '   ')).toHaveLength(4);
  });

  it('matches a case-insensitive substring of the name', () => {
    expect(filterOpenSourcePackages(packages, 'FIBER').map((p) => p.name)).toEqual([
      'github.com/gofiber/fiber/v3',
    ]);
    expect(filterOpenSourcePackages(packages, 'vue').map((p) => p.name)).toEqual(['Vue']);
  });

  it('matches scoped npm names including the @ and /', () => {
    expect(filterOpenSourcePackages(packages, '@fullcalendar/').map((p) => p.name)).toEqual([
      '@fullcalendar/core',
    ]);
  });

  it('ignores surrounding whitespace in the term', () => {
    expect(filterOpenSourcePackages(packages, '  crypto  ').map((p) => p.name)).toEqual([
      'golang.org/x/crypto',
    ]);
  });

  it('returns nothing when no name matches', () => {
    expect(filterOpenSourcePackages(packages, 'nonexistent')).toEqual([]);
  });

  it('does NOT match on version or license — only the name is searched', () => {
    expect(filterOpenSourcePackages(packages, 'BSD-3-Clause')).toEqual([]);
    expect(filterOpenSourcePackages(packages, '6.1.21')).toEqual([]);
  });

  it('leaves the input array untouched', () => {
    const input = [...packages];
    filterOpenSourcePackages(input, 'vue');
    expect(input).toEqual(packages);
  });
});

// Minimal Response stand-in for the static-asset fetch. `ok: false` and a
// rejecting json() cover the two ways the manifest request can go wrong.
function fetchStub(body: unknown, ok = true) {
  return vi.fn().mockResolvedValue({
    ok,
    status: ok ? 200 : 404,
    json: async () => {
      if (typeof body === 'string') throw new SyntaxError('Unexpected token <');
      return body;
    },
  });
}

describe('useOpenSourceAttribution', () => {
  beforeEach(() => {
    apiMock.mockReset();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('loads both manifests independently', async () => {
    apiMock.mockResolvedValue({ generator: 'go', note: 'best-effort', count: 1, packages: [pkg('gorm.io/gorm')] });
    vi.stubGlobal(
      'fetch',
      fetchStub({ generator: 'npm', note: 'declared', count: 1, packages: [pkg('vue')] }),
    );

    const attribution = useOpenSourceAttribution();
    await attribution.load();

    expect(attribution.loading.value).toBe(false);
    expect(attribution.backendPackages.value.map((p) => p.name)).toEqual(['gorm.io/gorm']);
    expect(attribution.frontendPackages.value.map((p) => p.name)).toEqual(['vue']);
    expect(attribution.backendNote.value).toBe('best-effort');
    expect(attribution.frontendNote.value).toBe('declared');
    expect(attribution.backendError.value).toBeNull();
    expect(attribution.frontendError.value).toBeNull();
  });

  it('keeps the frontend list when the backend endpoint fails', async () => {
    apiMock.mockRejectedValue(new Error('401'));
    vi.stubGlobal('fetch', fetchStub({ generator: 'npm', note: '', count: 1, packages: [pkg('vue')] }));

    const attribution = useOpenSourceAttribution();
    await attribution.load();

    expect(attribution.backendPackages.value).toEqual([]);
    expect(attribution.backendError.value).toContain('backend');
    expect(attribution.frontendPackages.value).toHaveLength(1);
    expect(attribution.frontendError.value).toBeNull();
  });

  it('treats a non-manifest response as an error (SPA index.html fallback)', async () => {
    apiMock.mockResolvedValue({ generator: 'go', note: '', count: 0, packages: [] });
    // A missing public/open-source.json is answered with the SPA shell, not a 404.
    vi.stubGlobal('fetch', fetchStub('<!DOCTYPE html><html></html>'));

    const attribution = useOpenSourceAttribution();
    await attribution.load();

    expect(attribution.frontendPackages.value).toEqual([]);
    expect(attribution.frontendError.value).toContain('frontend');
    expect(attribution.backendError.value).toBeNull();
  });
});
