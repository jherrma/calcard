/**
 * Generated CalDAV/CardDAV credentials and the username rule the server
 * enforces when one is created
 * (server/internal/domain/user/validation.go, ValidateDAVCredentialUsername).
 *
 * Randomness comes from crypto.getRandomValues only. Bytes are mapped onto an
 * alphabet by REJECTION SAMPLING: a byte at or above the largest multiple of
 * the alphabet size that fits in 256 is thrown away, so every character is
 * equally likely. A plain `byte % n` would favour the first `256 % n`
 * characters (for a–z: 22 of the 26 letters 10/256 each, the rest 9/256).
 */

/** Lowercase letters a–z: the only characters a new credential username may hold. */
export const DAV_USERNAME_ALPHABET = 'abcdefghijklmnopqrstuvwxyz';
/** Length of a generated username (26^16 ≈ 2^75 possibilities). */
export const DAV_USERNAME_LENGTH = 16;
/** Bounds the server accepts for a username someone types themselves. */
export const DAV_USERNAME_MIN_LENGTH = 3;
export const DAV_USERNAME_MAX_LENGTH = 50;

// No look-alikes (0/O/o, 1/l/I), so a password read off the screen can be
// typed into a phone. The symbols are URL-unreserved (RFC 3986): the password
// survives a URL's user-info part or a config file without escaping.
const PASSWORD_CLASSES = [
  'ABCDEFGHJKLMNPQRSTUVWXYZ',
  'abcdefghijkmnpqrstuvwxyz',
  '23456789',
  '-_.',
] as const;
export const DAV_PASSWORD_ALPHABET = PASSWORD_CLASSES.join('');
/** Length of a generated password (≈ 2^140 possibilities). */
export const DAV_PASSWORD_LENGTH = 24;
/** The server's only password rule for DAV credentials. */
export const DAV_PASSWORD_MIN_LENGTH = 8;

/** Fills the array with random bytes and returns it, like crypto.getRandomValues. */
export type RandomBytes = (bytes: Uint8Array) => Uint8Array;

const cryptoRandomBytes: RandomBytes = (bytes) => crypto.getRandomValues(bytes);

/**
 * `length` characters drawn uniformly from `alphabet` (at most 256 single
 * UTF-16 code units). `randomBytes` exists for the specs.
 */
export function randomFromAlphabet(
  alphabet: string,
  length: number,
  randomBytes: RandomBytes = cryptoRandomBytes,
): string {
  const n = alphabet.length;
  if (n < 1 || n > 256) throw new RangeError('alphabet must have 1 to 256 characters');
  const limit = 256 - (256 % n); // the largest multiple of n that fits in a byte
  let out = '';
  while (out.length < length) {
    // Ask for exactly what is still missing: no random byte is left unused.
    for (const byte of randomBytes(new Uint8Array(length - out.length))) {
      if (byte < limit) out += alphabet.charAt(byte % n);
    }
  }
  return out;
}

/** A fresh credential username: 16 lowercase letters a–z. */
export function generateDavUsername(randomBytes: RandomBytes = cryptoRandomBytes): string {
  return randomFromAlphabet(DAV_USERNAME_ALPHABET, DAV_USERNAME_LENGTH, randomBytes);
}

/**
 * A fresh credential password: 24 characters with at least one upper-case
 * letter, lower-case letter, digit and symbol. The DAV-credential rule is only
 * "8 characters or more", but holding every class also satisfies the account
 * password rule (user.ValidatePassword) and the strength meter. A draw that
 * misses a class is discarded whole and drawn again, which keeps every
 * qualifying password equally likely (forcing one character per class into
 * fixed positions would not).
 */
export function generateDavPassword(randomBytes: RandomBytes = cryptoRandomBytes): string {
  // With real randomness a draw misses a class about 30% of the time; 100
  // misses in a row only happen with a broken random source.
  for (let attempt = 0; attempt < 100; attempt++) {
    const password = randomFromAlphabet(DAV_PASSWORD_ALPHABET, DAV_PASSWORD_LENGTH, randomBytes);
    if (PASSWORD_CLASSES.every((cls) => [...password].some((c) => cls.includes(c)))) {
      return password;
    }
  }
  throw new Error('random source keeps producing passwords that miss a character class');
}

/**
 * Why `username` cannot be used for a new credential, or null when it can.
 * Mirrors ValidateDAVCredentialUsername. Upper case is reported rather than
 * silently lower-cased: the username must reach the DAV client exactly as it
 * is stored, and a form that changes what was typed hides that.
 */
export function davUsernameError(username: string): string | null {
  if (username === '') return 'Username is required';
  if (!/^[a-z]+$/.test(username)) return 'Only lowercase letters a–z are allowed';
  if (username.length < DAV_USERNAME_MIN_LENGTH || username.length > DAV_USERNAME_MAX_LENGTH) {
    return `Username must be ${DAV_USERNAME_MIN_LENGTH}–${DAV_USERNAME_MAX_LENGTH} letters long`;
  }
  return null;
}
