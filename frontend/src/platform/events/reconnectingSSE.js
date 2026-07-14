// parseSSEEvent 将 EventSource MessageEvent 转换为稳定事件对象，并保留命名事件类型。
export function parseSSEEvent(event) {
  try {
    const parsed = JSON.parse(event.data);
    if (parsed && typeof parsed === 'object' && !parsed.event) {
      return { ...parsed, event: event.type };
    }
    return parsed;
  } catch {
    return { event: event.type, payload: { message: event.data } };
  }
}

// connectReconnectingSSE 建立带游标、有界退避和显式销毁语义的 SSE 连接。
export function connectReconnectingSSE({
  url,
  eventNames,
  initialCursor = 0,
  onEvent,
  onOpen,
  onReset,
  onTransportError,
  eventSourceFactory = defaultEventSourceFactory,
  schedule = (callback, delay) => window.setTimeout(callback, delay),
  cancelSchedule = (timer) => window.clearTimeout(timer),
  random = Math.random,
  baseRetryDelay = 250,
  maxRetryDelay = 3000,
  retryJitter = 0.2
}) {
  let cursor = finiteCursor(initialCursor);
  let attempt = 0;
  let source = null;
  let sourceListeners = [];
  let retryTimer = null;
  let closed = false;
  const resetEventName = 'stream.reset';

  // open 为当前游标建立唯一连接；构造失败与传输失败使用同一重连策略。
  function open() {
    if (closed) {
      return;
    }
    try {
      const current = eventSourceFactory(withCursor(url, cursor));
      source = current;
      const subscribedEvents = [...new Set([...(eventNames || []), resetEventName])];
      const listeners = subscribedEvents.map((eventName) => {
        const listener = (event) => {
          if (closed || source !== current) {
            return;
          }
          const parsed = parseSSEEvent(event);
          if (eventName === resetEventName) {
            stopForReset(current, listeners, parsed);
            return;
          }
          cursor = Math.max(cursor, eventCursor(parsed, event));
          onEvent?.(parsed, { cursor });
        };
        current.addEventListener(eventName, listener);
        return [eventName, listener];
      });
      sourceListeners = listeners;

      current.onopen = () => {
        if (closed || source !== current) {
          return;
        }
        attempt = 0;
        onOpen?.({ cursor });
      };
      current.onerror = () => {
        if (closed || source !== current) {
          return;
        }
        disposeSource(current, listeners);
        source = null;
        sourceListeners = [];
        scheduleReconnect();
      };
    } catch (error) {
      scheduleReconnect(error);
    }
  }

  // stopForReset 永久关闭旧游标连接，交由调用方回源完整快照后建立新流。
  function stopForReset(current, listeners, event) {
    closed = true;
    if (retryTimer != null) {
      cancelSchedule(retryTimer);
      retryTimer = null;
    }
    disposeSource(current, listeners);
    source = null;
    sourceListeners = [];
    onReset?.(event, { cursor });
  }

  // scheduleReconnect 使用带抖动的有界指数退避，避免大量页面同时断线后形成重连洪峰。
  function scheduleReconnect(cause) {
    if (closed || retryTimer != null) {
      return;
    }
    attempt += 1;
    const exponentialDelay = Math.min(maxRetryDelay, baseRetryDelay * 2 ** (attempt - 1));
    const jitterFactor = 1 + retryJitter * (2 * random() - 1);
    const delay = Math.max(0, Math.min(maxRetryDelay, Math.round(exponentialDelay * jitterFactor)));
    onTransportError?.({ attempt, delay, cursor, cause });
    retryTimer = schedule(() => {
      retryTimer = null;
      open();
    }, delay);
  }

  // close 永久停止重连并移除全部监听器，避免页面切换后旧会话继续写状态。
  function close() {
    closed = true;
    if (retryTimer != null) {
      cancelSchedule(retryTimer);
      retryTimer = null;
    }
    if (source) {
      disposeSource(source, sourceListeners);
      source = null;
      sourceListeners = [];
    }
  }

  open();

  return {
    close,
    getCursor: () => cursor
  };
}

// disposeSource 清理命名事件与生命周期回调，并关闭底层连接。
function disposeSource(current, listeners = []) {
  listeners.forEach(([eventName, listener]) => current.removeEventListener(eventName, listener));
  current.onopen = null;
  current.onerror = null;
  current.close();
}

// defaultEventSourceFactory 使用浏览器原生 EventSource；认证依赖同源 HttpOnly Cookie。
function defaultEventSourceFactory(url) {
  return new window.EventSource(url, { withCredentials: true });
}

// withCursor 在重连 URL 中携带最后确认序号，避免依赖易丢失的内存通知。
function withCursor(url, cursor) {
  if (cursor <= 0) {
    return url;
  }
  return `${url}${url.includes('?') ? '&' : '?'}after_seq=${encodeURIComponent(cursor)}`;
}

// eventCursor 优先读取协议 seq，兼容标准 SSE Last-Event-ID。
function eventCursor(parsed, event) {
  return finiteCursor(parsed?.seq || event?.lastEventId);
}

// finiteCursor 只接受非负整数游标，非法值按零处理。
function finiteCursor(value) {
  const number = Number(value);
  return Number.isSafeInteger(number) && number > 0 ? number : 0;
}
