# Release HTTP Service E2E Report

status: pending_environment
started_at: not_started
finished_at: not_finished

## Command

`make release-http-service-e2e`

## Environment

- RELEASE_BUSINESS_BASE_URL: `required`
- RELEASE_AGENT_BASE_URL: `required`
- RELEASE_TEST_PROJECT_ID: `prj_active_1001`
- RELEASE_TEST_SPACE_ID: `sp_personal_1001`
- RELEASE_TEST_TRACE_ID: `trace-release-http-service-e2e`
- RELEASE_ACCESS_TOKEN: not recorded

## Evidence

- [ ] business /healthz
- [ ] business /readyz
- [ ] agent /healthz
- [ ] agent /readyz
- [ ] Business login - /api/auth/login
- [ ] Agent session - /api/agent/sessions
- [ ] entry guide run completed - /api/agent/runs
- [ ] entry guide replay - creative.guide.presented, agent.run.completed
- [ ] normal run waiting_input - /api/agent/runs
- [ ] router replay - creative.router.decided, agent.message.completed

## Runtime IDs

- session_id: `not_created`
- guide_run_id: `not_created`
- normal_run_id: `not_created`

## Unexecuted

未执行原因：当前本地未提供 `RELEASE_BUSINESS_BASE_URL` 和 `RELEASE_AGENT_BASE_URL`；完整测试环境执行后由 `scripts/validate-release-http-service-e2e.sh` 自动覆盖本报告。
