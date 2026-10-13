/**
 * The account password policy, mirroring `user.ValidatePassword` on the server:
 * at least 8 characters with an upper-case letter, a lower-case letter, a digit
 * and a special character. Returns the first rule the password breaks, in the
 * server's order and wording, or `null` when it passes.
 */
export function passwordPolicyError(password: string): string | null {
  if (password.length < 8) return 'Password must be at least 8 characters';
  if (!/[A-Z]/.test(password)) return 'Password must contain at least one uppercase letter';
  if (!/[a-z]/.test(password)) return 'Password must contain at least one lowercase letter';
  if (!/\d/.test(password)) return 'Password must contain at least one digit';
  if (!/[^a-zA-Z0-9]/.test(password)) return 'Password must contain at least one special character';
  return null;
}
