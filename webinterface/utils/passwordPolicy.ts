/**
 * The account password policy, mirroring `user.ValidatePassword` on the server:
 * at least 8 bytes with an upper-case letter, a lower-case letter, a digit and
 * a special character. The server counts bytes and uses Go's Unicode classes
 * (`unicode.IsUpper` = Lu, `IsLower` = Ll, `IsDigit` = Nd, special = punctuation
 * or symbol), so `Übermut2024!` passes and a space is NOT a special character.
 * Returns the first rule the password breaks, in the server's order, or `null`
 * when it passes.
 *
 * The server also has a limit this cannot mirror: bcrypt refuses more than 72
 * bytes, and the server answers that with a plain 500.
 */
export function passwordPolicyError(password: string): string | null {
  if (new TextEncoder().encode(password).length < 8) return 'Password must be at least 8 characters';
  if (!/\p{Lu}/u.test(password)) return 'Password must contain at least one uppercase letter';
  if (!/\p{Ll}/u.test(password)) return 'Password must contain at least one lowercase letter';
  if (!/\p{Nd}/u.test(password)) return 'Password must contain at least one digit';
  if (!/[\p{P}\p{S}]/u.test(password)) return 'Password must contain at least one special character';
  return null;
}
