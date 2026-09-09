// Shared theme changer: light/dark/system, persisted per-browser, shared across
// every variant (and the index gallery) since it's the same origin. The accent
// colour is fixed to teal (see shared/base.css) and isn't user-configurable.
const THEME_KEY = 'image-resizer:theme-mode';   // 'light' | 'dark' | 'system'

function getThemeMode() {
  try { return localStorage.getItem(THEME_KEY) || 'system'; } catch { return 'system'; }
}

/** Apply the stored theme mode to <html>. Safe to call multiple times. */
function applyTheme() {
  const mode = getThemeMode();
  const root = document.documentElement;
  if (mode === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', mode);
}

function setThemeMode(mode) {
  try { localStorage.setItem(THEME_KEY, mode); } catch {}
  applyTheme();
  refreshThemeChangerUI();
}

// Apply immediately on script load (before the rest of the page renders) to avoid a flash.
applyTheme();

/**
 * Render the theme-changer control into a container.
 * If `targetId` is given and exists, it's rendered inside that element (e.g. the
 * shared #variant-bar). Otherwise it creates a floating pill fixed to the top-right —
 * used by pages like index.html that have no variant bar to dock into.
 */
function renderThemeChanger(targetId) {
  let host = targetId && document.getElementById(targetId);
  let el = document.getElementById('theme-changer');
  if (!el) {
    el = document.createElement('div');
    el.id = 'theme-changer';
    el.className = 'theme-changer' + (host ? '' : ' floating');
    (host || document.body).appendChild(el);
  }
  el.innerHTML = `
    <div class="tc-modes" role="group" aria-label="Theme">
      <button type="button" data-mode="light" title="Light">&#9728;&#65039;</button>
      <button type="button" data-mode="dark" title="Dark">&#127769;</button>
      <button type="button" data-mode="system" title="Match system">&#128421;&#65039;</button>
    </div>
  `;
  el.querySelectorAll('[data-mode]').forEach(btn => {
    btn.addEventListener('click', () => setThemeMode(btn.dataset.mode));
  });
  refreshThemeChangerUI();
}

function refreshThemeChangerUI() {
  const el = document.getElementById('theme-changer');
  if (!el) return;
  const mode = getThemeMode();
  el.querySelectorAll('[data-mode]').forEach(btn => btn.classList.toggle('active', btn.dataset.mode === mode));
}

// Follow OS theme changes live while in "system" mode.
if (window.matchMedia) {
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (getThemeMode() === 'system') applyTheme();
  });
}
