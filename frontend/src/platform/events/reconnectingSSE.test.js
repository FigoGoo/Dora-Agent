import { describe, expect, it, vi } from 'vitest';
import { connectReconnectingSSE, parseSSEEvent } from './reconnectingSSE.js';

describe('reconnecting SSE', () => {
  it('reconnects with the last confirmed cursor and bounded backoff', () => {
    const sources = [];
    const scheduled = [];
    const received = [];
    const transportErrors = [];
    const stream = connectReconnectingSSE({
      url: '/api/events?scope=s1',
      eventNames: ['a2ui.action'],
      initialCursor: 3,
      eventSourceFactory: (url) => {
        const source = new MockEventSource(url);
        sources.push(source);
        return source;
      },
      schedule: (callback, delay) => {
        scheduled.push({ callback, delay });
        return scheduled.length;
      },
      cancelSchedule: vi.fn(),
      random: () => 0.5,
      onEvent: (event) => received.push(event),
      onTransportError: (state) => transportErrors.push(state)
    });

    expect(sources[0].url).toBe('/api/events?scope=s1&after_seq=3');
    sources[0].emit('a2ui.action', { seq: 8, payload: { ok: true } });
    expect(received[0]).toMatchObject({ event: 'a2ui.action', seq: 8 });

    sources[0].fail();
    expect(sources[0].close).toHaveBeenCalledTimes(1);
    expect(scheduled[0].delay).toBe(250);
    expect(transportErrors[0]).toMatchObject({ attempt: 1, delay: 250, cursor: 8 });

    scheduled[0].callback();
    expect(sources[1].url).toBe('/api/events?scope=s1&after_seq=8');
    sources[1].fail();
    expect(scheduled[1].delay).toBe(500);

    stream.close();
    expect(sources[1].close).toHaveBeenCalledTimes(1);
    expect(stream.getCursor()).toBe(8);
  });

  it('ignores lifecycle callbacks and events from a disposed source', () => {
    const sources = [];
    const scheduled = [];
    const onEvent = vi.fn();
    const onOpen = vi.fn();
    const stream = connectReconnectingSSE({
      url: '/api/events',
      eventNames: ['a2ui.ready'],
      eventSourceFactory: (url) => {
        const source = new MockEventSource(url);
        sources.push(source);
        return source;
      },
      schedule: (callback) => {
        scheduled.push(callback);
        return scheduled.length;
      },
      cancelSchedule: vi.fn(),
      random: () => 0.5,
      onEvent,
      onOpen
    });
    const staleOpen = sources[0].onopen;
    const staleEvent = sources[0].listeners['a2ui.ready'];

    sources[0].fail();
    scheduled[0]();
    staleOpen();
    staleEvent({ type: 'a2ui.ready', data: JSON.stringify({ seq: 1 }) });

    expect(onOpen).not.toHaveBeenCalled();
    expect(onEvent).not.toHaveBeenCalled();
    stream.close();
  });

  it('converts invalid JSON to a safe error-like payload', () => {
    expect(parseSSEEvent({ type: 'a2ui.error', data: 'broken payload' })).toEqual({
      event: 'a2ui.error',
      payload: { message: 'broken payload' }
    });
  });

  it('stops the old cursor stream and notifies the caller when stream.reset arrives', () => {
    const sources = [];
    const scheduled = [];
    const onEvent = vi.fn();
    const onReset = vi.fn();
    const stream = connectReconnectingSSE({
      url: '/api/events?scope=s1',
      eventNames: ['chat.message'],
      initialCursor: 9,
      eventSourceFactory: (url) => {
        const source = new MockEventSource(url);
        sources.push(source);
        return source;
      },
      schedule: (callback) => {
        scheduled.push(callback);
        return scheduled.length;
      },
      cancelSchedule: vi.fn(),
      onEvent,
      onReset
    });

    sources[0].emit('stream.reset', {
      event: 'stream.reset',
      reason: 'cursor_expired',
      snapshot_required: true,
      min_available_seq: 21,
      latest_seq: 42
    });

    expect(onReset).toHaveBeenCalledWith(
      expect.objectContaining({
        event: 'stream.reset',
        reason: 'cursor_expired',
        snapshot_required: true,
        min_available_seq: 21,
        latest_seq: 42
      }),
      { cursor: 9 }
    );
    expect(onEvent).not.toHaveBeenCalled();
    expect(sources[0].close).toHaveBeenCalledTimes(1);
    sources[0].fail();
    expect(scheduled).toHaveLength(0);
    expect(stream.getCursor()).toBe(9);
    stream.close();
  });
});

class MockEventSource {
  constructor(url) {
    this.url = url;
    this.listeners = {};
    this.close = vi.fn();
    this.onopen = null;
    this.onerror = null;
  }

  addEventListener(eventName, listener) {
    this.listeners[eventName] = listener;
  }

  removeEventListener(eventName) {
    delete this.listeners[eventName];
  }

  emit(eventName, payload) {
    this.listeners[eventName]?.({ type: eventName, data: JSON.stringify(payload) });
  }

  fail() {
    this.onerror?.();
  }
}
