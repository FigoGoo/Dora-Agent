import { useCallback, useEffect, useRef, useState } from 'react';
import { bootstrapProjectWorkspace } from '../projects/projectQuickCreate.js';
import { loadAgentWorkspaceSnapshot } from './workspaceApi.js';
import { parseProjectBootstrap, parseWorkspaceSnapshot } from './workspaceContract.js';
import { openWorkspaceEventStream } from './workspaceEventStream.js';
import { classifyWorkspaceEvent, createWorkspaceState, workspaceReducer } from './workspaceReducer.js';

export const DEFAULT_PROJECT_RETRY_DELAYS = Object.freeze([0, 250, 500, 1000, 2000, 3000]);
export const DEFAULT_STREAM_RETRY_DELAYS = Object.freeze([250, 500, 1000, 2000, 3000]);
const DEFAULT_SCHEDULE = (callback, delay) => window.setTimeout(callback, delay);
const DEFAULT_CANCEL_SCHEDULE = (timer) => window.clearTimeout(timer);

// useProjectWorkspace 拥有正式 Project Bootstrap→Agent Snapshot→SSE 生命周期和唯一连接代次。
export function useProjectWorkspace({
  projectID,
  bootstrap = bootstrapProjectWorkspace,
  loadSnapshot = loadAgentWorkspaceSnapshot,
  openStream = openWorkspaceEventStream,
  projectRetryDelays = DEFAULT_PROJECT_RETRY_DELAYS,
  streamRetryDelays = DEFAULT_STREAM_RETRY_DELAYS,
  schedule = DEFAULT_SCHEDULE,
  cancelSchedule = DEFAULT_CANCEL_SCHEDULE
}) {
  const stateRef = useRef(createWorkspaceState());
  const [state, setState] = useState(stateRef.current);
  const generationRef = useRef(0);
  const abortRef = useRef(null);
  const streamRef = useRef(null);
  const streamGenerationRef = useRef(0);
  const timerRef = useRef(null);
  const reconnectAttemptRef = useRef(0);
  const cycleModeRef = useRef('loading');
  const [cycleVersion, setCycleVersion] = useState(0);

  // commit 同步维护状态 Ref，让同一批连续 SSE 回调始终看到刚推进的 Cursor。
  const commit = useCallback((action) => {
    const next = workspaceReducer(stateRef.current, action);
    stateRef.current = next;
    setState(next);
    return next;
  }, []);

  const closeActiveResources = useCallback(() => {
    streamGenerationRef.current += 1;
    abortRef.current?.abort();
    abortRef.current = null;
    streamRef.current?.close();
    streamRef.current = null;
    if (timerRef.current != null) {
      cancelSchedule(timerRef.current);
      timerRef.current = null;
    }
  }, [cancelSchedule]);

  // requestCycle 在 Reset/Retry 前同步切断旧连接；下一代 Effect 才能提交新的 Snapshot。
  const requestCycle = useCallback((mode, error = null) => {
    closeActiveResources();
    generationRef.current += 1;
    cycleModeRef.current = mode;
    commit(mode === 'reset' ? { type: 'reset', error } : { type: 'loading', phase: 'project' });
    setCycleVersion((value) => value + 1);
  }, [closeActiveResources, commit]);

  useEffect(() => {
    closeActiveResources();
    const generation = generationRef.current + 1;
    generationRef.current = generation;
    const controller = new AbortController();
    abortRef.current = controller;
    const mode = cycleModeRef.current;
    cycleModeRef.current = 'loading';
    if (mode === 'reset' && stateRef.current.snapshot) {
      commit({ type: 'reset' });
    } else {
      commit({ type: 'loading', phase: 'project' });
    }

    const current = (sessionID = '') => {
      const stateSessionID = stateRef.current.snapshot?.session?.id || '';
      return generation === generationRef.current
        && !controller.signal.aborted
        && (!sessionID || !stateSessionID || stateSessionID === sessionID);
    };

    const setTimer = (callback, delay) => {
      if (!current()) return;
      if (timerRef.current != null) cancelSchedule(timerRef.current);
      timerRef.current = schedule(() => {
        timerRef.current = null;
        if (current()) callback();
      }, delay);
    };

    const fail = (error) => {
      if (!current() || error?.name === 'AbortError') return;
      const status = Number(error?.status) || 0;
      if (status === 401) {
        closeActiveResources();
        commit({ type: 'unauthorized', error: publicWorkspaceError(error) });
        return;
      }
      if (status === 404) {
        closeActiveResources();
        commit({ type: 'not_found', error: publicWorkspaceError(error) });
        return;
      }
      commit({ type: 'offline', error: publicWorkspaceError(error) });
    };

    const beginReset = (error) => {
      if (!current()) return;
      requestCycle('reset', publicWorkspaceError(error));
    };

    const connect = (project, snapshot, cursor) => {
      if (!current(snapshot.session.id)) return;
      streamRef.current?.close();
      streamRef.current = null;
      const streamGeneration = streamGenerationRef.current + 1;
      streamGenerationRef.current = streamGeneration;
      const activeStream = () => current(snapshot.session.id)
        && streamGeneration === streamGenerationRef.current;
      commit({ type: 'stream_connecting' });
      try {
        const openedStream = openStream({
          projectID: project.projectID,
          sessionID: snapshot.session.id,
          cursor,
          onEvent: (event) => {
            if (!activeStream()) return;
            const disposition = classifyWorkspaceEvent(stateRef.current, event);
            if (disposition === 'apply') {
              commit({ type: 'event_applied', event });
            } else if (disposition === 'reset') {
              beginReset(Object.assign(new Error('实时事件序号不连续'), { code: 'WORKSPACE_EVENT_GAP' }));
            }
          },
          onReady: (ready) => {
            if (!activeStream()) return;
            if (
              ready.cursor !== stateRef.current.cursor
              || ready.latestSeq !== ready.cursor
              || ready.minAvailableSeq > ready.cursor
            ) {
              beginReset(Object.assign(new Error('实时事件 Ready Cursor 不一致'), { code: 'WORKSPACE_EVENT_GAP' }));
              return;
            }
            reconnectAttemptRef.current = 0;
            commit({ type: 'stream_live' });
          },
          onReset: () => {
            if (!activeStream()) return;
            beginReset(Object.assign(new Error('实时事件窗口需要完整同步'), { code: 'WORKSPACE_RESET_REQUIRED' }));
          },
          onProtocolError: (error) => {
            if (!activeStream()) return;
            beginReset(error);
          },
          onTransportError: () => {
            if (!activeStream()) return;
            streamGenerationRef.current += 1;
            probeAndReconnect(project, snapshot, reconnectAttemptRef.current);
          }
        });
        if (activeStream()) {
          streamRef.current = openedStream;
        } else {
          openedStream?.close();
        }
      } catch (error) {
        if (!activeStream()) return;
        streamGenerationRef.current += 1;
        probeAndReconnect(project, snapshot, reconnectAttemptRef.current, error);
      }
    };

    // probeAndReconnect 只使用同一个 Workspace Snapshot API 判断身份、资源和 Cursor Window。
    const probeAndReconnect = async (project, originalSnapshot, attempt, cause) => {
      if (!current(originalSnapshot.session.id)) return;
      streamRef.current?.close();
      streamRef.current = null;
      commit({ type: 'stream_reconnecting', error: publicWorkspaceError(cause) });
      try {
        const payload = await loadSnapshot(originalSnapshot.session.id, { signal: controller.signal });
        if (!current(originalSnapshot.session.id)) return;
        const probe = parseWorkspaceSnapshot(payload, {
          expectedProjectID: project.projectID,
          expectedSessionID: originalSnapshot.session.id
        });
        if (probe.eventHighWatermark < stateRef.current.cursor) {
          beginReset(Object.assign(new Error('权威事件高水位发生倒退'), { code: 'WORKSPACE_PROJECTION_INVALID' }));
          return;
        }
        if (stateRef.current.cursor < probe.minAvailableSeq) {
          beginReset(Object.assign(new Error('实时事件游标已过期'), { code: 'WORKSPACE_CURSOR_EXPIRED' }));
          return;
        }
        if (attempt >= streamRetryDelays.length) {
          commit({ type: 'offline', error: publicWorkspaceError(cause) });
          return;
        }
        reconnectAttemptRef.current = attempt + 1;
        setTimer(() => connect(project, originalSnapshot, stateRef.current.cursor), streamRetryDelays[attempt]);
      } catch (error) {
        if (!current(originalSnapshot.session.id) || error?.name === 'AbortError') return;
        const status = Number(error?.status) || 0;
        if (status === 401 || status === 404) {
          fail(error);
          return;
        }
        if (attempt + 1 >= streamRetryDelays.length) {
          commit({ type: 'offline', error: publicWorkspaceError(error) });
          return;
        }
        reconnectAttemptRef.current = attempt + 1;
        setTimer(() => probeAndReconnect(project, originalSnapshot, attempt + 1, error), streamRetryDelays[attempt]);
      }
    };

    const loadReadySnapshot = async (project) => {
      commit({ type: 'loading_phase', phase: 'snapshot' });
      try {
        const payload = await loadSnapshot(project.sessionID, { signal: controller.signal });
        if (!current()) return;
        const snapshot = parseWorkspaceSnapshot(payload, {
          expectedProjectID: project.projectID,
          expectedSessionID: project.sessionID
        });
        reconnectAttemptRef.current = 0;
        commit({ type: 'snapshot_ready', project, snapshot });
        connect(project, snapshot, snapshot.eventHighWatermark);
      } catch (error) {
        fail(error);
      }
    };

    const loadProject = async (attempt) => {
      if (!current()) return;
      try {
        const payload = await bootstrap(projectID, { signal: controller.signal });
        if (!current()) return;
        const project = parseProjectBootstrap(payload, projectID);
        if (project.creationStatus === 'ready') {
          await loadReadySnapshot(project);
          return;
        }
        if (attempt + 1 >= projectRetryDelays.length) {
          commit({
            type: 'offline',
            error: publicWorkspaceError(Object.assign(new Error('工作台仍在准备，请稍后重试'), {
              code: 'SESSION_PROVISIONING', retryable: true
            }))
          });
          return;
        }
        commit({ type: 'loading_phase', phase: 'provisioning' });
        setTimer(() => loadProject(attempt + 1), projectRetryDelays[attempt + 1]);
      } catch (error) {
        fail(error);
      }
    };

    void loadProject(0);
    return () => {
      if (generation === generationRef.current) generationRef.current += 1;
      closeActiveResources();
    };
  }, [
    bootstrap,
    cancelSchedule,
    closeActiveResources,
    commit,
    cycleVersion,
    loadSnapshot,
    openStream,
    projectID,
    projectRetryDelays,
    requestCycle,
    schedule,
    streamRetryDelays
  ]);

  const retry = useCallback(() => {
    requestCycle(stateRef.current.snapshot ? 'reset' : 'loading');
  }, [requestCycle]);

  return { state, retry };
}

function publicWorkspaceError(error) {
  if (!error) return null;
  return {
    status: Number(error.status) || 0,
    code: String(error.code || 'WORKSPACE_UNAVAILABLE'),
    message: String(error.message || '工作台暂时不可用'),
    requestID: String(error.requestID || ''),
    retryable: Boolean(error.retryable) || Number(error.status) === 0 || Number(error.status) >= 500
  };
}
