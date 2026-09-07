// Renders the "switch variant" tab bar into #variant-bar. Include after base.css,
// then call renderVariantBar('<current-key>') from each variant page.
const VARIANTS = [
  { key: 'sidebar', label: 'Sidebar Studio', href: 'variants/v1-sidebar.html', emoji: '🎛️' },
  { key: 'wizard',  label: 'Guided Wizard',  href: 'variants/v2-wizard.html',  emoji: '🧭' },
  { key: 'dashboard', label: 'Dashboard Cards', href: 'variants/v3-dashboard.html', emoji: '🗂️' },
  { key: 'raw', label: 'Power / Raw JSON', href: 'variants/v4-raw.html', emoji: '⚙️' },
  { key: 'presets', label: 'Preset Picker', href: 'variants/v5-presets.html', emoji: '🎯' },
  { key: 'compare', label: 'Compare Slider', href: 'variants/v6-compare.html', emoji: '🔍' },
];

function renderVariantBar(currentKey) {
  const bar = document.getElementById('variant-bar');
  if (!bar) return;
  const isInVariants = location.pathname.includes('/variants/');
  const prefix = isInVariants ? '' : 'variants/';
  const homeHref = isInVariants ? '../index.html' : 'index.html';

  bar.innerHTML = `
    <a class="vb-home" href="${homeHref}" title="Back to variant gallery">&larr; All variants</a>
    <span class="vb-label">Prototype:</span>
    ${VARIANTS.map(v => `
      <a class="vb-tab ${v.key === currentKey ? 'active' : ''}"
         href="${isInVariants ? v.href.replace('variants/', '') : v.href}">
        <span>${v.emoji}</span>${v.label}
      </a>
    `).join('')}
    <span class="vb-spacer"></span>
    <span style="font-size:12px;color:var(--text-dim)">Same API, different UI — pick one to build in Vue</span>
  `;
}
