import { describe, expect, it, vi } from 'vitest';
import {
  creationSpecPreviewCompletedEventFixture,
  inputAcceptedEventFixture,
  storyboardAcceptedEventFixture,
  storyboardEventFixture,
  turnEventFixture,
  WORKSPACE_IDS
} from '../../test/workspaceFixtures.js';
import { openWorkspaceEventStream } from './workspaceEventStream.js';

describe('Workspace EventSource adapter', () => {
  it('uses the Business same-origin path and delivers only strictly parsed persistent events', () => {
    const source = new MockEventSource();
    const onEvent = vi.fn();
    const stream = openWorkspaceEventStream({
      projectID: WORKSPACE_IDS.project,
      sessionID: WORKSPACE_IDS.session,
      cursor: 1,
      eventSourceFactory: (url) => {
        source.url = url;
        return source;
      },
      onEvent
    });

    expect(source.url).toBe(`/api/v1/agent/sessions/${WORKSPACE_IDS.session}/events?after_seq=1`);
    source.emit('session.input.accepted', inputAcceptedEventFixture(), '2');
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ seq: 2, eventID: WORKSPACE_IDS.event }));
    stream.close();
    expect(source.close).toHaveBeenCalledTimes(1);
    source.emit('session.input.accepted', inputAcceptedEventFixture(), '2');
    expect(onEvent).toHaveBeenCalledTimes(1);
  });

  it('registers and delivers the persistent Creation Spec Preview event types', () => {
    const source = new MockEventSource();
    const onEvent = vi.fn();
    openWorkspaceEventStream({
      projectID: WORKSPACE_IDS.project,
      sessionID: WORKSPACE_IDS.session,
      cursor: 1,
      eventSourceFactory: () => source,
      onEvent
    });
    source.emit('creation_spec.preview.completed', creationSpecPreviewCompletedEventFixture(), '2');
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({
      event: 'creation_spec.preview.completed',
      payload: expect.objectContaining({ creationSpecID: WORKSPACE_IDS.creationSpec })
    }));
  });

  it.each(['session.turn.completed', 'session.turn.failed', 'session.turn.recovery_pending'])(
    'registers and delivers %s through the same strict parser',
    (eventName) => {
      const source = new MockEventSource();
      const onEvent = vi.fn();
      openWorkspaceEventStream({
        projectID: WORKSPACE_IDS.project,
        sessionID: WORKSPACE_IDS.session,
        cursor: 2,
        eventSourceFactory: () => source,
        onEvent
      });
      source.emit(eventName, turnEventFixture(eventName), '3');
      expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({
        event: eventName,
        payload: expect.objectContaining({ turnID: WORKSPACE_IDS.turn })
      }));
    }
  );

  it.each([
    ['plan_storyboard.preview.accepted', storyboardAcceptedEventFixture, 2],
    ['plan_storyboard.preview.completed', () => storyboardEventFixture(), 3],
    ['plan_storyboard.preview.failed', () => storyboardEventFixture('plan_storyboard.preview.failed'), 3],
    ['plan_storyboard.preview.runtime_failed', () => storyboardEventFixture('plan_storyboard.preview.runtime_failed'), 3]
  ])('registers and strictly delivers %s', (eventName, fixture, seq) => {
    const source = new MockEventSource();
    const onEvent = vi.fn();
    openWorkspaceEventStream({
      projectID: WORKSPACE_IDS.project,
      sessionID: WORKSPACE_IDS.session,
      cursor: seq - 1,
      eventSourceFactory: () => source,
      onEvent
    });
    source.emit(eventName, fixture(), String(seq));
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ event: eventName, seq }));
  });

  it('closes on protocol failure, reset, or transport failure instead of auto-reconnecting', () => {
    const protocolSource = new MockEventSource();
    const onProtocolError = vi.fn();
    openWorkspaceEventStream({
      projectID: WORKSPACE_IDS.project,
      sessionID: WORKSPACE_IDS.session,
      cursor: 1,
      eventSourceFactory: () => protocolSource,
      onProtocolError
    });
    protocolSource.emit('session.input.accepted', inputAcceptedEventFixture(), '3');
    expect(onProtocolError).toHaveBeenCalledTimes(1);
    expect(protocolSource.close).toHaveBeenCalledTimes(1);

    const resetSource = new MockEventSource();
    const onReset = vi.fn();
    openWorkspaceEventStream({
      projectID: WORKSPACE_IDS.project,
      sessionID: WORKSPACE_IDS.session,
      cursor: 1,
      eventSourceFactory: () => resetSource,
      onReset
    });
    resetSource.emit('stream.reset', {
      schema_version: 'workspace.stream-control.v1',
      event: 'stream.reset',
      session_id: WORKSPACE_IDS.session,
      reason: 'cursor_expired',
      snapshot_required: true,
      min_available_seq: 3,
      latest_seq: 5
    }, '');
    expect(onReset).toHaveBeenCalledWith(expect.objectContaining({ reason: 'cursor_expired' }));
    expect(resetSource.close).toHaveBeenCalledTimes(1);
    resetSource.emit('stream.reset', {
      schema_version: 'workspace.stream-control.v1',
      event: 'stream.reset',
      session_id: WORKSPACE_IDS.session,
      reason: 'cursor_expired',
      snapshot_required: true,
      min_available_seq: 3,
      latest_seq: 5
    }, '');
    expect(onReset).toHaveBeenCalledTimes(1);

    const failedSource = new MockEventSource();
    const onTransportError = vi.fn();
    openWorkspaceEventStream({
      projectID: WORKSPACE_IDS.project,
      sessionID: WORKSPACE_IDS.session,
      cursor: 1,
      eventSourceFactory: () => failedSource,
      onTransportError
    });
    failedSource.onerror();
    expect(onTransportError).toHaveBeenCalledTimes(1);
    expect(failedSource.close).toHaveBeenCalledTimes(1);
  });
});

class MockEventSource {
  constructor() {
    this.listeners = {};
    this.close = vi.fn();
    this.onerror = null;
  }

  addEventListener(eventName, listener) {
    this.listeners[eventName] = listener;
  }

  removeEventListener(eventName) {
    delete this.listeners[eventName];
  }

  emit(eventName, data, lastEventId) {
    this.listeners[eventName]?.({ type: eventName, data: JSON.stringify(data), lastEventId });
  }
}
