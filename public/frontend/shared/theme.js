// Shared theme changer: light/dark/system + accent colour, persisted per-browser,
// shared across every variant (and the index gallery) since it's the same origin.
const THEME_KEY = 'image-resizer:theme-mode';   // 'light' | 'dark' | 'system'
const ACCENT_KEY = 'image-resizer:accent';      // hex string, or absent = default

const ACCENTS = [
  { name: 'Indigo', value: '#4f46e5' },
  { name: 'Teal', value: '#0d9488' },
  { name: 'Rose', value: '#e11d48' },
  { name: 'Amber', value: '#d97706' },
  { name: 'Emerald', value: '#059669' },
];

function getThemeMode() {
  try { return localStorage.getItem(THEME_KEY) || 'system'; } catch { return 'system'; }
}
function getAccent() {
  try { return localStorage.getItem(ACCENT_KEY) || ''; } catch { return ''; }
}

/** Apply the stored theme mode + accent to <html>. Safe to call multiple times. */
function applyTheme() {
  const mode = getThemeMode();
  const root = document.documentElement;
  if (mode === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', mode);

  const accent = getAccent();
  if (accent) root.style.setProperty('--accent', accent);
  else root.style.removeProperty('--accent');
}

function setThemeMode(mode) {
  try { localStorage.setItem(THEME_KEY, mode); } catch {}
  applyTheme();
  refreshThemeChangerUI();
}

function setAccent(hex) {
  try { localStorage.setItem(ACCENT_KEY, hex); } catch {}
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
    <div class="tc-accents" role="group" aria-label="Accent colour">
      ${ACCENTS.map(a => `<button type="button" class="tc-swatch" data-accent="${a.value}" style="background:${a.value}" title="${a.name}"></button>`).join('')}
      <label class="tc-swatch custom" title="Custom accent">
        <input type="color" id="tc-custom-color" value="${getAccent() || ACCENTS[0].value}" />
      </label>
    </div>
  `;
  el.querySelectorAll('[data-mode]').forEach(btn => {
    btn.addEventListener('click', () => setThemeMode(btn.dataset.mode));
  });
  el.querySelectorAll('[data-accent]').forEach(btn => {
    btn.addEventListener('click', () => setAccent(btn.dataset.accent));
  });
  el.querySelector('#tc-custom-color').addEventListener('input', (e) => setAccent(e.target.value));
  refreshThemeChangerUI();
}

function refreshThemeChangerUI() {
  const el = document.getElementById('theme-changer');
  if (!el) return;
  const mode = getThemeMode();
  const accent = getAccent();
  el.querySelectorAll('[data-mode]').forEach(btn => btn.classList.toggle('active', btn.dataset.mode === mode));
  el.querySelectorAll('[data-accent]').forEach(btn => btn.classList.toggle('active', !!accent && btn.dataset.accent.toLowerCase() === accent.toLowerCase()));
}

// Follow OS theme changes live while in "system" mode.
if (window.matchMedia) {
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (getThemeMode() === 'system') applyTheme();
  });
}
