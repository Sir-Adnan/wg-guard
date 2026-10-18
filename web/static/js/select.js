/* Themed select listbox. The native <select> keeps the value, validation and submission; a
 * button with the same classes takes its place in layout and opens one listbox at a time.
 * Options are read when the listbox opens, so scripts may replace them at any time.
 * No framework, no idle polling. Opt out per control with data-native. */
let sequence = 0;
let active = null;
const states = new WeakMap();
const ENHANCE = 'select.select:not([multiple]):not([data-native]), select[data-fill-preset]';

const sprite = document.querySelector('meta[name="ui-icons"]')?.content || '';
function icon(name, className) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('class', 'icon icon--sm ' + className); svg.setAttribute('aria-hidden', 'true');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', sprite + '#i-' + name);
  svg.append(use);
  return svg;
}

function dismiss(focus = false) {
  if (!active) return;
  const {button, panel, closeOverlays} = active;
  if (typeof panel.hidePopover === 'function' && panel.matches(':popover-open')) panel.hidePopover();
  panel.hidden = true; panel.replaceChildren();
  button.setAttribute('aria-expanded', 'false'); button.removeAttribute('aria-activedescendant');
  if (panel.parentElement !== button.parentElement && button.isConnected) button.after(panel);
  active = null;
  if (focus) closeOverlays();
  if (focus && button.isConnected) button.focus({preventScroll: true});
}

function bind(select, closeOverlays) {
  if (states.has(select) || select.multiple || select.size > 1) return;
  const button = document.createElement('button');
  button.type = 'button';
  // Same classes as the select, so layout rules written for `.select` place the trigger.
  button.className = select.className + ' select-trigger';
  button.classList.add('select');
  if (select.dir) button.dir = select.dir;
  const name = select.getAttribute('aria-label') || [...select.labels].map(label => label.textContent.trim()).join(' ');
  button.setAttribute('role', 'combobox'); button.setAttribute('aria-label', name);
  button.setAttribute('aria-haspopup', 'listbox'); button.setAttribute('aria-expanded', 'false');
  const label = document.createElement('span'); label.className = 'select-value';
  button.append(label, icon('chevrons-up-down', 'select-chevron'));
  const panel = document.createElement('div'); panel.className = 'select-content'; panel.hidden = true;
  panel.id = 'select-list-' + (++sequence); panel.setAttribute('role', 'listbox'); panel.setAttribute('aria-label', name);
  button.setAttribute('aria-controls', panel.id);
  if (typeof panel.showPopover === 'function') panel.setAttribute('popover', 'manual');

  let items = [], current = -1, typed = '', typedAt = 0;
  const state = {select, button, panel, closeOverlays};
  states.set(select, state);

  const sync = () => {
    const option = select.selectedOptions[0];
    label.textContent = option?.textContent.trim() || '';
    label.dir = option?.dir || 'auto';
    button.disabled = select.disabled; button.hidden = select.hidden;
    button.setAttribute('aria-invalid', select.getAttribute('aria-invalid') || 'false');
    const description = select.getAttribute('aria-describedby');
    if (description) button.setAttribute('aria-describedby', description); else button.removeAttribute('aria-describedby');
    items.forEach(item => item.setAttribute('aria-selected', String(item.option === select.selectedOptions[0])));
    if (button.disabled && active === state) dismiss();
  };
  const render = () => {
    const nodes = [];
    items = [];
    for (const child of select.children) {
      const options = child instanceof HTMLOptGroupElement ? [...child.children] : [child];
      if (child instanceof HTMLOptGroupElement) {
        const heading = document.createElement('div'); heading.className = 'select-label'; heading.setAttribute('role', 'presentation');
        heading.textContent = child.label; nodes.push(heading);
      }
      for (const option of options) {
        if (!(option instanceof HTMLOptionElement) || option.hidden) continue;
        const index = items.length;
        const item = document.createElement('div'); item.className = 'select-item'; item.id = panel.id + '-' + index;
        item.setAttribute('role', 'option');
        item.setAttribute('aria-disabled', String(option.disabled || Boolean(child.disabled)));
        const text = document.createElement('bdi'); text.className = 'select-item-text';
        text.textContent = option.textContent.trim(); text.dir = option.dir || 'auto';
        item.append(text, icon('check', 'select-check'));
        item.option = option;
        item.addEventListener('pointerdown', event => event.preventDefault());
        item.addEventListener('pointermove', () => { if (!isDisabled(index) && active === state && current !== index) point(index); });
        item.addEventListener('click', () => choose(index));
        items.push(item); nodes.push(item);
      }
    }
    panel.replaceChildren(...nodes);
  };
  const isDisabled = index => items[index]?.getAttribute('aria-disabled') === 'true';
  const point = index => {
    if (!items.length) return;
    current = Math.max(0, Math.min(items.length - 1, index));
    items.forEach((item, i) => item.toggleAttribute('data-highlighted', i === current));
    button.setAttribute('aria-activedescendant', items[current].id);
    const item = items[current].getBoundingClientRect(), box = panel.getBoundingClientRect();
    if (item.top < box.top + 4) panel.scrollTop -= box.top + 4 - item.top;
    else if (item.bottom > box.bottom - 4) panel.scrollTop += item.bottom - box.bottom + 4;
  };
  const position = () => {
    const rect = button.getBoundingClientRect();
    if (rect.bottom < 0 || rect.top > innerHeight) { dismiss(); return; }
    panel.style.inlineSize = Math.min(Math.max(rect.width, 180), innerWidth - 16) + 'px';
    const below = innerHeight - rect.bottom - 14, above = rect.top - 14;
    panel.style.maxBlockSize = Math.min(320, innerHeight - 16, Math.max(88, below, above)) + 'px';
    const bounds = panel.getBoundingClientRect(), rtl = getComputedStyle(button).direction === 'rtl';
    const up = below < bounds.height && above > below;
    panel.toggleAttribute('data-side-top', up);
    panel.style.left = Math.max(8, Math.min(innerWidth - bounds.width - 8, rtl ? rect.right - bounds.width : rect.left)) + 'px';
    panel.style.top = Math.max(8, Math.min(innerHeight - bounds.height - 8, up ? rect.top - bounds.height - 6 : rect.bottom + 6)) + 'px';
  };
  state.position = position;
  const open = () => {
    if (button.disabled) return;
    dismiss(); closeOverlays(); render(); sync(); active = state; panel.hidden = false; button.setAttribute('aria-expanded', 'true');
    if (typeof panel.showPopover === 'function') panel.showPopover();
    // Native dialogs must contain their fallback popup; card blur/overflow must not clip it.
    else (select.closest('dialog') || document.body).append(panel);
    position();
    if (active !== state) return;
    const selected = items.findIndex(item => item.option === select.selectedOptions[0]);
    point(selected >= 0 ? selected : items.findIndex((_, i) => !isDisabled(i)));
  };
  const choose = index => {
    const option = items[index]?.option;
    if (select.disabled || !option || isDisabled(index)) return;
    if (!option.selected) {
      option.selected = true;
      select.dispatchEvent(new Event('input', {bubbles: true})); select.dispatchEvent(new Event('change', {bubbles: true}));
    }
    sync(); dismiss(true);
  };
  const move = step => {
    let next = current;
    for (let n = 0; n < items.length; n++) { next = (next + step + items.length) % items.length; if (!isDisabled(next)) break; }
    point(next);
  };
  button.addEventListener('click', () => active === state ? dismiss() : open());
  button.addEventListener('keydown', event => {
    if (event.key === 'Tab') { dismiss(); return; }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault(); if (active !== state) open();
      if (event.key === 'Home' || event.key === 'End') {
        const enabled = items.map((_, index) => index).filter(index => !isDisabled(index));
        if (enabled.length) point(event.key === 'Home' ? enabled[0] : enabled.at(-1));
      } else move(event.key === 'ArrowDown' ? 1 : -1);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault(); if (active === state) choose(current); else open();
    } else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      const now = Date.now(); typed = now - typedAt < 700 ? typed + event.key : event.key; typedAt = now;
      if (active !== state) open();
      const index = items.findIndex((item, i) => !isDisabled(i) && item.option.textContent.trim().toLocaleLowerCase().startsWith(typed.toLocaleLowerCase()));
      if (index >= 0) point(index);
    }
  });
  button.addEventListener('blur', () => { if (active === state) dismiss(); });
  // A label click or validation focuses the hidden native control; the trigger takes over.
  select.addEventListener('focus', () => button.focus({preventScroll: true}));
  select.addEventListener('invalid', () => button.setAttribute('aria-invalid', 'true'));
  select.addEventListener('change', () => queueMicrotask(sync));
  select.addEventListener('ui:select-sync', () => queueMicrotask(sync));
  for (const type of ['input', 'change', 'reset']) select.form?.addEventListener(type, () => queueMicrotask(sync));
  select.closest('dialog')?.addEventListener('close', () => { if (active === state) dismiss(); });
  select.after(button, panel);
  select.classList.add('select-native'); select.tabIndex = -1; select.setAttribute('aria-hidden', 'true');
  sync();
  new MutationObserver(sync).observe(select, {attributes: true, attributeFilter: ['disabled', 'hidden', 'aria-invalid', 'aria-describedby'], childList: true, subtree: true});
}

export function enhanceSelects(root = document, closeOverlays = () => {}) {
  if (root.matches?.(ENHANCE)) bind(root, closeOverlays);
  root.querySelectorAll(ENHANCE).forEach(select => bind(select, closeOverlays));
}
document.addEventListener('pointerdown', event => { if (active && !active.button.contains(event.target) && !active.panel.contains(event.target)) dismiss(); }, true);
// An open Select owns Escape before help or the enclosing dialog handles it.
window.addEventListener('keydown', event => { if (event.key === 'Escape' && active) { event.preventDefault(); event.stopPropagation(); dismiss(true); } }, true);
window.addEventListener('resize', () => dismiss());
document.addEventListener('scroll', event => { if (active && !active.panel.contains(event.target)) active.position(); }, true);
document.body.addEventListener('htmx:beforeSwap', () => dismiss());
