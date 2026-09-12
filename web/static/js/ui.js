/* Shared interaction contract. No business state, dependencies or idle polling. */
const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => [...root.querySelectorAll(s)];
const text = key => $('meta[name="ui-' + key + '"]')?.content || '';
const focusable = root => $$('a[href],button,input,select,textarea,[tabindex]', root)
  .filter(el => !el.disabled && el.tabIndex >= 0 && !el.closest('[inert]') && el.getClientRects().length);
const restoreFocus = el => { if (el?.isConnected && !el.closest('[inert]')) el.focus({ preventScroll: true }); };
document.documentElement.dataset.ui = 'ready';

export function toast(message, kind = 'ok') {
  if (!message) return;
  const host = $('.toasts');
  if (!host) return;
  const item = document.createElement('div');
  item.className = 'toast toast--' + (kind === 'err' ? 'err' : 'ok');
  item.setAttribute('role', kind === 'err' ? 'alert' : 'status');
  const copy = document.createElement('span');
  copy.textContent = message;
  const dismiss = document.createElement('button');
  dismiss.type = 'button'; dismiss.className = 'icon-btn';
  dismiss.textContent = '×'; dismiss.setAttribute('aria-label', text('close'));
  dismiss.addEventListener('click', () => item.remove());
  item.append(copy, dismiss); host.append(item);
  while (host.children.length > 3) host.firstElementChild.remove();
  // Errors stay available until dismissed. A focused/hovered message never vanishes.
  if (kind !== 'err') setTimeout(() => {
    if (!item.matches(':hover') && !item.contains(document.activeElement)) item.remove();
  }, 6000);
}

function syncTheme() {
  const selected = document.documentElement.dataset.theme || 'system';
  $$('[data-theme-choice]').forEach(el => el.setAttribute('aria-checked', String(el.dataset.themeChoice === selected)));
}
function setTheme(choice) {
  if (choice === 'dark' || choice === 'light') document.documentElement.dataset.theme = choice;
  else { choice = 'system'; delete document.documentElement.dataset.theme; }
  document.cookie = 'wg_theme=' + choice + ';path=/;max-age=31536000;samesite=lax';
  syncTheme();
}

let activeMenu = null;
let menuTrigger = null;
function closeMenu(returnFocus = false) {
  if (!activeMenu) return;
  activeMenu.classList.remove('is-open');
  activeMenu.style.cssText = '';
  menuTrigger?.setAttribute('aria-expanded', 'false');
  const trigger = menuTrigger;
  activeMenu = menuTrigger = null;
  if (returnFocus) restoreFocus(trigger);
}
function openMenu(trigger, last = false) {
  closeMenu();
  const menu = $('.menu', trigger.parentElement);
  if (!menu) return;
  activeMenu = menu; menuTrigger = trigger;
  menu.classList.add('is-open'); trigger.setAttribute('aria-expanded', 'true');
  const r = trigger.getBoundingClientRect();
  menu.style.position = 'fixed'; menu.style.insetInlineEnd = 'auto';
  menu.style.maxHeight = Math.max(120, innerHeight - 16) + 'px';
  menu.style.overflowY = 'auto';
  const w = menu.offsetWidth, h = menu.offsetHeight;
  menu.style.left = Math.max(8, Math.min(document.documentElement.dir === 'rtl' ? r.left : r.right - w, innerWidth - w - 8)) + 'px';
  menu.style.top = Math.max(8, r.bottom + h + 6 > innerHeight ? r.top - h - 6 : r.bottom + 6) + 'px';
  const items = focusable(menu);
  items[last ? items.length - 1 : 0]?.focus();
}

const invokers = new WeakMap();
export function openModal(id) {
  const dialog = document.getElementById(id);
  if (!dialog?.showModal) return null;
  if (!dialog.open) {
    invokers.set(dialog, document.activeElement);
    closeMenu();
    dialog.showModal();
  }
  return dialog;
}
document.addEventListener('close', event => {
  if (event.target instanceof HTMLDialogElement) restoreFocus(invokers.get(event.target));
}, true);

const sidebar = $('#sidebar');
const main = $('.main');
const drawerTrigger = $('#btn-drawer');
const mobile = matchMedia('(max-width: 960px)');
let drawerOpen = false;
function setDrawer(open, returnFocus = true) {
  if (!sidebar) return;
  open = open && mobile.matches;
  const wasOpen = drawerOpen;
  drawerOpen = open;
  closeMenu();
  sidebar.classList.toggle('is-open', open);
  sidebar.inert = mobile.matches && !open;
  $('#scrim')?.classList.toggle('is-open', open);
  drawerTrigger?.setAttribute('aria-expanded', String(open));
  if (main) main.inert = open;
  document.body.classList.toggle('nav-open', open);
  if (open) {
    sidebar.setAttribute('role', 'dialog'); sidebar.setAttribute('aria-modal', 'true');
    focusable(sidebar)[0]?.focus();
  } else {
    sidebar.removeAttribute('role'); sidebar.removeAttribute('aria-modal');
    if (wasOpen && returnFocus) restoreFocus(mobile.matches ? drawerTrigger : $('.nav [aria-current="page"]'));
  }
}
mobile.addEventListener('change', () => setDrawer(false));
setDrawer(false, false);

const shell = $('#shell');
function setCollapsed(collapsed) {
  if (!shell) return;
  shell.toggleAttribute('data-collapsed', collapsed);
  $('#btn-collapse')?.setAttribute('aria-expanded', String(!collapsed));
  try { localStorage.setItem('wg_sidebar', collapsed ? '1' : '0'); } catch { /* optional preference */ }
}
try { if (localStorage.getItem('wg_sidebar') === '1') setCollapsed(true); } catch { /* optional preference */ }

document.addEventListener('click', event => {
  const target = event.target instanceof Element ? event.target : null;
  if (!target) return;
  const trigger = target.closest('.menu-anchor > button');
  if (trigger) { menuTrigger === trigger ? closeMenu(true) : openMenu(trigger); return; }
  const theme = target.closest('[data-theme-choice]');
  if (theme) { setTheme(theme.dataset.themeChoice); closeMenu(true); }
  else if (activeMenu && (!target.closest('.menu') || target.closest('a,button'))) {
    closeMenu(Boolean(target.closest('.menu a,.menu button')));
  }
  if (target.closest('#btn-drawer')) setDrawer(true);
  if (target.closest('#scrim,[data-close-nav]') || (drawerOpen && target.closest('.nav a'))) setDrawer(false);
  if (target.closest('#btn-collapse')) setCollapsed(!shell.hasAttribute('data-collapsed'));
  const opener = target.closest('[data-open-modal]');
  if (opener) { event.preventDefault(); openModal(opener.dataset.openModal); }
  if (target.closest('[data-close-modal]')) target.closest('dialog')?.close();
});

document.addEventListener('keydown', event => {
  const trigger = event.target.closest?.('.menu-anchor > button');
  if (trigger && ['ArrowDown', 'ArrowUp'].includes(event.key)) {
    event.preventDefault(); openMenu(trigger, event.key === 'ArrowUp'); return;
  }
  if (activeMenu) {
    if (event.key === 'Escape') { event.preventDefault(); closeMenu(true); return; }
    if (event.key === 'Tab') { closeMenu(true); return; }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault();
      const items = focusable(activeMenu), index = items.indexOf(document.activeElement);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 :
        (index + (event.key === 'ArrowUp' ? -1 : 1) + items.length) % items.length;
      items[next]?.focus(); return;
    }
  }
  if (!drawerOpen) return;
  if (event.key === 'Escape') { event.preventDefault(); setDrawer(false); }
  if (event.key === 'Tab') {
    const items = focusable(sidebar), first = items[0], last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  }
});
document.addEventListener('scroll', event => {
  if (activeMenu && !activeMenu.contains(event.target)) closeMenu();
}, true);
window.addEventListener('resize', () => closeMenu());

// Keep submitter values successful: do not disable form controls before serialization.
const pending = new Map();
function markPending(form) {
  const controls = $$('button[type="submit"],button:not([type]),input[type="submit"]', form);
  pending.set(form, controls.map(el => [el, el.getAttribute('aria-disabled')]));
  controls.forEach(el => el.setAttribute('aria-disabled', 'true'));
  form.setAttribute('aria-busy', 'true');
}
function recover(form) {
  const controls = pending.get(form);
  if (!controls) return;
  controls.forEach(([el, prior]) => prior === null ? el.removeAttribute('aria-disabled') : el.setAttribute('aria-disabled', prior));
  form.removeAttribute('aria-busy'); pending.delete(form);
}
function recoverAll() { for (const form of pending.keys()) recover(form); }
window.addEventListener('submit', event => {
  const form = event.target;
  // Window runs after document-level form/confirmation handlers. A microtask
  // queued in an earlier listener can run before those handlers cancel the event.
  if (event.defaultPrevented || form.method.toLowerCase() !== 'post') return;
  if (pending.has(form)) { event.preventDefault(); return; }
  markPending(form);
});
document.addEventListener('click', event => {
  if (event.target.closest?.('[aria-disabled="true"]')) event.preventDefault();
}, true);
window.addEventListener('pageshow', recoverAll);
document.body.addEventListener('htmx:configRequest', event => {
  const csrf = $('meta[name="csrf-token"]');
  if (csrf) event.detail.headers['X-CSRF-Token'] = csrf.content;
});
document.body.addEventListener('htmx:beforeRequest', event => {
  if (document.hidden && event.target.closest?.('[data-pause-hidden]')) { event.preventDefault(); return; }
  const form = event.target.closest('form');
  if (form && event.detail.requestConfig.verb === 'post') {
    if (pending.has(form)) { event.preventDefault(); return; }
    markPending(form);
  }
  event.target.setAttribute('aria-busy', 'true');
});
document.body.addEventListener('htmx:afterRequest', event => {
  event.target.removeAttribute('aria-busy'); recover(event.target.closest('form'));
});
for (const type of ['htmx:responseError', 'htmx:sendError', 'htmx:timeout']) {
  document.body.addEventListener(type, event => {
    recoverAll();
    const code = event.detail?.xhr?.getResponseHeader?.('X-WG-Error');
    toast(code === 'csrf' || code === 'forbidden' ? text('error-' + code) : text('error'), 'err');
  });
}
document.body.addEventListener('htmx:beforeCleanupElement', event => {
  if (activeMenu && (event.target.contains(activeMenu) || event.target.contains(menuTrigger))) closeMenu();
  for (const form of pending.keys()) if (event.target.contains(form)) recover(form);
});
document.body.addEventListener('wg:toast', event => {
  const d = event.detail || {};
  toast(typeof d === 'string' ? d : d.message || d.value, d.kind);
});
syncTheme();
document.addEventListener('click', event => {
  const toggle = event.target.closest('[data-toggle-password]');
  if (!toggle) return;
  const input = document.getElementById(toggle.dataset.togglePassword);
  if (!input) return;
  const show = input.type === 'password';
  input.type = show ? 'text' : 'password';
  toggle.setAttribute('aria-pressed', String(show));
});
document.querySelector('[data-initial-focus]')?.focus();
