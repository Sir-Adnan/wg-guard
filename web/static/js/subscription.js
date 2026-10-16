/* Public download enhancement. Native anchors remain the no-JavaScript path. */
document.addEventListener('click', async event => {
  const link = event.target.closest('[data-config-download]');
  if (!link || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  if (link.getAttribute('aria-busy') === 'true') return;
  const state = link.closest('.publicsub-device').querySelector('[data-download-state]');
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
  const state = button.closest('.publicsub-device')?.querySelector('[data-download-state]');
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

/* Open the setup guide on the visitor's platform. */
(() => {
  const ua = navigator.userAgent || '';
  const touchMac = /Macintosh/.test(ua) && navigator.maxTouchPoints > 1;
  const os = /Android/i.test(ua) ? 'android' : (/iPhone|iPad|iPod/.test(ua) || touchMac) ? 'ios'
    : /Windows/.test(ua) ? 'windows' : /Macintosh|Mac OS X/.test(ua) ? 'macos' : /Linux|X11/.test(ua) ? 'linux' : '';
  const input = os && document.getElementById('os-' + os);
  if (input) input.checked = true;
})();
