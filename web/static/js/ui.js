/* Shared interaction contract. No business state, dependencies or idle polling. */
const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => [...root.querySelectorAll(s)];
const text = key => $('meta[name="ui-' + key + '"]')?.content || '';
const focusable = root => $$('a[href],button,input,select,textarea,[tabindex]', root)
  .filter(el => !el.disabled && el.tabIndex >= 0 && !el.closest('[inert]') && el.getClientRects().length);
const restoreFocus = el => { if (el?.isConnected && !el.closest('[inert]')) el.focus({ preventScroll: true }); };
document.documentElement.dataset.ui = 'ready';
document.documentElement.dataset.inputModality = 'pointer';
document.addEventListener('pointerdown', () => { document.documentElement.dataset.inputModality = 'pointer'; }, true);
document.addEventListener('keydown', event => {
  if (event.key === 'Tab' || event.key.startsWith('Arrow') || ['Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) {
    document.documentElement.dataset.inputModality = 'keyboard';
  }
}, true);

function toastMarker(kind) {
  const marker = document.createElement('span');
  marker.className = 'toast-icon';
  marker.setAttribute('aria-hidden', 'true');
  marker.textContent = kind === 'err' ? '!' : '✓';
  return marker;
}

function positionFeedback(item, anchor) {
  const target = anchor.getBoundingClientRect();
  const box = item.getBoundingClientRect();
  const left = Math.max(8, Math.min(innerWidth - box.width - 8, target.left + target.width / 2 - box.width / 2));
  const above = target.top - box.height - 9;
  item.style.left = left + 'px';
  item.style.top = (above >= 8 ? above : Math.min(innerHeight - box.height - 8, target.bottom + 9)) + 'px';
}

export function toast(message, kind = 'ok', anchor = null) {
  if (!message) return;
  const item = document.createElement('div');
  item.className = 'toast toast--' + (kind === 'err' ? 'err' : 'ok');
  item.setAttribute('role', kind === 'err' ? 'alert' : 'status');
  const copy = document.createElement('span');
  copy.textContent = message;
  item.append(toastMarker(kind), copy);

  if (anchor instanceof Element && anchor.isConnected) {
    $('.copy-feedback')?.remove();
    item.classList.add('copy-feedback');
    document.body.append(item);
    positionFeedback(item, anchor);
    setTimeout(() => item.remove(), kind === 'err' ? 5000 : 2200);
    return;
  }

  const host = $('.toasts');
  if (!host) return;
  const dismiss = document.createElement('button');
  dismiss.type = 'button'; dismiss.className = 'icon-btn';
  dismiss.textContent = '×'; dismiss.setAttribute('aria-label', text('close'));
  dismiss.addEventListener('click', () => item.remove());
  item.append(dismiss); host.append(item);
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
let activeTip = null;
let tipAnchor = null;
let priorDescription = null;

function hideTip() {
  activeTip?.remove();
  if (tipAnchor) {
    if (priorDescription === null) tipAnchor.removeAttribute('aria-describedby');
    else tipAnchor.setAttribute('aria-describedby', priorDescription);
  }
  activeTip = tipAnchor = null;
  priorDescription = null;
}

function showTip(anchor) {
  if (!anchor?.dataset.tip || (anchor.closest('.nav') && shell && !shell.hasAttribute('data-collapsed'))) return;
  if (tipAnchor === anchor) return;
  hideTip();
  const tip = document.createElement('div');
  tip.id = 'ui-floating-tip';
  tip.className = 'floating-tip is-visible';
  tip.setAttribute('role', 'tooltip');
  tip.textContent = anchor.dataset.tip;
  document.body.append(tip);
  activeTip = tip;
  tipAnchor = anchor;
  priorDescription = anchor.getAttribute('aria-describedby');
  anchor.setAttribute('aria-describedby', priorDescription ? priorDescription + ' ' + tip.id : tip.id);
  const target = anchor.getBoundingClientRect();
  const box = tip.getBoundingClientRect();
  const left = Math.max(8, Math.min(innerWidth - box.width - 8, target.left + target.width / 2 - box.width / 2));
  const above = target.top - box.height - 8;
  tip.style.left = left + 'px';
  tip.style.top = (above >= 8 ? above : Math.min(innerHeight - box.height - 8, target.bottom + 8)) + 'px';
}

function closeMenu(returnFocus = false) {
  if (!activeMenu) return;
  activeMenu.classList.remove('is-open');
  activeMenu.style.cssText = '';
  menuTrigger?.setAttribute('aria-expanded', 'false');
  const trigger = menuTrigger;
  activeMenu = menuTrigger = null;
  if (returnFocus) restoreFocus(trigger);
}
function positionMenu() {
  if (!activeMenu || !menuTrigger) return;
  const r = menuTrigger.getBoundingClientRect();
  activeMenu.style.position = 'fixed'; activeMenu.style.insetInlineEnd = 'auto';
  activeMenu.style.maxHeight = Math.max(120, innerHeight - 16) + 'px';
  activeMenu.style.overflowY = 'auto';
  const menuRect = activeMenu.getBoundingClientRect();
  const w = menuRect.width, h = menuRect.height;
  activeMenu.style.left = Math.max(8, Math.min(document.documentElement.dir === 'rtl' ? r.left : r.right - w, innerWidth - w - 8)) + 'px';
  activeMenu.style.top = Math.max(8, r.bottom + h + 6 > innerHeight ? r.top - h - 6 : r.bottom + 6) + 'px';
}
function openMenu(trigger, last = false) {
  hideTip();
  closeMenu();
  const menu = $('.menu', trigger.parentElement);
  if (!menu) return;
  activeMenu = menu; menuTrigger = trigger;
  menu.classList.add('is-open'); trigger.setAttribute('aria-expanded', 'true');
  positionMenu();
  const items = focusable(menu);
  items[last ? items.length - 1 : 0]?.focus({ preventScroll: true });
}

const invokers = new WeakMap();
export function openModal(id, invoker = document.activeElement) {
  const dialog = document.getElementById(id);
  if (!dialog?.showModal) return null;
  if (!dialog.open) {
    invokers.set(dialog, invoker);
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
  const disclosureTrigger = target.closest('[data-open-disclosure]');
  if (disclosureTrigger) {
    const disclosure = document.getElementById(disclosureTrigger.dataset.openDisclosure);
    if (disclosure instanceof HTMLDetailsElement) {
      event.preventDefault();
      disclosure.open = true;
      disclosure.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'auto' });
      requestAnimationFrame(() => focusable(disclosure).find(el => !el.matches('summary'))?.focus({ preventScroll: true }));
    }
    return;
  }
  const trigger = target.closest('.menu-anchor > button');
  if (trigger) {
    event.preventDefault();
    menuTrigger === trigger ? closeMenu(true) : openMenu(trigger);
    return;
  }
  const theme = target.closest('[data-theme-choice]');
  if (theme) { setTheme(theme.dataset.themeChoice); closeMenu(true); }
  else if (activeMenu && (!target.closest('.menu') || target.closest('a,button'))) {
    closeMenu(Boolean(target.closest('.menu a,.menu button')));
  }
  if (target.closest('#btn-drawer')) setDrawer(true);
  if (target.closest('#scrim,[data-close-nav]') || (drawerOpen && target.closest('.nav a'))) setDrawer(false);
  if (target.closest('#btn-collapse')) setCollapsed(!shell.hasAttribute('data-collapsed'));
  const opener = target.closest('[data-open-modal]');
  if (opener) { event.preventDefault(); openModal(opener.dataset.openModal, opener); }
  if (target.closest('[data-close-modal]')) target.closest('dialog')?.close();
});

document.addEventListener('pointerover', event => {
  const anchor = event.target.closest?.('[data-tip]');
  if (anchor && !anchor.contains(event.relatedTarget)) showTip(anchor);
});
document.addEventListener('pointerout', event => {
  if (tipAnchor && tipAnchor.contains(event.target) && !tipAnchor.contains(event.relatedTarget)) hideTip();
});
document.addEventListener('focusin', event => {
  const anchor = event.target.closest?.('[data-tip]');
  if (anchor) showTip(anchor);
  const body = event.target.closest?.('dialog.drawer .drawer-body');
  if (body) requestAnimationFrame(() => {
    if (!body.closest('dialog')?.open || !event.target.isConnected) return;
    const view = body.getBoundingClientRect();
    const field = event.target.getBoundingClientRect();
    if (field.bottom > view.bottom - 8) body.scrollTop += field.bottom - view.bottom + 8;
    else if (field.top < view.top + 8) body.scrollTop -= view.top - field.top + 8;
  });
});
document.addEventListener('focusout', event => {
  if (tipAnchor && tipAnchor.contains(event.target) && !tipAnchor.contains(event.relatedTarget)) hideTip();
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
  hideTip();
  if (activeMenu && !activeMenu.contains(event.target)) {
    // Responsive tables may scroll a trigger into view as it is clicked.
    // Keep its menu anchored instead of dismissing it before the first action.
    if (event.target instanceof Element && event.target.contains(menuTrigger)) positionMenu();
    else closeMenu();
  }
}, true);
window.addEventListener('resize', () => { hideTip(); closeMenu(); });

// WebKit can finish native focus scrolling after the first paint and leave a
// control partly outside a short viewport. Correct the settled position while
// preserving ordinary focus and the nearest scroll container.
document.addEventListener('focusin', event => {
  const target = event.target;
  if (!(target instanceof HTMLElement) || document.documentElement.dataset.inputModality !== 'keyboard') return;
  const dialog = target.closest('dialog[open]');
  if (!dialog && (!target.closest('form') || !target.matches('input,select,textarea,button'))) return;
  const expose = () => {
    if (document.activeElement !== target) return;
    const box = target.getBoundingClientRect();
    if (dialog) {
      if (box.top < 0 || box.bottom > innerHeight) target.scrollIntoView({ block: 'center', inline: 'nearest', behavior: 'auto' });
      return;
    }
    const header = document.querySelector('.topbar')?.getBoundingClientRect();
    const upper = (header?.bottom || 0) + 8, lower = innerHeight - 8;
    if (box.top < upper) window.scrollBy({ top: box.top - upper, behavior: 'auto' });
    else if (box.bottom > lower) window.scrollBy({ top: box.bottom - lower, behavior: 'auto' });
  };
  requestAnimationFrame(() => requestAnimationFrame(() => {
    expose();
    setTimeout(expose, 120);
  }));
});

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
  if (form.matches('[data-file-download]')) setTimeout(() => recover(form), 1500);
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
