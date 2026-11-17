import { describe, expect, it } from 'vitest';
import { VisibleClock } from '../src/lib/dwell';
describe('visible detail dwell', () => {
  it('excludes hidden intervals and does not create subsecond records', () => {
    const clock = new VisibleClock(); clock.resume(100, 0);
    expect(clock.pause(900)).toBeUndefined();
    expect(clock.take(9000)).toBeUndefined();
    clock.resume(9000, 9000);
    expect(clock.pause(11000)).toEqual({ seconds: 2, occurredAt: '1970-01-01T00:00:09.000Z' });
  });
  it('retains subsecond remainder between periodic visible samples without double counting', () => {
    const clock = new VisibleClock(); clock.resume(0, 0);
    expect(clock.take(15500)).toEqual({ seconds: 15, occurredAt: '1970-01-01T00:00:00.000Z' });
    expect(clock.take(16200)).toEqual({ seconds: 1, occurredAt: '1970-01-01T00:00:15.000Z' });
    expect(clock.pause(16900)).toBeUndefined();
  });
});
