#!/usr/bin/env python3
"""Run the release HTTP service E2E gate against a deployed test environment."""

from __future__ import annotations

from dataclasses import dataclass
import json
import os
import sys
from typing import Any
from urllib import error, parse, request
import uuid


DEFAULT_ACCOUNT = "user1001@dora.local"
DEFAULT_PASSWORD = "local-user-change-me"
DEFAULT_PROJECT_ID = "prj_active_1001"
DEFAULT_SPACE_ID = "sp_personal_1001"
DEFAULT_TIMEOUT_SECONDS = 10
DEFAULT_TRACE_ID = "trace-release-http-service-e2e"


class ReleaseHTTPServiceE2EError(Exception):
    """Raised when the release HTTP service E2E gate fails."""


@dataclass(frozen=True)
class Config:
    business_base_url: str
    agent_base_url: str
    account: str
    password: str
    project_id: str
    space_id: str
    trace_id: str
    timeout_seconds: int
    access_token: str


def required_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise ReleaseHTTPServiceE2EError(f"{name} is required")
    return value.rstrip("/")


def load_config() -> Config:
    return Config(
        business_base_url=required_env("RELEASE_BUSINESS_BASE_URL"),
        agent_base_url=required_env("RELEASE_AGENT_BASE_URL"),
        account=os.getenv("RELEASE_TEST_ACCOUNT", DEFAULT_ACCOUNT),
        password=os.getenv("RELEASE_TEST_PASSWORD", DEFAULT_PASSWORD),
        project_id=os.getenv("RELEASE_TEST_PROJECT_ID", DEFAULT_PROJECT_ID),
        space_id=os.getenv("RELEASE_TEST_SPACE_ID", DEFAULT_SPACE_ID),
        trace_id=os.getenv("RELEASE_TEST_TRACE_ID", DEFAULT_TRACE_ID),
        timeout_seconds=int(os.getenv("RELEASE_TEST_TIMEOUT_SECONDS", str(DEFAULT_TIMEOUT_SECONDS))),
        access_token=os.getenv("RELEASE_ACCESS_TOKEN", ""),
    )


def build_url(base_url: str, path: str) -> str:
    parsed = parse.urlparse(base_url)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ReleaseHTTPServiceE2EError(f"invalid base URL: {base_url}")
    return f"{base_url.rstrip('/')}/{path.lstrip('/')}"


def http_request(
    config: Config,
    method: str,
    url: str,
    *,
    token: str = "",
    idempotency_key: str = "",
    body: dict[str, Any] | None = None,
) -> bytes:
    encoded_body = None
    headers = {
        "Accept": "application/json",
        "X-Trace-Id": config.trace_id,
        "X-Space-Id": config.space_id,
    }
    if body is not None:
        encoded_body = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if idempotency_key:
        headers["Idempotency-Key"] = idempotency_key

    req = request.Request(url, data=encoded_body, headers=headers, method=method)
    try:
        with request.urlopen(req, timeout=config.timeout_seconds) as resp:
            status = resp.getcode()
            response_body = resp.read()
    except error.HTTPError as exc:
        response_body = exc.read().decode("utf-8", errors="replace")
        raise ReleaseHTTPServiceE2EError(f"{method} {url} status={exc.code} body={response_body}") from exc
    except error.URLError as exc:
        raise ReleaseHTTPServiceE2EError(f"{method} {url} failed: {exc}") from exc

    if status < 200 or status >= 300:
        raise ReleaseHTTPServiceE2EError(f"{method} {url} status={status} body={response_body.decode('utf-8', errors='replace')}")
    return response_body


def json_request(
    config: Config,
    method: str,
    url: str,
    *,
    token: str = "",
    idempotency_key: str = "",
    body: dict[str, Any] | None = None,
) -> dict[str, Any]:
    response_body = http_request(config, method, url, token=token, idempotency_key=idempotency_key, body=body)
    try:
        decoded = json.loads(response_body.decode("utf-8"))
    except json.JSONDecodeError as exc:
        raise ReleaseHTTPServiceE2EError(f"{method} {url} returned non-JSON body: {response_body!r}") from exc
    if not isinstance(decoded, dict):
        raise ReleaseHTTPServiceE2EError(f"{method} {url} returned non-object JSON: {decoded!r}")
    return decoded


def field(response: dict[str, Any], name: str) -> str:
    value = response.get(name)
    if isinstance(value, str) and value:
        return value

    data = response.get("data")
    if isinstance(data, dict):
        value = data.get(name)
        if isinstance(value, str) and value:
            return value

    raise ReleaseHTTPServiceE2EError(f"response missing {name}: {response!r}")


def status_value(response: dict[str, Any]) -> str:
    value = response.get("status")
    if isinstance(value, str) and value:
        return value

    data = response.get("data")
    if isinstance(data, dict):
        value = data.get("status")
        if isinstance(value, str) and value:
            return value

    raise ReleaseHTTPServiceE2EError(f"response missing status: {response!r}")


def events_from(response: dict[str, Any]) -> list[dict[str, Any]]:
    events = response.get("events")
    if not isinstance(events, list):
        data = response.get("data")
        if isinstance(data, dict):
            events = data.get("events")
    if not isinstance(events, list):
        raise ReleaseHTTPServiceE2EError(f"replay response missing events: {response!r}")

    out: list[dict[str, Any]] = []
    for event_item in events:
        if not isinstance(event_item, dict):
            raise ReleaseHTTPServiceE2EError(f"event is not an object: {event_item!r}")
        out.append(event_item)
    return out


def assert_event_types(response: dict[str, Any], *required_types: str) -> None:
    seen = {event.get("type") for event in events_from(response) if isinstance(event.get("type"), str)}
    missing = [event_type for event_type in required_types if event_type not in seen]
    if missing:
        raise ReleaseHTTPServiceE2EError(f"missing event types {missing}: {response!r}")


def assert_endpoint_ok(config: Config, service_name: str, base_url: str, path: str) -> None:
    url = build_url(base_url, path)
    http_request(config, "GET", url)
    print(f"[release-http-e2e] {service_name} {path} ok")


def login(config: Config, suffix: str) -> str:
    if config.access_token:
        print("[release-http-e2e] using RELEASE_ACCESS_TOKEN")
        return config.access_token

    response = json_request(
        config,
        "POST",
        build_url(config.business_base_url, "/api/auth/login"),
        idempotency_key=f"idem-release-http-login-{suffix}",
        body={
            "login_type": "personal",
            "account": config.account,
            "password": config.password,
        },
    )
    token = field(response, "access_token")
    print("[release-http-e2e] business login ok")
    return token


def create_agent_session(config: Config, token: str, suffix: str) -> str:
    response = json_request(
        config,
        "POST",
        build_url(config.agent_base_url, "/api/agent/sessions"),
        token=token,
        idempotency_key=f"idem-release-http-session-{suffix}",
        body={
            "project_id": config.project_id,
            "initial_title": "release HTTP service E2E",
        },
    )
    session_id = field(response, "session_id")
    print(f"[release-http-e2e] agent session ok: {session_id}")
    return session_id


def create_run(
    config: Config,
    token: str,
    suffix: str,
    session_id: str,
    *,
    run_intent: str,
    client_message_id: str,
    text: str,
) -> dict[str, Any]:
    return json_request(
        config,
        "POST",
        build_url(config.agent_base_url, "/api/agent/runs"),
        token=token,
        idempotency_key=f"idem-release-http-{run_intent}-{suffix}",
        body={
            "session_id": session_id,
            "project_id": config.project_id,
            "run_intent": run_intent,
            "user_input": {
                "client_message_id": client_message_id,
                "content_type": "text",
                "text": text,
            },
        },
    )


def replay_events(config: Config, token: str, run_id: str, limit: int) -> dict[str, Any]:
    encoded_run_id = parse.quote(run_id, safe="")
    return json_request(
        config,
        "GET",
        build_url(config.agent_base_url, f"/api/agent/runs/{encoded_run_id}/events?after_sequence=0&limit={limit}"),
        token=token,
    )


def run_gate() -> None:
    config = load_config()
    suffix = uuid.uuid4().hex[:12]

    assert_endpoint_ok(config, "business", config.business_base_url, "/healthz")
    assert_endpoint_ok(config, "business", config.business_base_url, "/readyz")
    assert_endpoint_ok(config, "agent", config.agent_base_url, "/healthz")
    assert_endpoint_ok(config, "agent", config.agent_base_url, "/readyz")

    token = login(config, suffix)
    session_id = create_agent_session(config, token, suffix)

    guide_run = create_run(
        config,
        token,
        suffix,
        session_id,
        run_intent="entry_guide",
        client_message_id=f"cm_release_http_guide_{suffix}",
        text="",
    )
    if status_value(guide_run) != "completed":
        raise ReleaseHTTPServiceE2EError(f"entry guide run should complete: {guide_run!r}")
    guide_replay = replay_events(config, token, field(guide_run, "run_id"), 50)
    assert_event_types(guide_replay, "creative.guide.presented", "agent.run.completed")
    print("[release-http-e2e] entry guide run and replay ok")

    normal_run = create_run(
        config,
        token,
        suffix,
        session_id,
        run_intent="normal",
        client_message_id=f"cm_release_http_normal_{suffix}",
        text="帮我做一个产品宣传片，年轻一点",
    )
    if status_value(normal_run) != "waiting_input":
        raise ReleaseHTTPServiceE2EError(f"normal run should stop at router clarify gate: {normal_run!r}")
    normal_replay = replay_events(config, token, field(normal_run, "run_id"), 100)
    assert_event_types(normal_replay, "creative.router.decided", "agent.message.completed")
    print("[release-http-e2e] router clarify run and replay ok")
    print("[release-http-e2e] release HTTP service E2E passed")


def main() -> int:
    try:
        run_gate()
    except ReleaseHTTPServiceE2EError as exc:
        print(f"[release-http-e2e] {exc}", file=sys.stderr)
        return 1
    except ValueError as exc:
        print(f"[release-http-e2e] invalid environment value: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
