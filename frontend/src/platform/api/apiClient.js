import { notifyAuthSessionExpired } from '../auth/authEvents.js';

// ApiError 表示前端统一处理的 HTTP API 错误，保留稳定错误码和请求关联标识。
export class ApiError extends Error {
  constructor({ status, code, message, requestID, details, retryable = false, cause } = {}) {
    super(message || '请求失败', cause ? { cause } : undefined);
    this.name = 'ApiError';
    this.status = Number(status) || 0;
    this.code = String(code || 'API_REQUEST_FAILED');
    this.requestID = String(requestID || '');
    this.details = details;
    this.retryable = Boolean(retryable);
  }
}

// requestJSON 通过统一的 Cookie 会话调用 JSON API，并将失败响应转换为 ApiError。
export async function requestJSON(path, options = {}) {
  const { suppressAuthExpiryNotification = false, ...fetchOptions } = options;
  const headers = normalizeHeaders(options.headers);
  const body = options.body;

  if (!hasHeader(headers, 'accept')) {
    headers.Accept = 'application/json';
  }
  if (body != null && !isFormData(body) && !hasHeader(headers, 'content-type')) {
    headers['Content-Type'] = 'application/json';
  }

  const response = await fetch(path, {
    ...fetchOptions,
    credentials: 'include',
    headers
  });

  if (!response.ok) {
    const error = await apiErrorFromResponse(response);
    if (response.status === 401 && !suppressAuthExpiryNotification) {
      notifyAuthSessionExpired(error);
    }
    throw error;
  }
  if (response.status === 204) {
    return null;
  }

  const payload = await readResponsePayload(response);
  if (payload.empty) {
    return null;
  }
  if (!payload.isJSON) {
    throw new ApiError({
      status: response.status,
      code: 'INVALID_JSON_RESPONSE',
      message: '服务返回了无法识别的数据格式',
      requestID: response.headers?.get?.('x-request-id') || ''
    });
  }
  return payload.value;
}

// requestOptionalJSON 调用可选 JSON 资源，只有 404 会转换为 null。
export async function requestOptionalJSON(path, options) {
  try {
    return await requestJSON(path, options);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return null;
    }
    throw error;
  }
}

// apiErrorFromResponse 按稳定错误 Envelope、普通 JSON 和纯文本的顺序映射错误。
async function apiErrorFromResponse(response) {
  const payload = await readResponsePayload(response);
  const responseObject = payload.isJSON && payload.value && typeof payload.value === 'object' ? payload.value : {};
  const envelope = Object.keys(responseObject).length
    ? responseObject.error && typeof responseObject.error === 'object'
      ? responseObject.error
      : responseObject
    : {};
  const requestID = envelope.request_id || response.headers?.get?.('x-request-id') || '';
  const fallbackMessage = payload.isJSON
    ? typeof responseObject.error === 'string' ? responseObject.error : ''
    : String(payload.value || '').trim();

  return new ApiError({
    status: response.status,
    code: envelope.code || `HTTP_${response.status}`,
    message: envelope.message || fallbackMessage || response.statusText || `请求失败（HTTP ${response.status}）`,
    requestID,
    details: envelope.details,
    retryable: envelope.retryable
  });
}

// readResponsePayload 只读取一次响应 Body，避免错误解析覆盖原始失败信息。
async function readResponsePayload(response) {
  const text = await response.text();
  if (!text.trim()) {
    return { empty: true, isJSON: true, value: null };
  }
  try {
    return { empty: false, isJSON: true, value: JSON.parse(text) };
  } catch {
    return { empty: false, isJSON: false, value: text };
  }
}

// normalizeHeaders 把 Headers 或普通对象转换为可安全扩展的普通对象。
function normalizeHeaders(headers) {
  if (typeof Headers === 'function' && headers instanceof Headers) {
    return Object.fromEntries(headers.entries());
  }
  return { ...(headers || {}) };
}

// hasHeader 以不区分大小写的方式判断 Header 是否已经由调用方提供。
function hasHeader(headers, name) {
  const expected = name.toLowerCase();
  return Object.keys(headers).some((key) => key.toLowerCase() === expected);
}

// isFormData 判断请求体是否为浏览器 FormData，避免手工覆盖 multipart boundary。
function isFormData(value) {
  return typeof FormData === 'function' && value instanceof FormData;
}
