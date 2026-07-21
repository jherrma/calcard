/**
 * useAuthedImage loads an image that sits behind Bearer-token authentication
 * and exposes it as an object URL suitable for an `<img :src>`.
 *
 * A plain `<img>` element cannot attach an `Authorization` header, so pointing
 * it straight at a protected endpoint (e.g. a contact's `photo_url`) yields a
 * 401. Instead we fetch the bytes with the authenticated API client, wrap them
 * in a blob URL, and hand that back reactively.
 *
 * The `url` getter is watched: whenever it changes — or when the owning scope
 * is disposed — the previously created object URL is revoked so blobs never
 * leak. Relative URLs are resolved against the configured API base URL so the
 * request still reaches the API even when the SPA runs on a different origin.
 *
 * On any failure `src` stays `null`, letting callers fall back to their
 * initials/placeholder rendering.
 */
export const useAuthedImage = (url: () => string | undefined | null) => {
  const api = useApi();
  const config = useRuntimeConfig();
  const src = ref<string | null>(null);

  // Replace the current object URL, revoking the old one first so we never
  // leak a blob. Passing null clears the image.
  const setSrc = (next: string | null) => {
    if (src.value && src.value !== next) {
      URL.revokeObjectURL(src.value);
    }
    src.value = next;
  };

  watch(
    url,
    async (u, _old, onCleanup) => {
      // onCleanup fires before the next run and when the scope is disposed, so
      // it marks an in-flight fetch stale to avoid a late response overwriting
      // (and leaking) a newer image.
      let cancelled = false;
      onCleanup(() => {
        cancelled = true;
      });

      if (!u) {
        setSrc(null);
        return;
      }

      const base = (config.public.apiBaseUrl as string) || '';
      const absolute = u.startsWith('http') ? u : `${base}${u}`;

      try {
        const blob = await api<Blob>(absolute, { responseType: 'blob' });
        if (cancelled) return;
        setSrc(URL.createObjectURL(blob));
      } catch {
        // Keep the initials fallback visible.
        if (!cancelled) setSrc(null);
      }
    },
    { immediate: true },
  );

  // Revoke the last object URL when the consuming component/scope goes away.
  onScopeDispose(() => setSrc(null));

  return src;
};
