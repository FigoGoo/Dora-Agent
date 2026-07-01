#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== toolchain baseline strict baseline =="
scripts/validate-toolchain-contract-baseline.sh

echo "== Go toolchain =="
go_version="$(go version)"
echo "$go_version"
case "$go_version" in
  *"go1.26.3 darwin/arm64"*) ;;
  *) echo "unexpected Go toolchain: $go_version" >&2; exit 1 ;;
esac

echo "== gofmt dry check =="
unformatted="$(find services internal tests -name '*.go' -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  echo "gofmt required" >&2
  exit 1
fi

echo "== SQL up/down pair check =="
python3 - <<'PY'
from pathlib import Path

roots = [Path("db/migrations/iterations")]
missing = []
for root in roots:
    ups = {p.name[:-len(".up.sql")]: p for p in root.glob("**/*.up.sql")}
    downs = {p.name[:-len(".down.sql")]: p for p in root.glob("**/*.down.sql")}
    for name, path in ups.items():
        if name not in downs:
            missing.append(f"missing down for {path}")
    for name, path in downs.items():
        if name not in ups:
            missing.append(f"missing up for {path}")
if missing:
    raise SystemExit("\n".join(missing))
print(f"sql migration pairs ok: {len(ups)} pairs")
PY

echo "== SQL external constraint keyword scan =="
constraint_scan_paths=(db/migrations api services internal)
if [[ -d code-plan ]]; then
  constraint_scan_paths+=(code-plan)
fi
if rg -n "FOREIGN KEY|REFERENCES" "${constraint_scan_paths[@]}"; then
  echo "blocked database-level external constraint keyword found" >&2
  exit 1
fi

echo "== Agent/Business config key coverage =="
python3 - <<'PY'
from pathlib import Path

agent_src = "\n".join(p.read_text() for p in Path("services/agent").glob("**/*.go") if not p.name.endswith("_test.go"))
business_src = "\n".join(p.read_text() for p in Path("services/business").glob("**/*.go") if not p.name.endswith("_test.go"))

agent_keys = [
    "APP_ENV", "APP_NAME", "LOG_LEVEL", "AGENT_HTTP_PORT", "AGENT_HTTP_ADDR",
    "AGENT_DATABASE_URL", "AGENT_SERVICE_NAME", "BUSINESS_SERVICE_NAME",
    "KITEX_REGISTRY", "KITEX_TIMEOUT_MS", "ETCD_ENDPOINTS", "ETCD_NAMESPACE",
    "AGENT_SSE_ENABLED", "AGENT_WS_ENABLED", "AGENT_SSE_HEARTBEAT_SECONDS",
    "AGENT_EVENT_REPLAY_PAGE_SIZE", "AGENT_EVENT_REPLAY_MAX_PAGE_SIZE",
    "AGENT_CONFIG_SOURCE", "AGENT_DEFAULT_CONFIG_VERSION", "AGENT_TOOL_ALLOWLIST",
    "AGENT_MEMORY_ENABLED", "AGENT_TOOL_DEFAULT_TIMEOUT_MS", "AGENT_SAFETY_POLICY_VERSION",
]
business_keys = [
    "APP_ENV", "APP_NAME", "LOG_LEVEL", "BUSINESS_DATABASE_URL", "BUSINESS_KITEX_PORT",
    "BUSINESS_HTTP_PORT", "BUSINESS_HTTP_ENABLED", "BUSINESS_HTTP_ADDR", "PUBLIC_WEB_BASE_URL",
    "BUSINESS_SERVICE_NAME", "KITEX_REGISTRY", "KITEX_TIMEOUT_MS", "ETCD_ENDPOINTS",
    "ETCD_NAMESPACE", "ADMIN_BOOTSTRAP_ACCOUNT", "ADMIN_BOOTSTRAP_PASSWORD_HASH",
    "ADMIN_BOOTSTRAP_CREDENTIAL_SECRET_REF", "TOS_ENDPOINT", "TOS_BUCKET", "TOS_ACCESS_KEY_ID",
    "TOS_SECRET_ACCESS_KEY", "TOS_REGION", "TOS_BASE_URL", "TOS_REQUEST_TIMEOUT",
    "TOS_CONNECT_TIMEOUT", "VOLC_TLS_ENDPOINT", "VOLC_TLS_REGION", "VOLC_TLS_ACCESS_KEY_ID",
    "VOLC_TLS_ACCESS_KEY_SECRET", "VOLC_TLS_PROJECT_ID", "VOLC_TLS_TOPIC_ID",
    "SECRET_ENCRYPTION_KEY_REF", "CORS_ALLOWED_ORIGINS",
]

missing = []
for key in agent_keys:
    if key not in agent_src:
        missing.append(f"agent config loader missing {key}")
for key in business_keys:
    if key not in business_src:
        missing.append(f"business config loader missing {key}")
if missing:
    raise SystemExit("\n".join(missing))
print(f"config keys ok: agent={len(agent_keys)} business={len(business_keys)}")
PY

echo "== engineering baseline semantic alignment with active contracts =="
python3 - <<'PY'
from pathlib import Path

def fail(message: str) -> None:
    raise SystemExit(message)

business_sql = Path("db/migrations/iterations/2026-06-27-business-core/business/0001_common_idempotency_audit.up.sql").read_text()
idempotency_src = Path("services/business/internal/infra/idempotency/idempotency.go").read_text()
idempotency_tests = "\n".join(p.read_text() for p in Path("services/business/internal/infra/idempotency").glob("*_test.go"))
audit_src = Path("services/business/internal/pkg/auditlog/auditlog.go").read_text()
state_src = Path("services/agent/internal/domain/state/state.go").read_text()
agent_repository_test = Path("services/agent/internal/infra/repository/repository_integration_test.go").read_text()

required_sql = [
    "tenant_id varchar(64) NOT NULL",
    "UNIQUE (tenant_id, scope, idempotency_key)",
    "result_ref_type varchar(64)",
    "result_ref_id varchar(128)",
    "error_code varchar(64)",
    "audit_id varchar(64) PRIMARY KEY",
    "operator_type varchar(32) NOT NULL",
    "operator_id varchar(64)",
    "business_action varchar(128) NOT NULL",
    "metadata_summary jsonb",
]
for needle in required_sql:
    if needle not in business_sql:
        fail(f"business migration missing active contract field/constraint: {needle}")

required_idempotency = [
    "TenantID",
    'Where("tenant_id = ? AND scope = ? AND idempotency_key = ?"',
    "ReplayResult *ResultRef",
    "ResultRefType",
    "ResultRefID",
    "RequestHashInput",
    "canonicalBody",
    "shouldIgnoreHashField",
    '"tenant_id": input.TenantID',
    '"space_id"',
    '"actor_user_id"',
    "clause.OnConflict",
]
for needle in required_idempotency:
    if needle not in idempotency_src:
        fail(f"idempotency implementation missing tenant/result semantic: {needle}")
if 'Where("scope = ? AND idempotency_key = ?"' in idempotency_src:
    fail("idempotency implementation still queries by scope + key without tenant")
if "sha256.Sum256(body)" in idempotency_src:
    fail("idempotency request hash still hashes raw body bytes")

required_idempotency_tests = [
    "TestHashRequestCanonicalizesJSONAndIgnoresObservabilityFields",
    "TestHashRequestRequiresTenantAndActor",
    "testConcurrentSameKeyBegin",
    "DecisionProcessing",
    "DecisionConflict",
]
for needle in required_idempotency_tests:
    if needle not in idempotency_tests:
        fail(f"idempotency tests missing engineering baseline semantic coverage: {needle}")

required_audit = [
    "AuditID",
    "OperatorType",
    "OperatorID",
    "TenantID",
    "BusinessAction",
    "MetadataSummary",
]
for needle in required_audit:
    if needle not in audit_src:
        fail(f"audit implementation missing active contract field: {needle}")

required_states = [
    "RunStatusWaitingConfirmation = foundation.RunStatusWaitingConfirmation",
    'RunStatusResuming            = "resuming"',
    "RunStatusCancelled           = foundation.RunStatusCancelled",
    'InterruptStatusRequired = "required"',
    'InterruptStatusAccepted = "accepted"',
    'InterruptStatusRejected = "rejected"',
    'InterruptStatusExpired  = "expired"',
    'InterruptStatusResolved = "resolved"',
    "RunStatusWaitingConfirmation || to == RunStatusCompleted",
    "RunStatusResuming || to == RunStatusCancelled || to == RunStatusFailed",
    "to == RunStatusRunning || to == RunStatusFailed",
    "InterruptStatusAccepted || to == InterruptStatusRejected || to == InterruptStatusExpired",
]
for needle in required_states:
    if needle not in state_src:
        fail(f"agent state machine missing code-plan semantic: {needle}")
for needle in ['SafetyResultPassed  = "passed"', 'SafetyResultBlocked = "blocked"', 'SafetyResultFailed  = "failed"', "IsValidSafetyResult"]:
    if needle not in state_src:
        fail(f"agent safety result semantic missing: {needle}")

for forbidden in ["RunStatusInterrupted", 'RunStatusCanceled', "InterruptStatusPending", "InterruptStatusCanceled"]:
    if forbidden in state_src:
        fail(f"agent state machine still contains stale semantic: {forbidden}")
for forbidden in ['Result:                "allow"', 'got.Result != "allow"', 'Scene:                 "asset_publish"', 'TargetType:            "artifact"']:
    if forbidden in agent_repository_test:
        fail(f"agent repository test still contains stale safety semantic: {forbidden}")
if "ErrInvalidSafetyEvidence" not in agent_repository_test:
    fail("agent repository test does not reject stale safety evidence result")

print("engineering_baseline semantic alignment ok")
PY

echo "== Go tests, including Testcontainers PostgreSQL integration =="
go test ./...

echo "== Explicit repository integration package checks =="
go test ./services/agent/internal/infra/repository ./services/business/internal/infra/idempotency

echo "engineering baseline validation passed"
