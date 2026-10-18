/* Live preview for the theme gallery. Only the server actions persist a
   choice; leaving the page restores the saved appearance. */
const root = document.documentElement;
const form = document.querySelector('[data-appearance-form]');
const AXES = ['density', 'sidebar', 'tabs', 'surface'];
const saved = { preset: root.dataset.visualPreset || '' };
for (const axis of AXES) saved[axis] = root.dataset[axis] || '';
const confirmation = form?.querySelector('input[name="confirm"]');
const nameOut = form?.querySelector('[data-appearance-name]');
const hint = form?.querySelector('[data-appearance-hint]');
const savedName = nameOut?.textContent || '';
const savedHint = hint?.textContent || '';
const loaded = new Map();

function stylesheet(href) {
  if (!href) return Promise.resolve();
  const existing = loaded.get(href) || document.querySelector(`link[rel="stylesheet"][href="${CSS.escape(href)}"]`);
  if (existing instanceof Promise) return existing;
  if (existing) return Promise.resolve();
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = href;
  link.dataset.presetPreview = '';
  const ready = new Promise(resolve => { link.onload = resolve; link.onerror = resolve; });
  loaded.set(href, ready);
  document.head.append(link);
  return ready;
}

function apply(state) {
  if (state.preset) root.dataset.visualPreset = state.preset;
  else delete root.dataset.visualPreset;
  for (const axis of AXES) if (state[axis]) root.dataset[axis] = state[axis];
}

function preview(input) {
  const id = input.value === 'wg-guard-neutral' ? '' : input.value;
  const state = { preset: id };
  for (const axis of AXES) state[axis] = input.dataset[axis];
  stylesheet(input.dataset.css).then(() => { if (input.checked) apply(state); });
  const name = input.closest('.preset-card')?.querySelector('.preset-name')?.textContent;
  if (name && nameOut) nameOut.textContent = name;
  const dirty = id !== saved.preset;
  if (hint) {
    hint.textContent = dirty ? hint.closest('form').dataset.unsaved || savedHint : savedHint;
    hint.dataset.state = dirty ? 'dirty' : '';
  }
}

form?.addEventListener('change', event => {
  if (event.target?.name === 'preset') preview(event.target);
});

/* Group filter: progressive enhancement (all cards show without JS). */
const filter = document.querySelector('[data-preset-filter]');
if (filter) {
  filter.hidden = false;
  filter.addEventListener('click', event => {
    const button = event.target.closest('[data-preset-group]');
    if (!button) return;
    for (const b of filter.querySelectorAll('[data-preset-group]')) b.setAttribute('aria-pressed', String(b === button));
    const group = button.dataset.presetGroup;
    for (const card of form.querySelectorAll('.preset-card')) card.hidden = Boolean(group) && card.dataset.group !== group;
  });
}

/* The panel-wide action needs its explicit confirmation; "apply to me" does not. */
form?.addEventListener('submit', event => {
  if (!event.submitter?.matches('[data-panel-default-submit]') || confirmation?.checked) return;
  event.preventDefault();
  if (confirmation) {
    confirmation.required = true;
    confirmation.reportValidity();
    confirmation.focus({ preventScroll: true });
  }
});
form?.querySelector('button[type="submit"]:not([data-panel-default-submit])')?.addEventListener('click', () => {
  if (confirmation) confirmation.required = false;
});

window.addEventListener('pagehide', () => {
  apply(saved);
  if (nameOut) nameOut.textContent = savedName;
});

/* The subscription preview opens the form's current, unsaved choices. */
const subForm = document.querySelector('[data-sub-appearance]');
const previewLink = subForm?.querySelector('[data-sub-preview-link]');
if (subForm && previewLink) {
  const sync = () => {
    const url = new URL(previewLink.href, location.href);
    const data = new FormData(subForm);
    for (const [param, field] of [['layout', 'sub_layout'], ['preset', 'sub_preset'], ['mode', 'sub_mode']]) {
      // Blank is meaningful ("follow the panel"), so it is sent too.
      if (data.has(field)) url.searchParams.set(param, data.get(field));
    }
    previewLink.href = url.pathname + url.search;
  };
  subForm.addEventListener('change', sync);
  sync();
}
