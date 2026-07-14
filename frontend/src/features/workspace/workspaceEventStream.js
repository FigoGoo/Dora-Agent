import {
  parsePersistentWorkspaceEvent,
  parseStreamReady,
  parseStreamReset,
  WORKSPACE_PERSISTENT_EVENTS
} from './workspaceContract.js';
import { workspaceEventsPath } from './workspaceApi.js';

// openWorkspaceEventStream 建立单次正式 SSE；传输失败立即关闭，由 Workspace Snapshot Probe 决定是否重连。
export function openWorkspaceEventStream({
  projectID,
  sessionID,
  cursor,
  onEvent,
  onReady,
  onReset,
  onTransportError,
  onProtocolError,
  eventSourceFactory = defaultEventSourceFactory
}) {
  const source = eventSourceFactory(workspaceEventsPath(sessionID, cursor));
  let closed = false;
  const listeners = [];

  const add = (eventName, handler) => {
    const listener = (event) => {
      if (!closed) handler(event);
    };
    source.addEventListener(eventName, listener);
    listeners.push([eventName, listener]);
  };

  WORKSPACE_PERSISTENT_EVENTS.forEach((eventName) => {
    add(eventName, (messageEvent) => {
      try {
        const event = parsePersistentWorkspaceEvent(messageEvent, {
          expectedProjectID: projectID,
          expectedSessionID: sessionID
        });
        onEvent?.(event);
      } catch (error) {
        close();
        onProtocolError?.(error);
      }
    });
  });
  add('stream.ready', (messageEvent) => {
    try {
      const ready = parseStreamReady(messageEvent, sessionID);
      onReady?.(ready);
    } catch (error) {
      close();
      onProtocolError?.(error);
    }
  });
  add('stream.reset', (messageEvent) => {
    try {
      const reset = parseStreamReset(messageEvent, sessionID);
      close();
      onReset?.(reset);
    } catch (error) {
      close();
      onProtocolError?.(error);
    }
  });

  source.onerror = () => {
    if (closed) return;
    close();
    onTransportError?.();
  };

  function close() {
    if (closed) return;
    closed = true;
    listeners.forEach(([eventName, listener]) => source.removeEventListener(eventName, listener));
    source.onerror = null;
    source.close();
  }

  return { close };
}

function defaultEventSourceFactory(url) {
  return new window.EventSource(url, { withCredentials: true });
}
