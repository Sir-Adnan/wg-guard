/* Public download enhancement. Native anchors remain the no-JavaScript path. */
document.addEventListener('click', async event => {
  const link = event.target.closest('[data-config-download]');
  if (!link || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  if (link.getAttribute('aria-busy') === 'true') return;
  const state = link.closest('[data-sub-device]').querySelector('[data-download-state]');
  link.setAttribute('aria-busy', 'true');
  link.setAttribute('aria-disabled', 'true');
  state.setAttribute('role', 'status');
  state.textContent = state.dataset.loading;
  try {
    const response = await fetch(link.href, { cache: 'no-store', credentials: 'same-origin', redirect: 'error' });
    if (!response.ok || !response.headers.get('content-type')?.startsWith('application/octet-stream') || !response.headers.get('content-disposition')?.startsWith('attachment;')) {
      state.setAttribute('role', 'alert');
      state.textContent = response.status === 429 ? state.dataset.rateLimit : state.dataset.error;
      return;
    }
    const blob = new Blob([await response.arrayBuffer()], { type: 'application/octet-stream' });
    const objectURL = URL.createObjectURL(blob);
    const download = document.createElement('a');
    download.href = objectURL;
    download.download = /filename="([^"]+)"/.exec(response.headers.get('content-disposition'))?.[1] || 'wg-guard.conf';
    document.body.append(download);
    download.click();
    download.remove();
    setTimeout(() => URL.revokeObjectURL(objectURL), 1000);
    state.textContent = state.dataset.success;
  } catch {
    state.setAttribute('role', 'alert');
    state.textContent = state.dataset.error;
  } finally {
    link.removeAttribute('aria-busy');
    link.removeAttribute('aria-disabled');
  }
});

/* Copy the AmneziaVPN key for apps without the vpn:// handler (desktop, iOS). */
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-amnezia-copy]');
  if (!button) return;
  const state = button.closest('[data-sub-device]')?.querySelector('[data-download-state]');
  try {
    const response = await fetch(button.dataset.amneziaCopy, { cache: 'no-store', credentials: 'same-origin', redirect: 'error' });
    const key = (await response.text()).trim();
    if (!response.ok || !key.startsWith('vpn://')) throw new Error('unavailable');
    await navigator.clipboard.writeText(key);
    if (state) { state.setAttribute('role', 'status'); state.textContent = state.dataset.copied; }
  } catch {
    if (state) { state.setAttribute('role', 'alert'); state.textContent = state.dataset.error; }
  }
});

/* Copy a guide command or path. Without clipboard access the text is selected
 * so the visitor can copy it by hand; the result is announced politely. */
document.querySelectorAll('[data-sub-copy]').forEach(button => { button.hidden = false; });
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-sub-copy]');
  if (!button) return;
  const code = button.parentElement.querySelector('code');
  const status = document.querySelector('[data-sub-copy-status]');
  let copied = false;
  try { await navigator.clipboard.writeText(code.textContent.trim()); copied = true; } catch {
    getSelection()?.selectAllChildren(code);
  }
  if (status) status.textContent = copied ? status.dataset.copied : status.dataset.failed;
  if (!copied) return;
  button.dataset.copied = 'true';
  clearTimeout(button.copyReset);
  button.copyReset = setTimeout(() => { delete button.dataset.copied; }, 1500);
});

/* Open the setup guide on the visitor's platform. */
(() => {
  const ua = navigator.userAgent || '';
  const touchMac = /Macintosh/.test(ua) && navigator.maxTouchPoints > 1;
  const os = /Android/i.test(ua) ? 'android' : (/iPhone|iPad|iPod/.test(ua) || touchMac) ? 'ios'
    : /Windows/.test(ua) ? 'windows' : /Macintosh|Mac OS X/.test(ua) ? 'macos' : /Linux|X11/.test(ua) ? 'linux' : '';
  const input = os && document.getElementById('os-' + os);
  if (input) input.checked = true;
  // Keep the chosen tab visible inside the horizontally scrolling tab row
  // without moving the page (scrollIntoView would also scroll vertically).
  const reveal = radio => {
    const label = radio && document.querySelector(`label[for="${radio.id}"]`);
    const row = label?.parentElement;
    if (!row) return;
    const r = row.getBoundingClientRect(), l = label.getBoundingClientRect();
    const delta = l.left < r.left ? l.left - r.left - 8 : l.right > r.right ? l.right - r.right + 8 : 0;
    if (delta) row.scrollBy({ left: delta });
  };
  reveal(document.querySelector('input[name="os"]:checked'));
  document.addEventListener('change', event => {
    if (event.target.matches?.('input[name="os"]')) reveal(event.target);
  });
})();

/* Visitor's own page style: an allowlisted preset ID in a cookie scoped to
 * /sub/ (the server ignores unknown IDs). Blank restores the provider default. */
document.addEventListener('click', event => {
  const choice = event.target.closest('[data-sub-preset]');
  if (!choice) return;
  const id = choice.dataset.subPreset;
  const secure = location.protocol === 'https:' ? '; Secure' : '';
  document.cookie = 'wg_sub_preset=' + encodeURIComponent(id) + '; Path=/sub/; Max-Age=' + (id ? 31536000 : 0) + '; SameSite=Lax' + secure;
  location.reload();
});
