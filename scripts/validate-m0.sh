#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOROOT="${GOROOT:-/Users/figo/sdk/go1.26.3}"
export GOPATH="${GOPATH:-/Users/figo/go}"
export PATH="$GOROOT/bin:$GOPATH/bin:$PATH"

echo "== toolchain =="
go version
thriftgo --version
kitex --version

echo "== generated code baseline =="
test -f go.mod
test -f go.sum
test -f kitex-all.sh
test -d kitex_gen/dora/api/businessagent

echo "== yaml/json syntax and OpenAPI references =="
python3 - <<'PY'
from __future__ import annotations

import json
from pathlib import Path

import yaml


def fail(message: str) -> None:
    raise SystemExit(message)


for path in sorted(Path("api").glob("**/*.json")) + sorted(Path("tests").glob("**/*.json")):
    json.loads(path.read_text())

def walk(node):
    if isinstance(node, dict):
        ref = node.get("$ref")
        if isinstance(ref, str):
            yield ref
        for value in node.values():
            yield from walk(value)
    elif isinstance(node, list):
        for value in node:
            yield from walk(value)

for path in sorted(Path("api").glob("**/*.yaml")) + sorted(Path("api").glob("**/*.yml")):
    yaml.safe_load(path.read_text())


def validate_openapi(path: Path, require_named_components: bool) -> int:
    doc = yaml.safe_load(path.read_text())
    components = doc.get("components", {})

    def has_component(kind: str, name: str) -> bool:
        return name in components.get(kind, {})

    for ref in walk(doc):
        if not ref.startswith("#/components/"):
            continue
        _, _, kind, name = ref.split("/", 3)
        if not has_component(kind, name):
            fail(f"{path}: missing OpenAPI component {kind}/{name}")

    operation_ids = set()
    for route, item in doc.get("paths", {}).items():
        for method, operation in item.items():
            operation_id = operation.get("operationId")
            if not operation_id:
                fail(f"{path}: {method.upper()} {route} missing operationId")
            if operation_id in operation_ids:
                fail(f"{path}: duplicate operationId {operation_id}")
            operation_ids.add(operation_id)
            if require_named_components:
                ok = operation.get("responses", {}).get("200", {})
                ref = ok.get("$ref")
                if not ref or not ref.startswith("#/components/responses/"):
                    fail(f"{path}: {operation_id} missing named 200 response component")
                if operation.get("requestBody"):
                    body_ref = operation["requestBody"].get("$ref")
                    if not body_ref or not body_ref.startswith("#/components/requestBodies/"):
                        fail(f"{path}: {operation_id} missing named request body component")
    return len(operation_ids)


def validate_business_write_idempotency() -> None:
    doc = yaml.safe_load(Path("api/openapi/business-api.yaml").read_text())
    components = doc.get("components", {})
    for route, item in doc["paths"].items():
        for method, operation in item.items():
            if method.lower() not in {"post", "patch", "put", "delete"}:
                continue
            params = operation.get("parameters", [])
            has_idempotency = False
            for param in params:
                if "$ref" in param:
                    param = components.get("parameters", {}).get(param["$ref"].rsplit("/", 1)[-1], {})
                if param.get("name") == "Idempotency-Key" and param.get("in") == "header":
                    has_idempotency = True
            if not has_idempotency:
                fail(f"business OpenAPI write operation missing Idempotency-Key: {operation['operationId']}")
            request_body = operation.get("requestBody")
            if not request_body:
                fail(f"business OpenAPI write operation missing requestBody: {operation['operationId']}")
            if "$ref" in request_body:
                request_body = components["requestBodies"][request_body["$ref"].rsplit("/", 1)[-1]]
            schema = request_body["content"]["application/json"]["schema"]
            if "$ref" in schema:
                schema = components["schemas"][schema["$ref"].rsplit("/", 1)[-1]]
            if "request_hash" not in schema.get("properties", {}) or "request_hash" not in set(schema.get("required", [])):
                fail(f"business OpenAPI write request missing required request_hash: {operation['operationId']}")


business_count = validate_openapi(Path("api/openapi/business-api.yaml"), require_named_components=True)
agent_count = validate_openapi(Path("api/openapi/agent-workbench.yaml"), require_named_components=False)
validate_business_write_idempotency()

agent_openapi = Path("api/openapi/agent-workbench.yaml").read_text()
if "additionalProperties: true" in agent_openapi:
    fail("agent OpenAPI still contains additionalProperties: true generic object pockets")

print(f"openapi ok: business={business_count} operations agent={agent_count} operations")
PY

echo "== RPC code-plan alignment =="
python3 - <<'PY'
from __future__ import annotations

import re
from pathlib import Path


def fail(message: str) -> None:
    raise SystemExit(message)


thrift = Path("api/thrift/business_agent_service.thrift").read_text()
required_patterns = {
    r"struct\s+GeneratedAssetObjectInput\b": "GeneratedAssetObjectInput",
    r"3:\s+required\s+string\s+filename\b": "GeneratedAssetObjectInput.filename",
    r"4:\s+required\s+string\s+content_type\b": "GeneratedAssetObjectInput.content_type",
    r"struct\s+GeneratedAssetUploadSlot\b": "GeneratedAssetUploadSlot",
    r"2:\s+required\s+string\s+bucket\b": "GeneratedAssetUploadSlot.bucket",
    r"struct\s+GeneratedStorageObjectRef\b": "GeneratedStorageObjectRef",
    r"3:\s+required\s+string\s+content_type\b": "GeneratedStorageObjectRef.content_type",
    r"5:\s+required\s+string\s+checksum\b": "GeneratedStorageObjectRef.checksum",
    r"11:\s+required\s+GeneratedStorageObjectRef\s+storage_object_ref\b": "CommitArtifactDTO.storage_object_ref",
    r"struct\s+CommittedAssetRefDTO\b": "CommittedAssetRefDTO",
    r"10:\s+optional\s+string\s+estimate_id\b": "CommitGeneratedAssetAndChargeRequest.estimate_id",
    r"6:\s+optional\s+list<ChargedLineItemDTO>\s+charged_line_items\b": "CommitGeneratedAssetAndChargeResponse.charged_line_items",
    r"3:\s+optional\s+string\s+skill_scope_filter\b": "ListRoutableSkillsRequest.skill_scope_filter",
    r"7:\s+optional\s+string\s+account_id\b": "FreezeCreditsRequest.account_id",
}
for pattern, label in required_patterns.items():
    if not re.search(pattern, thrift):
        fail(f"RPC IDL missing code-plan baseline field: {label}")

forbidden_patterns = {
    r"struct\s+SkillScopeFilterDTO\b": "SkillScopeFilterDTO",
    r"struct\s+GeneratedArtifactPrepareInput\b": "GeneratedArtifactPrepareInput",
    r"struct\s+GeneratedAssetUploadSlotDTO\b": "GeneratedAssetUploadSlotDTO",
    r"struct\s+AssetRefDTO\b": "AssetRefDTO",
}
for pattern, label in forbidden_patterns.items():
    if re.search(pattern, thrift, flags=re.S):
        fail(f"RPC IDL still contains stale field/type: {label}")

freeze_match = re.search(r"struct\s+FreezeCreditsRequest\s*\{(?P<body>.*?)\n\}", thrift, flags=re.S)
if not freeze_match:
    fail("RPC IDL missing FreezeCreditsRequest")
if "credit_account_id" in freeze_match.group("body"):
    fail("RPC IDL still contains stale field/type: FreezeCreditsRequest.credit_account_id")

print("rpc code-plan alignment ok")
PY

echo "== blocked placeholder scan =="
if rg -n "JsonBody|ApiResponse|PageResponse" api/openapi; then
  echo "business OpenAPI still contains blocked placeholder names" >&2
  exit 1
fi

echo "== SQL external constraint keyword scan =="
if rg -n "FOREIGN KEY|REFERENCES" db/migrations api code-plan; then
  echo "SQL or contract area still contains blocked database-level external constraint keywords" >&2
  exit 1
fi

echo "== AG-UI fixture validation =="
python3 tests/agent/agui/validate_fixtures.py

echo "== business contract fixture validation =="
python3 tests/contract/validate_fixtures.py

echo "== Go generated code compile =="
go test ./...

echo "M0 validation passed"
