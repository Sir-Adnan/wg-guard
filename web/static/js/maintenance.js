/* Route-local presentation; the host owns admission, scheduling and recovery.
 * Delegated handlers survive HTMX swaps without retaining removed page nodes. */
const currentPage = () => document.querySelector('[data-maintenance-page]');
const compact = matchMedia('(max-width:800px)');
let density = 'normal';
try { density = localStorage.getItem('wg-maintenance-density') || density; } catch { /* optional preference */ }
const applyDensity = () => {
  currentPage()?.querySelectorAll('.release-list').forEach(list => { list.dataset.density = density; });
  currentPage()?.querySelectorAll('[data-density-toggle]').forEach(button => button.setAttribute('aria-pressed', String(density === 'compact')));
};
const initialize = (force = false) => {
  applyDensity();
  const versions = currentPage()?.querySelector('[data-responsive-disclosure]');
  if (versions && (force === true || !versions.dataset.maintenanceInitialized)) {
    versions.open = !compact.matches; versions.dataset.maintenanceInitialized = 'true';
  }
};
initialize(); compact.addEventListener('change', () => initialize(true));
document.body.addEventListener('htmx:afterSwap', () => initialize());
document.addEventListener('click', event => {
  if (!event.target.closest('[data-maintenance-page] [data-density-toggle]')) return;
  density = density === 'compact' ? 'normal' : 'compact';
  try { localStorage.setItem('wg-maintenance-density', density); } catch { /* optional preference */ }
  applyDensity();
});
document.addEventListener('change', event => {
  if (!event.target.matches('[data-maintenance-selection] select[name="target"]')) return;
  const url = new URL(location.href); url.searchParams.set('scope', event.target.value); location.assign(url);
});
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-maintenance-page] [data-copy-report]');
  if (!button || button.disabled || !/^\/updates\/report\/[a-f0-9]{32}$/.test(button.dataset.copyReport)) return;
  button.disabled = true;
  const { toast } = await import(document.querySelector('meta[name="ui-module"]').content);
  try {
    const response = await fetch(button.dataset.copyReport, {credentials:'same-origin',signal:AbortSignal.timeout(10000)});
    if (!response.ok || response.redirected || !response.headers.get('content-type')?.startsWith('text/plain')) throw new Error('report unavailable');
    const report = await response.text(); if (report.length > 65536) throw new Error('report exceeds limit');
    await navigator.clipboard.writeText(report);
    toast(document.querySelector('meta[name="ui-copied"]').content);
  } catch {
    toast(document.querySelector('meta[name="ui-copy-error"]').content,'err');
  } finally { button.disabled = false; }
});
// A restart can interrupt the transport, while the durable host job continues.
// Authentication/authorization failures retain the shared error handling.
for (const type of ['htmx:sendError', 'htmx:timeout', 'htmx:responseError']) {
  document.body.addEventListener(type, event => {
    if (!event.target.closest?.('[data-maintenance-poll]')) return;
    const status = event.detail?.xhr?.status || 0;
    if (status >= 400 && status < 500) return;
    event.stopImmediatePropagation();
    const hint = currentPage()?.querySelector('[data-maintenance-connection]'); if (hint) hint.hidden = false;
  }, true);
}
document.body.addEventListener('htmx:afterRequest', event => {
  if (!event.target.closest?.('[data-maintenance-poll]') || !event.detail.successful) return;
  const hint = currentPage()?.querySelector('[data-maintenance-connection]'); if (hint) hint.hidden = true;
});
