// Shared full-size image viewer. Include after base.css, then call
// initLightbox() once per page, and openLightbox(items, startIndex) on click.
// items: [{ url, width, height, mode, format, file_size }]
let __lbItems = [];
let __lbIndex = 0;

function initLightbox() {
  if (document.getElementById('lightbox-overlay')) return;
  const overlay = document.createElement('div');
  overlay.id = 'lightbox-overlay';
  overlay.hidden = true;
  overlay.innerHTML = `
    <button class="lb-close" aria-label="Close">&times;</button>
    <button class="lb-nav prev" aria-label="Previous">&#8249;</button>
    <button class="lb-nav next" aria-label="Next">&#8250;</button>
    <div class="lb-stage">
      <img id="lb-img" />
      <div class="lb-meta" id="lb-meta"></div>
    </div>
  `;
  document.body.appendChild(overlay);

  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) closeLightbox();
  });
  overlay.querySelector('.lb-close').addEventListener('click', closeLightbox);
  overlay.querySelector('.lb-nav.prev').addEventListener('click', () => stepLightbox(-1));
  overlay.querySelector('.lb-nav.next').addEventListener('click', () => stepLightbox(1));
  document.addEventListener('keydown', (e) => {
    if (document.getElementById('lightbox-overlay').hidden) return;
    if (e.key === 'Escape') closeLightbox();
    if (e.key === 'ArrowLeft') stepLightbox(-1);
    if (e.key === 'ArrowRight') stepLightbox(1);
  });
}

function openLightbox(items, startIndex = 0) {
  __lbItems = items;
  __lbIndex = startIndex;
  document.getElementById('lightbox-overlay').hidden = false;
  document.body.style.overflow = 'hidden';
  renderLightboxFrame();
}

function closeLightbox() {
  const overlay = document.getElementById('lightbox-overlay');
  if (overlay) overlay.hidden = true;
  document.body.style.overflow = '';
}

function stepLightbox(delta) {
  if (!__lbItems.length) return;
  __lbIndex = (__lbIndex + delta + __lbItems.length) % __lbItems.length;
  renderLightboxFrame();
}

function renderLightboxFrame() {
  const item = __lbItems[__lbIndex];
  if (!item) return;
  document.getElementById('lb-img').src = item.url;
  const nav = document.querySelectorAll('#lightbox-overlay .lb-nav');
  nav.forEach(btn => btn.style.display = __lbItems.length > 1 ? '' : 'none');
  document.getElementById('lb-meta').innerHTML = `
    <span><b>${item.width}&times;${item.height}</b></span>
    ${item.mode ? `<span>${item.mode}</span>` : ''}
    ${item.format ? `<span>${item.format}</span>` : ''}
    ${item.file_size ? `<span>${bytesToSize(item.file_size)}</span>` : ''}
    ${__lbItems.length > 1 ? `<span class="lb-count">${__lbIndex + 1} / ${__lbItems.length}</span>` : ''}
    <a href="${item.url}" target="_blank" rel="noopener">Open original &#8599;</a>
    <a href="${item.url}" download>Download</a>
  `;
}
