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
    if (!response.ok || !response.headers.get('content-type')?.startsWith('text/plain') || !response.headers.get('content-disposition')?.startsWith('attachment;')) {
      state.setAttribute('role', 'alert');
      state.textContent = response.status === 429 ? state.dataset.rateLimit : state.dataset.error;
      return;
    }
    const blob = await response.blob();
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
