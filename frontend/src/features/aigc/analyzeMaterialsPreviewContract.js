export const ANALYZE_MATERIALS_INTENT_SCHEMA = 'analyze_materials.preview.intent.v1';
export const ANALYZE_MATERIALS_ENQUEUE_SCHEMA = 'analyze_materials.preview.enqueue.v1';
export const ANALYZE_MATERIALS_CARD_SCHEMA = 'analyze_materials.preview.card.v1';

const CANDIDATE_SCHEMA = 'material_analysis.preview.candidate.v1';
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const SHA256 = /^[0-9a-f]{64}$/;
const RESULT_CODE = /^[A-Z][A-Z0-9_]{0,63}$/;
const LOCAL_ID = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$/;
const FOCUS = new Set(['content', 'visual', 'narrative', 'risk']);
const LANGUAGES = new Set(['zh-CN', 'en-US']);
const SUCCEEDED = new Set(['completed', 'partial']);
const ROOT_SUCCESS = ['schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'result_code', 'analysis', 'coverage', 'evidence_refs'];
const ROOT_FAILURE = ['schema_version', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'result_code', 'failure_kind', 'summary', 'retryable'];
const INTENT_FIELDS = ['schema_version', 'asset_ids', 'analysis_goal', 'focus_dimensions', 'output_language', 'expected_assets'];
const EXPECTED_ASSET_FIELDS = ['asset_id', 'asset_version'];
const ENQUEUE_FIELDS = ['schema_version', 'request_id', 'session_id', 'input_id', 'turn_id', 'run_id', 'tool_call_id', 'status', 'replayed'];
const CANDIDATE_FIELDS = ['schema_version', 'asset_summaries', 'cross_asset_findings', 'usable_elements', 'risks', 'open_questions', 'unused_evidence_ids'];
const SUMMARY_FIELDS = ['asset_id', 'summary', 'observations', 'inferences'];
const OBSERVATION_FIELDS = ['observation_id', 'text', 'evidence_ids'];
const INFERENCE_FIELDS = ['inference_id', 'text', 'based_on_observation_ids', 'confidence', 'uncertainty'];
const FINDING_FIELDS = ['finding_id', 'finding_type', 'text', 'asset_ids', 'evidence_ids', 'confidence', 'uncertainty'];
const ELEMENT_FIELDS = ['element_id', 'label', 'description', 'evidence_ids', 'constraints'];
const RISK_FIELDS = ['risk_id', 'category', 'statement', 'evidence_ids', 'severity', 'uncertainty'];
const QUESTION_FIELDS = ['question_id', 'question', 'asset_ids', 'missing_requirement_ids'];
const COVERAGE_FIELDS = ['status', 'evidence_policy_version', 'target_asset_ids', 'analyzable_asset_ids', 'included_evidence_ids', 'missing_requirements', 'target_asset_set_digest', 'included_evidence_set_digest', 'missing_requirement_set_digest'];
const MISSING_FIELDS = ['requirement_id', 'asset_id', 'asset_version', 'focus_dimension', 'evidence_kind', 'reason_code'];
const EVIDENCE_FIELDS = ['evidence_id', 'asset_id', 'asset_version', 'media_type', 'evidence_kind', 'content_digest', 'locator'];

export class AnalyzeMaterialsPreviewContractError extends Error {
  constructor(message) {
    super(message);
    this.name = 'AnalyzeMaterialsPreviewContractError';
    this.code = 'INVALID_ANALYZE_MATERIALS_PREVIEW';
    this.status = 502;
    this.retryable = false;
  }
}

// parseAnalyzeMaterialsIntent 仅接受显式结构化 Intent；expected_assets 必须与 asset_ids exact-set。
export function parseAnalyzeMaterialsIntent(payload) {
  const value = exactObject(payload, INTENT_FIELDS, 'Analyze Materials Intent');
  exact(value.schema_version, ANALYZE_MATERIALS_INTENT_SCHEMA, 'schema_version');
  const assetIDs = uniqueArray(value.asset_ids, 'asset_ids', 1, 8, uuid);
  const expectedAssets = objectArray(value.expected_assets, 'expected_assets', 1, 8, (item, index) => {
    exactObject(item, EXPECTED_ASSET_FIELDS, `expected_assets[${index}]`);
    return { asset_id: uuid(item.asset_id, `expected_assets[${index}].asset_id`), asset_version: integer(item.asset_version, `expected_assets[${index}].asset_version`, 1) };
  });
  if (expectedAssets.length !== assetIDs.length || expectedAssets.some((item) => !assetIDs.includes(item.asset_id)) || new Set(expectedAssets.map((item) => item.asset_id)).size !== expectedAssets.length) {
    throw error('expected_assets 必须与 asset_ids exact-set 一致');
  }
  return {
    schema_version: value.schema_version,
    asset_ids: assetIDs,
    analysis_goal: text(value.analysis_goal, 'analysis_goal', 1, 1000),
    focus_dimensions: uniqueArray(value.focus_dimensions, 'focus_dimensions', 1, 4, (item, field) => enumValue(item, FOCUS, field)),
    output_language: enumValue(value.output_language, LANGUAGES, 'output_language'),
    expected_assets: expectedAssets
  };
}

export function parseAnalyzeMaterialsEnqueueResponse(payload, expectedSessionID) {
  const value = exactObject(payload, ENQUEUE_FIELDS, 'Analyze Materials Enqueue');
  exact(value.schema_version, ANALYZE_MATERIALS_ENQUEUE_SCHEMA, 'schema_version');
  const sessionID = uuid(value.session_id, 'session_id');
  if (sessionID !== uuid(expectedSessionID, 'expected session_id')) throw error('Enqueue session_id 不一致');
  exact(value.status, 'pending', 'status');
  if (typeof value.replayed !== 'boolean') throw error('replayed 必须为布尔值');
  return { schemaVersion: value.schema_version, requestID: uuid(value.request_id, 'request_id'), sessionID, inputID: uuid(value.input_id, 'input_id'), turnID: uuid(value.turn_id, 'turn_id'), runID: uuid(value.run_id, 'run_id'), toolCallID: uuid(value.tool_call_id, 'tool_call_id'), status: value.status, replayed: value.replayed };
}

// parseAnalyzeMaterialsPreviewCard 严格解析安全投影，不接受 Evidence 正文、Prompt 或任意 metadata。
export function parseAnalyzeMaterialsPreviewCard(payload) {
  const candidate = object(payload, 'Analyze Materials Card');
  const succeeded = SUCCEEDED.has(candidate.status);
  const value = exactObject(candidate, succeeded ? ROOT_SUCCESS : ROOT_FAILURE, 'Analyze Materials Card');
  exact(value.schema_version, ANALYZE_MATERIALS_CARD_SCHEMA, 'schema_version');
  const base = { kind: 'analyze_materials_preview', schemaVersion: value.schema_version, inputID: uuid(value.input_id, 'input_id'), turnID: uuid(value.turn_id, 'turn_id'), runID: uuid(value.run_id, 'run_id'), toolCallID: uuid(value.tool_call_id, 'tool_call_id'), status: value.status, resultCode: resultCode(value.result_code) };
  if (succeeded) {
    const analysis = parseCandidate(value.analysis);
    const coverage = parseCoverage(value.coverage, value.status);
    const evidenceRefs = objectArray(value.evidence_refs, 'evidence_refs', 1, 32, parseEvidenceRef);
    unique(evidenceRefs.map((item) => item.evidenceID), 'evidence_refs.evidence_id');
    return { ...base, failureKind: null, analysis, coverage, evidenceRefs, summary: '', retryable: false };
  }
  exact(value.status, 'failed', 'status');
  if (typeof value.retryable !== 'boolean') throw error('retryable 必须为布尔值');
  return { ...base, failureKind: enumValue(value.failure_kind, new Set(['tool', 'runtime']), 'failure_kind'), analysis: null, coverage: null, evidenceRefs: [], summary: text(value.summary, 'summary', 1, 2000), retryable: value.retryable };
}

export function parseAnalyzeMaterialsProjection(payload) {
  return payload === null ? null : parseAnalyzeMaterialsPreviewCard(payload);
}

export function isAnalyzeMaterialsUUIDV7(value) { return typeof value === 'string' && UUID_V7.test(value); }

function parseCandidate(value) {
  exactObject(value, CANDIDATE_FIELDS, 'analysis');
  exact(value.schema_version, CANDIDATE_SCHEMA, 'analysis.schema_version');
  const assetSummaries = objectArray(value.asset_summaries, 'analysis.asset_summaries', 1, 8, (item, index) => {
    const field = `analysis.asset_summaries[${index}]`; exactObject(item, SUMMARY_FIELDS, field);
    return { assetID: uuid(item.asset_id, `${field}.asset_id`), summary: text(item.summary, `${field}.summary`, 1, 2000), observations: objectArray(item.observations, `${field}.observations`, 1, 16, parseObservation), inferences: objectArray(item.inferences, `${field}.inferences`, 0, 16, parseInference) };
  });
  unique(assetSummaries.map((item) => item.assetID), 'analysis.asset_summaries.asset_id');
  return {
    schemaVersion: value.schema_version, assetSummaries,
    crossAssetFindings: objectArray(value.cross_asset_findings, 'analysis.cross_asset_findings', 0, 16, parseFinding),
    usableElements: objectArray(value.usable_elements, 'analysis.usable_elements', 0, 16, parseElement),
    risks: objectArray(value.risks, 'analysis.risks', 0, 16, parseRisk),
    openQuestions: objectArray(value.open_questions, 'analysis.open_questions', 0, 16, parseQuestion),
    unusedEvidenceIDs: uniqueArray(value.unused_evidence_ids, 'analysis.unused_evidence_ids', 0, 32, uuid)
  };
}

function parseObservation(item, index) { const field = `observation[${index}]`; exactObject(item, OBSERVATION_FIELDS, field); return { observationID: localID(item.observation_id, `${field}.observation_id`), text: text(item.text, `${field}.text`, 1, 2000), evidenceIDs: uniqueArray(item.evidence_ids, `${field}.evidence_ids`, 1, 32, uuid) }; }
function parseInference(item, index) { const field = `inference[${index}]`; exactObject(item, INFERENCE_FIELDS, field); return { inferenceID: localID(item.inference_id, `${field}.inference_id`), text: text(item.text, `${field}.text`, 1, 2000), basedOnObservationIDs: uniqueArray(item.based_on_observation_ids, `${field}.based_on_observation_ids`, 1, 16, localID), confidence: enumValue(item.confidence, new Set(['low', 'medium', 'high']), `${field}.confidence`), uncertainty: text(item.uncertainty, `${field}.uncertainty`, 0, 1000) }; }
function parseFinding(item, index) { const field = `finding[${index}]`; exactObject(item, FINDING_FIELDS, field); return { findingID: localID(item.finding_id, `${field}.finding_id`), findingType: text(item.finding_type, `${field}.finding_type`, 1, 200), text: text(item.text, `${field}.text`, 1, 2000), assetIDs: uniqueArray(item.asset_ids, `${field}.asset_ids`, 2, 8, uuid), evidenceIDs: uniqueArray(item.evidence_ids, `${field}.evidence_ids`, 1, 32, uuid), confidence: enumValue(item.confidence, new Set(['low', 'medium', 'high']), `${field}.confidence`), uncertainty: text(item.uncertainty, `${field}.uncertainty`, 0, 1000) }; }
function parseElement(item, index) { const field = `element[${index}]`; exactObject(item, ELEMENT_FIELDS, field); return { elementID: localID(item.element_id, `${field}.element_id`), label: text(item.label, `${field}.label`, 1, 200), description: text(item.description, `${field}.description`, 1, 1000), evidenceIDs: uniqueArray(item.evidence_ids, `${field}.evidence_ids`, 1, 32, uuid), constraints: uniqueArray(item.constraints, `${field}.constraints`, 0, 16, (value, name) => text(value, name, 1, 1000)) }; }
function parseRisk(item, index) { const field = `risk[${index}]`; exactObject(item, RISK_FIELDS, field); return { riskID: localID(item.risk_id, `${field}.risk_id`), category: enumValue(item.category, new Set(['content_safety', 'privacy', 'copyright', 'brand', 'quality']), `${field}.category`), statement: text(item.statement, `${field}.statement`, 1, 2000), evidenceIDs: uniqueArray(item.evidence_ids, `${field}.evidence_ids`, 1, 32, uuid), severity: enumValue(item.severity, new Set(['low', 'medium', 'high']), `${field}.severity`), uncertainty: text(item.uncertainty, `${field}.uncertainty`, 0, 1000) }; }
function parseQuestion(item, index) { const field = `question[${index}]`; exactObject(item, QUESTION_FIELDS, field); return { questionID: localID(item.question_id, `${field}.question_id`), question: text(item.question, `${field}.question`, 1, 2000), assetIDs: uniqueArray(item.asset_ids, `${field}.asset_ids`, 1, 8, uuid), missingRequirementIDs: uniqueArray(item.missing_requirement_ids, `${field}.missing_requirement_ids`, 1, 32, (value, name) => text(value, name, 1, 200)) }; }

function parseCoverage(value, expectedStatus) {
  exactObject(value, COVERAGE_FIELDS, 'coverage'); exact(value.status, expectedStatus, 'coverage.status');
  return { status: value.status, evidencePolicyVersion: text(value.evidence_policy_version, 'coverage.evidence_policy_version', 1, 128), targetAssetIDs: uniqueArray(value.target_asset_ids, 'coverage.target_asset_ids', 1, 8, uuid), analyzableAssetIDs: uniqueArray(value.analyzable_asset_ids, 'coverage.analyzable_asset_ids', 1, 8, uuid), includedEvidenceIDs: uniqueArray(value.included_evidence_ids, 'coverage.included_evidence_ids', 1, 32, uuid), missingRequirements: objectArray(value.missing_requirements, 'coverage.missing_requirements', 0, 32, parseMissing), targetAssetSetDigest: sha(value.target_asset_set_digest, 'coverage.target_asset_set_digest'), includedEvidenceSetDigest: sha(value.included_evidence_set_digest, 'coverage.included_evidence_set_digest'), missingRequirementDigest: sha(value.missing_requirement_set_digest, 'coverage.missing_requirement_set_digest') };
}
function parseMissing(item, index) { const field = `missing_requirements[${index}]`; exactObject(item, MISSING_FIELDS, field); return { requirementID: text(item.requirement_id, `${field}.requirement_id`, 1, 200), assetID: uuid(item.asset_id, `${field}.asset_id`), assetVersion: integer(item.asset_version, `${field}.asset_version`, 1), focusDimension: enumValue(item.focus_dimension, FOCUS, `${field}.focus_dimension`), evidenceKind: text(item.evidence_kind, `${field}.evidence_kind`, 1, 64), reasonCode: resultCode(item.reason_code, `${field}.reason_code`) }; }
function parseEvidenceRef(item, index) { const field = `evidence_refs[${index}]`; exactObject(item, EVIDENCE_FIELDS, field); return { evidenceID: uuid(item.evidence_id, `${field}.evidence_id`), assetID: uuid(item.asset_id, `${field}.asset_id`), assetVersion: integer(item.asset_version, `${field}.asset_version`, 1), mediaType: enumValue(item.media_type, new Set(['text', 'image']), `${field}.media_type`), evidenceKind: text(item.evidence_kind, `${field}.evidence_kind`, 1, 64), contentDigest: sha(item.content_digest, `${field}.content_digest`), locator: parseLocator(item.locator, `${field}.locator`) }; }
function parseLocator(value, field) { const locator = object(value, field); if (locator.kind === 'image_whole') { exactObject(locator, ['kind'], field); return { kind: locator.kind }; } if (locator.kind === 'text_range') { exactObject(locator, ['kind', 'start', 'end', 'source_length'], field); const start = integer(locator.start, `${field}.start`, 0); const end = integer(locator.end, `${field}.end`, 1); const sourceLength = integer(locator.source_length, `${field}.source_length`, 1); if (start >= end || end > sourceLength) throw error(`${field} 范围非法`); return { kind: locator.kind, start, end, sourceLength }; } if (locator.kind === 'image_region') { exactObject(locator, ['kind', 'x', 'y', 'width', 'height'], field); const x = integer(locator.x, `${field}.x`, 0); const y = integer(locator.y, `${field}.y`, 0); const width = integer(locator.width, `${field}.width`, 1); const height = integer(locator.height, `${field}.height`, 1); if (x + width > 10000 || y + height > 10000) throw error(`${field} 区域非法`); return { kind: locator.kind, x, y, width, height }; } throw error(`${field}.kind 未知`); }

function exactObject(value, fields, label) { object(value, label); const actual = Object.keys(value).sort(); const expected = [...fields].sort(); if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) throw error(`${label} 字段集合不符合冻结契约`); return value; }
function object(value, label) { if (!value || typeof value !== 'object' || Array.isArray(value)) throw error(`${label} 必须为对象`); return value; }
function objectArray(value, field, min, max, parser) { if (!Array.isArray(value) || value.length < min || value.length > max) throw error(`${field} 数量不符合冻结契约`); return value.map((item, index) => parser(object(item, `${field}[${index}]`), index)); }
function uniqueArray(value, field, min, max, parser) { if (!Array.isArray(value) || value.length < min || value.length > max) throw error(`${field} 数量不符合冻结契约`); const parsed = value.map((item, index) => parser(item, `${field}[${index}]`)); unique(parsed, field); return parsed; }
function unique(values, field) { if (new Set(values).size !== values.length) throw error(`${field} 包含重复项`); }
function text(value, field, min, max) { if (typeof value !== 'string' || value !== value.normalize('NFC') || [...value].length < min || [...value].length > max) throw error(`${field} 不是冻结范围内 NFC 字符串`); return value; }
function uuid(value, field) { if (typeof value !== 'string' || !UUID_V7.test(value)) throw error(`${field} 必须为规范 UUIDv7`); return value; }
function sha(value, field) { if (typeof value !== 'string' || !SHA256.test(value)) throw error(`${field} 必须为小写 SHA-256`); return value; }
function resultCode(value, field = 'result_code') { if (typeof value !== 'string' || !RESULT_CODE.test(value)) throw error(`${field} 不是稳定错误码`); return value; }
function localID(value, field) { if (typeof value !== 'string' || !LOCAL_ID.test(value)) throw error(`${field} 不是规范局部 ID`); return value; }
function integer(value, field, min) { if (!Number.isSafeInteger(value) || value < min) throw error(`${field} 必须为安全整数`); return value; }
function enumValue(value, allowed, field) { if (typeof value !== 'string' || !allowed.has(value)) throw error(`${field} 使用了未知枚举`); return value; }
function exact(actual, expected, field) { if (actual !== expected) throw error(`${field} 不符合冻结契约`); }
function error(message) { return new AnalyzeMaterialsPreviewContractError(message); }
