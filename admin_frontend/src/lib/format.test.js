import { describe, expect, test } from 'vitest';
import { toApiDateTime } from './format.js';

describe('format helpers', () => {
  test('converts datetime-local values to API timestamps', () => {
    expect(toApiDateTime('2026-07-06T08:30')).toBe(new Date('2026-07-06T08:30').toISOString());
    expect(toApiDateTime('')).toBe('');
  });
});
