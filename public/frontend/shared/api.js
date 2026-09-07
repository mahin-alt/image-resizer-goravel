// Shared API client for the image-resizer backend.
// Same-origin (served from Goravel's ./public), so relative paths work with no CORS setup.
const API_BASE = '/api/v1';

const MODES = ['contain', 'fit', 'cover', 'crop', 'fill'];

/** Submit an image + array of size configs. Returns { status, id } (202) or throws with .errors / .message */
async function submitImage(file, sizes) {
  const form = new FormData();
  form.append('image', file);
  form.append('data', JSON.stringify({ sizes }));

  const res = await fetch(`${API_BASE}/images`, { method: 'POST', body: form });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(body.error || 'Request failed');
    err.errors = body.errors;
    err.status = res.status;
    throw err;
  }
  return body; // { status: 'pending', id }
}

/** Fetch current status/detail for a request id. */
async function getImageRequest(id) {
  const res = await fetch(`${API_BASE}/images/${id}`);
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(body.error || 'Not found');
    err.status = res.status;
    throw err;
  }
  return body;
}

const TERMINAL_STATUSES = ['completed', 'partially_completed', 'failed', 'expired'];

/**
 * Poll GET /images/{id} until a terminal status is reached.
 * onUpdate(detail) fires on every poll (including the first).
 * Returns the final detail. Stops after `maxMs` (default 2 min).
 */
async function pollImageRequest(id, { onUpdate, intervalMs = 1200, maxMs = 120000 } = {}) {
  const start = Date.now();
  while (true) {
    const detail = await getImageRequest(id);
    if (onUpdate) onUpdate(detail);
    if (TERMINAL_STATUSES.includes(detail.status)) return detail;
    if (Date.now() - start > maxMs) throw new Error('Timed out waiting for processing to finish');
    await new Promise((r) => setTimeout(r, intervalMs));
  }
}

// --- Local history of submitted requests (per-browser, this origin only) ---
const HISTORY_KEY = 'image-resizer:history';

function loadHistory() {
  try {
    return JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]');
  } catch {
    return [];
  }
}

function saveHistoryEntry(entry) {
  const list = loadHistory();
  list.unshift(entry);
  localStorage.setItem(HISTORY_KEY, JSON.stringify(list.slice(0, 50)));
}

function updateHistoryEntry(id, patch) {
  const list = loadHistory();
  const idx = list.findIndex((e) => e.id === id);
  if (idx !== -1) {
    list[idx] = { ...list[idx], ...patch };
    localStorage.setItem(HISTORY_KEY, JSON.stringify(list));
  }
}

// --- Small helpers shared by variants ---
function defaultSize(overrides = {}) {
  return {
    width: 800,
    height: 600,
    mode: 'cover',
    allow_upscale: false,
    quality: 80,
    ...overrides,
  };
}

function bytesToSize(bytes) {
  if (!bytes && bytes !== 0) return '';
  const sizes = ['B', 'KB', 'MB', 'GB'];
  if (bytes === 0) return '0 B';
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${sizes[i]}`;
}

function statusLabel(status) {
  return {
    pending: 'Queued',
    processing: 'Processing',
    completed: 'Completed',
    partially_completed: 'Partially completed',
    failed: 'Failed',
    expired: 'Expired',
  }[status] || status;
}
