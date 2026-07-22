// @vitest-environment nuxt
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';
import ContactForm from './ContactForm.vue';
import type { AddressBook } from '~/types/contacts';

// SCOPE: the object-URL leak fixed in #25. ContactForm used to build the photo
// preview via URL.createObjectURL(...) INSIDE a computed, so every recompute
// allocated a fresh blob: URL and the previous one was never revoked — leaking
// for the tab's lifetime. The fix allocates the URL explicitly on file select,
// revokes the previous one before replacing it, and revokes on unmount. These
// tests assert create-count === revoke-count (nothing leaks) and that a remote
// https photo URL (owned by useAuthedImage, not us) is never revoked. They FAIL
// on the pre-fix code, where revokeObjectURL is never called.

// Mock the authed-image composable so we can inject an existing (remote) photo
// URL without any network / useApi dependency. hoisted so it is safe to
// reference from the (hoisted) mockNuxtImport factory.
const { authedImageRef, useAuthedImageMock } = vi.hoisted(() => {
  const authedImageRef: { value: string | null } = { value: null };
  return { authedImageRef, useAuthedImageMock: (_url?: unknown) => authedImageRef };
});

mockNuxtImport('useAuthedImage', () => useAuthedImageMock);

interface FormVm {
  photoPreview: string | null;
  onFileSelected: (event: Event) => void;
  removePhoto: () => void;
}

let createCount = 0;
let createObjectURL: ReturnType<typeof vi.fn>;
let revokeObjectURL: ReturnType<typeof vi.fn>;

// A file selection event shaped just enough for onFileSelected (reads
// event.target.files?.[0]). The file object itself only needs to be truthy —
// URL.createObjectURL is stubbed and ignores it.
function fileEvent(name: string): Event {
  const file = { name, type: 'image/png' } as unknown as File;
  return { target: { files: [file] } } as unknown as Event;
}

function mountForm() {
  const addressBooks = [
    { ID: 1, UUID: 'ab-1', Name: 'Personal' } as unknown as AddressBook,
  ];
  const wrapper = mount(ContactForm, {
    props: { addressBooks },
    shallow: true,
  });
  return { wrapper, vm: wrapper.vm as unknown as FormVm };
}

beforeEach(() => {
  createCount = 0;
  createObjectURL = vi.fn(() => `blob:mock-${++createCount}`);
  revokeObjectURL = vi.fn();
  authedImageRef.value = null;
  // Augment the real URL constructor so `new URL()` elsewhere still works.
  vi.stubGlobal('URL', Object.assign(globalThis.URL, { createObjectURL, revokeObjectURL }));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ContactForm photo preview object-URL lifecycle', () => {
  it('revokes the previous object URL on replacement and on unmount (no leak)', async () => {
    const { wrapper, vm } = mountForm();

    // First selection allocates one blob URL, revokes nothing.
    vm.onFileSelected(fileEvent('a.png'));
    await nextTick();
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledTimes(0);
    expect(vm.photoPreview).toBe('blob:mock-1');

    // Selecting another photo must revoke the previous URL before allocating.
    vm.onFileSelected(fileEvent('b.png'));
    await nextTick();
    expect(createObjectURL).toHaveBeenCalledTimes(2);
    expect(revokeObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-1');
    expect(vm.photoPreview).toBe('blob:mock-2');

    // Unmount revokes the currently-held URL.
    wrapper.unmount();
    expect(revokeObjectURL).toHaveBeenCalledTimes(2);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-2');

    // Every allocated URL was revoked exactly once — nothing leaked.
    expect(revokeObjectURL.mock.calls.length).toBe(createObjectURL.mock.calls.length);
  });

  it('revokes the object URL when the photo is removed', async () => {
    const { vm } = mountForm();

    vm.onFileSelected(fileEvent('a.png'));
    await nextTick();
    expect(createObjectURL).toHaveBeenCalledTimes(1);

    vm.removePhoto();
    await nextTick();
    expect(revokeObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-1');
    expect(vm.photoPreview).toBeNull();
  });

  it('never revokes a remote https photo URL it did not allocate', async () => {
    authedImageRef.value = 'https://cdn.example.com/photo.jpg';
    const { wrapper, vm } = mountForm();

    // With no local file selected the preview is the remote URL, untouched.
    expect(vm.photoPreview).toBe('https://cdn.example.com/photo.jpg');

    // Drive the full allocate -> replace -> unmount lifecycle.
    vm.onFileSelected(fileEvent('a.png'));
    await nextTick();
    vm.onFileSelected(fileEvent('b.png'));
    await nextTick();
    wrapper.unmount();

    const revokedArgs = revokeObjectURL.mock.calls.map((c) => c[0]);
    // The remote URL is never passed to revoke...
    expect(revokedArgs).not.toContain('https://cdn.example.com/photo.jpg');
    // ...and everything we did revoke is a blob: URL we allocated.
    for (const arg of revokedArgs) {
      expect(String(arg).startsWith('blob:')).toBe(true);
    }
  });
});
