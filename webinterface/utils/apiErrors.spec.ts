import { describe, expect, it } from 'vitest';
import { RETRY_HINT, clientErrorMessage, httpErrorStatus } from './apiErrors';

describe('httpErrorStatus', () => {
  it('reads statusCode, then status', () => {
    expect(httpErrorStatus({ statusCode: 409 })).toBe(409);
    expect(httpErrorStatus({ status: 404 })).toBe(404);
    expect(httpErrorStatus(null)).toBeUndefined();
  });
});

describe('clientErrorMessage', () => {
  it("is the server's sentence for a 4xx", () => {
    expect(clientErrorMessage({ statusCode: 409, data: { message: 'Repeating tasks cannot be completed' } }, RETRY_HINT)).toBe(
      'Repeating tasks cannot be completed',
    );
    expect(clientErrorMessage({ status: 400, data: { error: 'bad' } }, RETRY_HINT)).toBe('bad');
  });

  it('is the fallback for a 5xx, a blank message or no status', () => {
    expect(clientErrorMessage({ statusCode: 500, data: { message: 'Internal server error' } }, RETRY_HINT)).toBe(RETRY_HINT);
    expect(clientErrorMessage({ statusCode: 400, data: { message: '  ' } }, RETRY_HINT)).toBe(RETRY_HINT);
    expect(clientErrorMessage(new Error('offline'), RETRY_HINT)).toBe(RETRY_HINT);
  });
});
