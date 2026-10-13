import { describe, it, expect } from 'vitest';
import { passwordPolicyError, MAX_PASSWORD_BYTES } from './passwordPolicy';

// SCOPE: the client-side copy of the server's password policy. Each rule is
// checked on its own, so a password that breaks only one rule names that rule.
describe('passwordPolicyError', () => {
  it('accepts a password that meets every rule', () => {
    expect(passwordPolicyError('Secure#Pass1')).toBeNull();
  });

  it.each([
    ['Ab1!', 'at least 8 characters'],
    ['lowercase1!', 'uppercase'],
    ['UPPERCASE1!', 'lowercase'],
    ['NoDigits!!', 'digit'],
    ['NoSpecial12', 'special'],
  ])('rejects %s for its missing rule', (password, rule) => {
    expect(passwordPolicyError(password)).toContain(rule);
  });

  // Where the server's Unicode rules differ from a naive ASCII check.
  it.each([
    ['Übermut2024!', 'a non-ASCII capital counts as upper case'],
    ['Abcdé1!', 'the length is counted in bytes, so 7 characters can be 8 bytes'],
    ['Abcdefg٣!', 'a non-ASCII digit counts as a digit'],
  ])('accepts %s (%s)', (password) => {
    expect(passwordPolicyError(password)).toBeNull();
  });

  it.each([
    ['Correct horse battery 9', 'a space is not a special character'],
    ['Abcdefg1é', 'a letter with an accent is not a special character'],
  ])('rejects %s (%s)', (password) => {
    expect(passwordPolicyError(password)).toContain('special');
  });

  // bcrypt hashes at most 72 bytes and the server refuses more, with this text.
  describe('72-byte maximum', () => {
    const prefix = 'Aa1!'; // meets every class rule in 4 bytes

    it('accepts exactly 72 bytes', () => {
      expect(passwordPolicyError(prefix + 'x'.repeat(68))).toBeNull();
    });

    it('rejects 73 bytes with the server message', () => {
      expect(passwordPolicyError(prefix + 'x'.repeat(69))).toBe('Password must be at most 72 bytes');
      expect(MAX_PASSWORD_BYTES).toBe(72);
    });

    it('counts multi-byte characters as bytes', () => {
      // 4 + 34×2 = 72 bytes in 38 characters: allowed.
      expect(passwordPolicyError(prefix + 'ä'.repeat(34))).toBeNull();
      // 4 + 35×2 = 74 bytes in only 39 characters: refused.
      expect(passwordPolicyError(prefix + 'ä'.repeat(35))).toContain('at most 72 bytes');
      // 4 + 23×3 = 73 bytes in 27 characters.
      expect(passwordPolicyError(prefix + '€'.repeat(23))).toContain('at most 72 bytes');
    });
  });
});
