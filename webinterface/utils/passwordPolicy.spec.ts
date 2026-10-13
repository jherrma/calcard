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
});
