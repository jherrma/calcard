import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  DAV_PASSWORD_ALPHABET,
  DAV_PASSWORD_LENGTH,
  DAV_USERNAME_LENGTH,
  davUsernameError,
  generateDavPassword,
  generateDavUsername,
  randomFromAlphabet,
  type RandomBytes,
} from './davCredentials';

// SCOPE: the generated CalDAV/CardDAV username and password, and the username
// rule the server enforces on create (ValidateDAVCredentialUsername).

/** A RandomBytes that hands out `bytes` in order, then repeats `tail`. */
function scripted(bytes: number[], tail = 0): RandomBytes & { calls: number } {
  let i = 0;
  const fn = ((buf: Uint8Array) => {
    fn.calls++;
    for (let j = 0; j < buf.length; j++) buf[j] = i < bytes.length ? bytes[i++]! : tail;
    return buf;
  }) as RandomBytes & { calls: number };
  fn.calls = 0;
  return fn;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('randomFromAlphabet', () => {
  it('maps accepted bytes by index and discards the biased top of the byte range', () => {
    // 26 letters: 256 % 26 = 22, so bytes 234..255 must be thrown away.
    const rng = scripted([255, 234, 0, 233, 25, 26]);
    // REVERT PROOF: a plain `byte % 26` turns 255 into 'v' and 234 into 'a'.
    expect(randomFromAlphabet('abcdefghijklmnopqrstuvwxyz', 4, rng)).toBe('azza');
    // Two bytes were rejected in the first round, so a second round asked for
    // exactly the two characters still missing.
    expect(rng.calls).toBe(2);
  });

  it('is uniform: every byte value, in turn, yields each letter equally often', () => {
    // A source that walks 0..255 over and over. Each full walk has 234
    // accepted bytes, 9 per letter, so 468 characters are 18 of each.
    let next = 0;
    const cycling: RandomBytes = (buf) => {
      for (let j = 0; j < buf.length; j++) buf[j] = next++ % 256;
      return buf;
    };
    const out = randomFromAlphabet('abcdefghijklmnopqrstuvwxyz', 468, cycling);
    const counts = new Map<string, number>();
    for (const c of out) counts.set(c, (counts.get(c) ?? 0) + 1);
    expect(counts.size).toBe(26);
    // REVERT PROOF: with a plain `byte % 26` the same bytes give a–d 19
    // times, e–v 18 and w–z 17.
    expect(new Set(counts.values())).toEqual(new Set([18]));
  });

  it('uses crypto.getRandomValues by default', () => {
    const spy = vi.spyOn(crypto, 'getRandomValues');
    randomFromAlphabet('ab', 10);
    expect(spy).toHaveBeenCalled();
  });

  it('refuses an alphabet a byte cannot index', () => {
    expect(() => randomFromAlphabet('', 4)).toThrow(RangeError);
    expect(() => randomFromAlphabet('x'.repeat(257), 4)).toThrow(RangeError);
  });
});

describe('generateDavUsername', () => {
  it('is 16 lowercase letters a–z, which the server rule accepts', () => {
    for (let i = 0; i < 50; i++) {
      const name = generateDavUsername();
      expect(name).toMatch(/^[a-z]{16}$/);
      expect(name).toHaveLength(DAV_USERNAME_LENGTH);
      expect(davUsernameError(name)).toBeNull();
    }
  });

  it('differs between calls', () => {
    const names = new Set(Array.from({ length: 20 }, () => generateDavUsername()));
    expect(names.size).toBe(20);
  });
});

describe('generateDavPassword', () => {
  it('is 24 characters from the unambiguous alphabet with every character class', () => {
    for (let i = 0; i < 50; i++) {
      const pw = generateDavPassword();
      expect(pw).toHaveLength(DAV_PASSWORD_LENGTH);
      for (const c of pw) expect(DAV_PASSWORD_ALPHABET).toContain(c);
      expect(pw).toMatch(/[A-Z]/);
      expect(pw).toMatch(/[a-z]/);
      expect(pw).toMatch(/[0-9]/);
      expect(pw).toMatch(/[-_.]/);
      // The server's rule for DAV credentials: at least 8 characters.
      expect(pw.length).toBeGreaterThanOrEqual(8);
    }
  });

  it('never contains look-alike characters', () => {
    expect(DAV_PASSWORD_ALPHABET).not.toMatch(/[0O1lIo]/);
    expect(new Set(DAV_PASSWORD_ALPHABET).size).toBe(DAV_PASSWORD_ALPHABET.length);
  });

  it('discards a whole draw that misses a class instead of patching it', () => {
    // Alphabet: 24 upper, 24 lower, 8 digits, 3 symbols. First draw: 24 × 'A'
    // (no lower case, digit or symbol) — rejected. Second draw: 'A','a','2','-'
    // then 'A's — accepted as drawn.
    const second = [0, 24, 48, 56];
    const rng = scripted([...Array(24).fill(0), ...second], 0);
    expect(generateDavPassword(rng)).toBe(`Aa2-${'A'.repeat(20)}`);
  });

  it('gives up on a random source that never yields every class', () => {
    expect(() => generateDavPassword(scripted([], 0))).toThrow();
  });
});

describe('davUsernameError', () => {
  it('accepts 3 to 50 lowercase letters', () => {
    expect(davUsernameError('abc')).toBeNull();
    expect(davUsernameError('z'.repeat(50))).toBeNull();
  });

  it('reports upper case instead of lower-casing it', () => {
    expect(davUsernameError('Thunderbird')).toBe('Only lowercase letters a–z are allowed');
    expect(davUsernameError('PHONE')).toBe('Only lowercase letters a–z are allowed');
  });

  it('refuses digits, symbols, spaces and non-ASCII letters', () => {
    for (const name of ['phone2', 'my-phone', 'my_phone', 'my phone', 'jürgen', ' abc', 'abc.']) {
      expect(davUsernameError(name), name).toBe('Only lowercase letters a–z are allowed');
    }
  });

  it('enforces the server length bounds', () => {
    expect(davUsernameError('')).toBe('Username is required');
    expect(davUsernameError('ab')).toBe('Username must be 3–50 letters long');
    expect(davUsernameError('a'.repeat(51))).toBe('Username must be 3–50 letters long');
  });
});
