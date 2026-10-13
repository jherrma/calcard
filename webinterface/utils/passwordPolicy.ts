/** The shortest password the server accepts, in UTF-8 bytes. */
export const MIN_PASSWORD_BYTES = 8;

/**
 * The longest password the server accepts, in UTF-8 bytes: bcrypt hashes at
 * most 72 bytes and the server refuses anything longer (`user.MaxPasswordBytes`).
 */
export const MAX_PASSWORD_BYTES = 72;

/** Which of the policy's rules a password meets, one flag per rule. */
export interface PasswordPolicyChecks {
  minLength: boolean;
  maxLength: boolean;
  upper: boolean;
  lower: boolean;
  digit: boolean;
  special: boolean;
}

/**
 * Evaluates every rule of the account password policy, mirroring
 * `user.ValidatePassword` on the server. The server counts bytes and uses Go's
 * Unicode classes (`unicode.IsUpper` = Lu, `IsLower` = Ll, `IsDigit` = Nd,
 * special = punctuation or symbol), so `Übermut2024!` passes and a space is NOT
 * a special character. Both length limits count UTF-8 bytes, not characters:
 * `ä` is two bytes, so 37 of them already break the 72-byte maximum.
 *
 * `passwordPolicyError` and `<AuthPasswordStrength>` both read these flags, so
 * the strength meter cannot call a password "Strong" that the policy refuses.
 */
export function passwordPolicyChecks(password: string): PasswordPolicyChecks {
  const bytes = new TextEncoder().encode(password).length;
  return {
    minLength: bytes >= MIN_PASSWORD_BYTES,
    maxLength: bytes <= MAX_PASSWORD_BYTES,
    upper: /\p{Lu}/u.test(password),
    lower: /\p{Ll}/u.test(password),
    digit: /\p{Nd}/u.test(password),
    special: /[\p{P}\p{S}]/u.test(password),
  };
}

/**
 * The account password policy: 8 to 72 bytes with an upper-case letter, a
 * lower-case letter, a digit and a special character (see
 * `passwordPolicyChecks`). Returns the first rule the password breaks, in the
 * server's order and with its wording, or `null` when it passes.
 *
 * Use it for every field that sets a NEW account password, never for a login
 * or current-password field: those must accept whatever the account has.
 */
export function passwordPolicyError(password: string): string | null {
  const checks = passwordPolicyChecks(password);
  if (!checks.minLength) return `Password must be at least ${MIN_PASSWORD_BYTES} characters`;
  if (!checks.maxLength) return `Password must be at most ${MAX_PASSWORD_BYTES} bytes`;
  if (!checks.upper) return 'Password must contain at least one uppercase letter';
  if (!checks.lower) return 'Password must contain at least one lowercase letter';
  if (!checks.digit) return 'Password must contain at least one digit';
  if (!checks.special) return 'Password must contain at least one special character';
  return null;
}
