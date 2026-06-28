export function formatDateTime(value) {
  if (!value) {
    return '-';
  }
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(new Date(value));
}

export function readListPayload(data) {
  if (!data) {
    return { items: [], total: 0, nextPageToken: '' };
  }
  const listKey = Object.keys(data).find((key) => Array.isArray(data[key]));
  const items = listKey ? data[listKey] : Array.isArray(data.items) ? data.items : [];
  return {
    items,
    total: data.total || items.length,
    nextPageToken: data.next_page_token || ''
  };
}

export function statusTone(status = '') {
  const normalized = String(status).toLowerCase();
  if (['active', 'enabled', 'success', 'connected', 'published', 'approved', 'shared'].includes(normalized)) {
    return 'success';
  }
  if (['disabled', 'failed', 'rejected', 'deprecated', 'taken_down'].includes(normalized)) {
    return 'danger';
  }
  if (['pending', 'pending_review', 'draft', 'untested', 'testing', 'running'].includes(normalized)) {
    return 'warning';
  }
  return 'neutral';
}

export function toApiDateTime(value) {
  if (!value) {
    return '';
  }
  return new Date(value).toISOString();
}
