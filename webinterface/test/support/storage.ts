/**
 * Web Storage doubles for specs.
 *
 * The Nuxt + happy-dom test environment exposes `window.localStorage` as a PLAIN
 * EMPTY OBJECT — `typeof window.localStorage === 'object'`, but it has no
 * `getItem`, no `setItem` and no `clear`. Node 22's built-in Web Storage wins
 * over happy-dom's implementation and stays inert without `--localstorage-file`
 * (hence the `--localstorage-file was provided without a valid path` warning in
 * the test output). So any spec that seeds or asserts persisted state has to
 * bring its own storage; `window.localStorage.clear()` throws
 * "clear is not a function" rather than doing nothing.
 *
 * Production code is unaffected — real browsers have a real localStorage. Code
 * under test should still guard its access, because Safari private mode and
 * blocked cookies make the real one THROW rather than return null.
 */

interface InstalledStorage {
  /** The backing map, for direct seeding/inspection. */
  data: Map<string, string>;
  /** Puts the environment's original descriptor back. */
  restore: () => void;
}

function replaceLocalStorage(descriptor: PropertyDescriptor): () => void {
  const original = Object.getOwnPropertyDescriptor(window, 'localStorage');
  Object.defineProperty(window, 'localStorage', { configurable: true, ...descriptor });
  return () => {
    if (original) {
      Object.defineProperty(window, 'localStorage', original);
    } else {
      delete (window as unknown as Record<string, unknown>).localStorage;
    }
  };
}

/** A working, empty, in-memory `window.localStorage`. */
export function installMemoryStorage(): InstalledStorage {
  const data = new Map<string, string>();
  const storage: Storage = {
    getItem: (key: string) => (data.has(key) ? data.get(key)! : null),
    setItem: (key: string, value: string) => {
      data.set(String(key), String(value));
    },
    removeItem: (key: string) => {
      data.delete(key);
    },
    clear: () => {
      data.clear();
    },
    key: (index: number) => [...data.keys()][index] ?? null,
    get length() {
      return data.size;
    },
  };
  return { data, restore: replaceLocalStorage({ value: storage }) };
}

/**
 * A `window.localStorage` that throws on *access*, the way Safari private mode
 * and a cookie-blocked browser do. Note it is the property read that throws, not
 * just the method call — code that does `window.localStorage.getItem(...)` blows
 * up before it ever reaches `getItem`.
 */
export function installThrowingStorage(message = 'SecurityError'): () => void {
  return replaceLocalStorage({
    get() {
      throw new Error(message);
    },
  });
}
