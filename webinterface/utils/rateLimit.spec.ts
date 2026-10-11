import { describe, it, expect } from 'vitest';
import {
  isIdempotentMethod,
  isRateLimitError,
  parseRetryAfter,
  rateLimitMessage,
  retryAfterFromError,
} from './rateLimit';

const NOW = Date.UTC(2026, 9, 11, 12, 0, 0);

describe('parseRetryAfter', () => {
  it('reads delay-seconds', () => {
    expect(parseRetryAfter('0', NOW)).toBe(0);
    expect(parseRetryAfter('7', NOW)).toBe(7);
    expect(parseRetryAfter(' 60 ', NOW)).toBe(60);
  });

  it('reads an HTTP-date as seconds from now, rounded up', () => {
    const fourSecondsAhead = new Date(NOW + 4_200).toUTCString(); // toUTCString drops the ms
    expect(parseRetryAfter(fourSecondsAhead, NOW)).toBe(4);
    expect(parseRetryAfter(fourSecondsAhead, NOW - 500)).toBe(5); // 4.5s → rounds up
    expect(parseRetryAfter(new Date(NOW + 90_000).toUTCString(), NOW)).toBe(90);
  });

  it('clamps an HTTP-date in the past to 0', () => {
    expect(parseRetryAfter(new Date(NOW - 60_000).toUTCString(), NOW)).toBe(0);
  });

  it('returns null for a missing or unparseable value, never 0', () => {
    expect(parseRetryAfter(null, NOW)).toBeNull();
    expect(parseRetryAfter(undefined, NOW)).toBeNull();
    expect(parseRetryAfter('', NOW)).toBeNull();
    expect(parseRetryAfter('soon', NOW)).toBeNull();
    expect(parseRetryAfter('-3', NOW)).toBeNull();
    expect(parseRetryAfter('1.5', NOW)).toBeNull();
  });
});

describe('rateLimitMessage', () => {
  it('names the wait in seconds', () => {
    expect(rateLimitMessage(60)).toBe('Too many requests, please try again in 60 seconds.');
    expect(rateLimitMessage(1)).toBe('Too many requests, please try again in 1 second.');
  });

  it('stays friendly when the server gave no usable wait', () => {
    expect(rateLimitMessage(null)).toBe('Too many requests, please try again in a moment.');
    expect(rateLimitMessage(0)).toBe('Too many requests, please try again in a moment.');
  });
});

describe('isIdempotentMethod', () => {
  it('only GET and HEAD (case-insensitive); a missing method is a GET', () => {
    expect(isIdempotentMethod(undefined)).toBe(true);
    expect(isIdempotentMethod('get')).toBe(true);
    expect(isIdempotentMethod('HEAD')).toBe(true);
    for (const m of ['POST', 'PUT', 'PATCH', 'DELETE', 'post']) {
      expect(isIdempotentMethod(m)).toBe(false);
    }
  });
});

describe('isRateLimitError / retryAfterFromError', () => {
  const err = (headers: Record<string, string>, data?: unknown) => ({
    status: 429,
    data,
    response: { headers: new Headers(headers) },
  });

  it('recognises a 429 by status or statusCode, nothing else', () => {
    expect(isRateLimitError({ status: 429 })).toBe(true);
    expect(isRateLimitError({ statusCode: 429 })).toBe(true);
    expect(isRateLimitError({ status: 500 })).toBe(false);
    expect(isRateLimitError(new Error('x'))).toBe(false);
    expect(isRateLimitError(null)).toBe(false);
  });

  it('prefers the Retry-After header', () => {
    expect(retryAfterFromError(err({ 'Retry-After': '12' }, { retry_after: 60 }), NOW)).toBe(12);
  });

  it("falls back to the body's retry_after when the header is hidden (cross-origin)", () => {
    expect(retryAfterFromError(err({}, { retry_after: 59.4 }), NOW)).toBe(60);
  });

  it('returns null when neither says anything', () => {
    expect(retryAfterFromError(err({}, { message: 'slow down' }), NOW)).toBeNull();
    expect(retryAfterFromError({ status: 429 }, NOW)).toBeNull();
  });
});
