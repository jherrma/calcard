/**
 * The longest password the server accepts, in UTF-8 bytes: bcrypt hashes at
 * most 72 bytes and the server refuses anything longer (`user.MaxPasswordBytes`).
 */
export const MAX_PASSWORD_BYTES = 72;

/**
 * The account password policy, mirroring `user.ValidatePassword` on the server:
 * 8 to 72 bytes with an upper-case letter, a lower-case letter, a digit and
 * a special character. The server counts bytes and uses Go's Unicode classes
 * (`unicode.IsUpper` = Lu, `IsLower` = Ll, `IsDigit` = Nd, special = punctuation
 * or symbol), so `Übermut2024!` passes and a space is NOT a special character.
 * Both length limits count UTF-8 bytes, not characters: `ä` is two bytes, so
 * 37 of them already break the 72-byte maximum. Returns the first rule the
 * password breaks, in the server's order and with its wording, or `null` when
 * it passes.
 *
 * Use it for every field that sets a NEW account password, never for a login
 * or current-password field: those must accept whatever the account has.
 */
export function passwordPolicyError(password: string): string | null {
  const bytes = new TextEncoder().encode(password).length;
  if (bytes < 8) return 'Password must be at least 8 characters';
  if (bytes > MAX_PASSWORD_BYTES) return `Password must be at most ${MAX_PASSWORD_BYTES} bytes`;
  if (!/\p{Lu}/u.test(password)) return 'Password must contain at least one uppercase letter';
  if (!/\p{Ll}/u.test(password)) return 'Password must contain at least one lowercase letter';
  if (!/\p{Nd}/u.test(password)) return 'Password must contain at least one digit';
  if (!/[\p{P}\p{S}]/u.test(password)) return 'Password must contain at least one special character';
  return null;
}
