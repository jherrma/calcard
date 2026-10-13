// @vitest-environment nuxt
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import PasswordStrength from './PasswordStrength.vue';
import { passwordPolicyError } from '~/utils/passwordPolicy';

// SCOPE: the meter scores with the same Unicode classes and byte lengths as the
// password policy (utils/passwordPolicy.ts), so it never calls a password
// "Strong" that the server refuses, nor marks down one the server accepts. Its
// ASCII regexes used to rate a space-only passphrase Strong and 'Übermut2024!'
// only Good.

function label(password: string): string {
  return mount(PasswordStrength, { props: { password } }).find('span').text();
}

describe('AuthPasswordStrength', () => {
  it('shows the prompt for an empty password', () => {
    expect(label('')).toBe('Enter password');
  });

  it.each([
    ['Secure#Pass1'],
    ['Übermut2024!'], // non-ASCII capital
    ['Abcdefg٣!'], // non-ASCII digit
    ['Aa1!' + 'x'.repeat(68)], // exactly 72 bytes
  ])('rates %s Strong, as the policy accepts it', (password) => {
    expect(passwordPolicyError(password)).toBeNull();
    expect(label(password)).toBe('Strong');
  });

  it.each([
    ['Correct horse battery 9', 'a space is not a special character'],
    ['Abcdefg1é', 'an accented letter is not a special character'],
    ['Aa1!' + 'x'.repeat(69), 'over 72 bytes'],
    ['Aa1!' + 'ä'.repeat(35), '39 characters but 74 bytes'],
  ])('does not rate %s Strong (%s)', (password) => {
    expect(passwordPolicyError(password)).not.toBeNull();
    expect(label(password)).toBe('Good');
  });

  it('counts the minimum in bytes like the server', () => {
    // 7 characters, 8 bytes: the length point is earned.
    expect(label('Abcdé1!')).toBe('Strong');
  });

  it('keeps the rest of the scale', () => {
    expect(label('abc1')).toBe('Weak'); // digit only
    expect(label('Abc1')).toBe('Fair'); // digit + both cases
  });
});
