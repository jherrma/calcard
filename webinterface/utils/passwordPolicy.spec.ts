import { describe, it, expect } from 'vitest';
import { passwordPolicyError } from './passwordPolicy';

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

  it('counts a # as the special character', () => {
    expect(passwordPolicyError('Abcdefg1#')).toBeNull();
  });
});
