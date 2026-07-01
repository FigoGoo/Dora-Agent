# Release HTTP Service E2E Report

status: passed
started_at: 2026-07-01T06:46:11Z
finished_at: 2026-07-01T06:46:11Z

## Command

`make release-http-service-e2e`

## Environment

- RELEASE_BUSINESS_BASE_URL: `http://127.0.0.1:51401`
- RELEASE_AGENT_BASE_URL: `http://127.0.0.1:51414`
- RELEASE_TEST_PROJECT_ID: `prj_active_1001`
- RELEASE_TEST_SPACE_ID: `sp_personal_1001`
- RELEASE_TEST_TRACE_ID: `trace-release-http-service-e2e`
- RELEASE_ACCESS_TOKEN: not recorded

## Evidence

- [x] business /healthz
- [x] business /readyz
- [x] agent /healthz
- [x] agent /readyz
- [x] Business login - /api/auth/login
- [x] Agent session - /api/agent/sessions
- [x] entry guide run completed - /api/agent/runs
- [x] entry guide replay - creative.guide.presented, agent.run.completed
- [x] normal run waiting_input - /api/agent/runs
- [x] router replay - creative.router.decided, agent.message.completed

## Runtime IDs

- session_id: `sess_djn16ipq9tvk`
- guide_run_id: `run_djn16iqnugog`
- normal_run_id: `run_djn16islfcls`

## Unexecuted

未执行项：无（release HTTP service E2E 范围内）
