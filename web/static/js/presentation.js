// Shared presentation primitives: no permissions, persistence or domain policy.
export function displayDigits(value) {
  const persian = document.documentElement.dataset.digits === 'persian';
  return String(value).replace(/[۰-۹٠-٩0-9]/g, character => {
    const code = character.charCodeAt(0);
    const number = code >= 0x6f0 ? code - 0x6f0 : code >= 0x660 ? code - 0x660 : code - 48;
    return String.fromCharCode((persian ? 0x6f0 : 48) + number);
  });
}
let activeHelp = null;
let helpTimer = null;
let suppressedHelpFocus = null;
function closeHelp() {
  clearTimeout(helpTimer);
  if (!activeHelp) return;
  const { details, content } = activeHelp;
  if (typeof content.hidePopover === 'function' && content.matches(':popover-open')) content.hidePopover();
  details.open = false;
  delete details.dataset.helpPinned;
  activeHelp = null;
}
function positionHelp(content, trigger) {
  const rect = trigger.getBoundingClientRect();
  const box = content.getBoundingClientRect();
  content.style.left = Math.max(8, Math.min(innerWidth - box.width - 8, rect.left + rect.width / 2 - box.width / 2)) + 'px';
  const below = rect.bottom + 8;
  content.style.top = Math.max(8, below + box.height <= innerHeight - 8 ? below : rect.top - box.height - 8) + 'px';
}
function showHelp(details) {
  if (activeHelp?.details === details) return;
  closeHelp();
  const content = details.querySelector('.field-help-content');
  activeHelp = { details, content };
  details.open = true;
  if (typeof content.showPopover === 'function') {
    content.showPopover();
    positionHelp(content, details.querySelector('summary'));
  }
}
export function enhanceGuidance(root = document) {
  const selectors = '.field > .hint, .form-section > p.hint, .form-section > span.hint, form .section-description, .section-head .hint, [data-guidance]';
  root.querySelectorAll(selectors).forEach(copy => {
    if (copy.closest('.field-help') || !copy.textContent.trim()) return;
    const host = copy.closest('.field') || copy.parentElement;
    const label = host.querySelector('label,legend,h2,h3')?.textContent.trim() || '';
    const help = document.createElement('details'); help.className = 'field-help';
    const trigger = document.createElement('summary'); trigger.className = 'field-help-trigger';
    trigger.setAttribute('aria-label', (document.querySelector('meta[name="ui-field-help"]')?.content || 'Help for %s').replace('%s', label));
    const glyph = document.createElement('span'); glyph.textContent = '?'; glyph.setAttribute('aria-hidden', 'true'); trigger.append(glyph);
    const content = document.createElement('div'); content.className = 'field-help-content';
    if (typeof content.showPopover === 'function') content.setAttribute('popover', 'manual');
    help.append(trigger, content);
    const title = host.querySelector(':scope > label, :scope > legend, :scope > h2, :scope > h3');
    if (title && title.tagName !== 'LEGEND') { const row = document.createElement('div'); row.className = 'field-label-row'; title.before(row); row.append(title, help); }
    else copy.before(help);
    content.append(copy);
    trigger.addEventListener('click', event => {
      event.preventDefault();
      if (help.dataset.helpPinned && help.open) closeHelp(); else { showHelp(help); help.dataset.helpPinned = 'true'; }
    });
    help.addEventListener('pointerenter', event => { if (event.pointerType === 'mouse') { clearTimeout(helpTimer); showHelp(help); } });
    help.addEventListener('pointerleave', () => { if (!help.dataset.helpPinned) helpTimer = setTimeout(() => { if (!help.contains(document.activeElement)) closeHelp(); }, 250); });
    content.addEventListener('pointerenter', () => clearTimeout(helpTimer));
    trigger.addEventListener('focus', () => { if (suppressedHelpFocus !== trigger) showHelp(help); });
    help.addEventListener('focusout', event => { if (!help.contains(event.relatedTarget)) closeHelp(); });
  });
}
export function enhanceSectionTabs(root = document) {
  root.querySelectorAll('[data-section-tabs]').forEach(group => {
    if (group.dataset.tabsReady) return;
    group.dataset.tabsReady = 'true';
    const list = group.querySelector('[data-tab-list]');
    const tabs = [...list.querySelectorAll('[data-tab-target]')];
    list.setAttribute('role', 'tablist');
    const activate = (tab, focus = false) => {
      closeHelp();
      for (const item of tabs) {
        const panel = group.querySelector('#' + item.dataset.tabTarget);
        const selected = item === tab;
        item.setAttribute('role', 'tab'); item.setAttribute('aria-selected', selected); item.tabIndex = selected ? 0 : -1;
        item.setAttribute('aria-controls', panel.id);
        panel.setAttribute('role', 'tabpanel'); panel.setAttribute('aria-labelledby', item.id); panel.hidden = !selected;
      }
      if (focus) tab.focus();
    };
    for (const tab of tabs) tab.addEventListener('click', () => activate(tab));
    list.addEventListener('keydown', event => {
      const current = tabs.indexOf(event.target); if (current < 0) return;
      const rtl = getComputedStyle(group).direction === 'rtl';
      let next = current;
      if (event.key === 'Home') next = 0;
      else if (event.key === 'End') next = tabs.length - 1;
      else if (event.key === 'ArrowRight') next = (current + (rtl ? -1 : 1) + tabs.length) % tabs.length;
      else if (event.key === 'ArrowLeft') next = (current + (rtl ? 1 : -1) + tabs.length) % tabs.length;
      else return;
      event.preventDefault(); activate(tabs[next], true);
    });
    const invalid = group.querySelector('[aria-invalid="true"]');
    activate(tabs.find(tab => group.querySelector('#' + tab.dataset.tabTarget)?.contains(invalid)) || tabs[0]);
  });
}

let activeChoiceMenu = null;
function closeChoiceMenu(returnFocus = false) {
  if (!activeChoiceMenu) return;
  closeHelp();
  const menu = activeChoiceMenu; const panel = menu.querySelector('[data-choice-panel]');
  if (typeof panel.hidePopover === 'function' && panel.matches(':popover-open')) panel.hidePopover();
  menu.open = false; activeChoiceMenu = null;
  if (returnFocus) menu.querySelector('summary').focus();
}
export function enhanceChoiceMenus(root = document) {
  root.querySelectorAll('[data-choice-menu]').forEach(menu => {
    if (menu.dataset.menuReady) return; menu.dataset.menuReady = 'true';
    const trigger = menu.querySelector('summary'); const panel = menu.querySelector('[data-choice-panel]');
    if (typeof panel.showPopover === 'function') panel.setAttribute('popover', 'manual');
    trigger.addEventListener('click', event => {
      event.preventDefault();
      if (menu.open) { closeChoiceMenu(); return; }
      closeHelp(); closeChoiceMenu(); activeChoiceMenu = menu; menu.open = true;
      if (typeof panel.showPopover === 'function') {
        panel.showPopover();
        const rect = trigger.getBoundingClientRect(); panel.style.width = Math.min(Math.max(rect.width, 280), innerWidth - 16) + 'px';
        positionHelp(panel, trigger);
      }
      panel.querySelector('input')?.focus({preventScroll:true});
    });
    const search = menu.querySelector('[data-choice-search]');
    if (menu.hasAttribute('data-choice-single')) {
      const syncSingle = () => { const selected = menu.querySelector('input[type="radio"]:checked'); if (selected) menu.querySelector('[data-choice-summary]').textContent = selected.closest('label').textContent.trim(); };
      syncSingle();
      menu.addEventListener('change', event => { if (event.target.type === 'radio') { syncSingle(); closeChoiceMenu(true); } });
    }
    search?.addEventListener('input', () => {
      const term = search.value.toLocaleLowerCase().trim();
      menu.querySelectorAll('.choice-option').forEach(option => { option.hidden = !option.textContent.toLocaleLowerCase().includes(term); });
    });
    panel.addEventListener('keydown', event => {
      const inputs = [...panel.querySelectorAll('input')].filter(input => !input.closest('[hidden]'));
      const current = inputs.indexOf(event.target); if (current < 0) return;
      let next;
      if (event.key === 'ArrowDown') next = (current + 1) % inputs.length;
      else if (event.key === 'ArrowUp') next = (current - 1 + inputs.length) % inputs.length;
      else if (event.key === 'Home' && event.target !== search) next = 0;
      else if (event.key === 'End' && event.target !== search) next = inputs.length - 1;
      else return;
      event.preventDefault(); inputs[next].focus();
    });
  });
}
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && activeChoiceMenu) { closeChoiceMenu(true); event.preventDefault(); event.stopPropagation(); return; }
  if (event.key === 'Escape' && activeHelp) { const trigger = activeHelp.details.querySelector('summary'); closeHelp(); event.preventDefault(); event.stopPropagation(); suppressedHelpFocus = trigger; trigger.focus(); queueMicrotask(() => { suppressedHelpFocus = null; }); }
}, true);
document.addEventListener('pointerdown', event => { if (activeHelp && !activeHelp.details.contains(event.target)) closeHelp(); if (activeChoiceMenu && !activeChoiceMenu.contains(event.target)) closeChoiceMenu(); }, true);
window.addEventListener('resize', () => { closeHelp(); closeChoiceMenu(); });
document.body.addEventListener('htmx:beforeSwap', () => { closeHelp(); closeChoiceMenu(); });
