/* The preview is local to this page. Only the server actions persist choices. */
const root = document.documentElement;
const form = document.querySelector('[data-appearance-form]');
const originalPreset = root.dataset.visualPreset || 'wg-guard-neutral';
const confirmation = form?.querySelector('input[name="confirm"]');

form?.addEventListener('change', event => {
  if (event.target?.name !== 'preset') return;
  const id = event.target.value;
  if (id === 'wg-guard-neutral') delete root.dataset.visualPreset;
  else root.dataset.visualPreset = id;
  const name = event.target.closest('.visual-preset-choice')?.querySelector('.visual-preset-name strong')?.textContent;
  if (name) form.querySelector('[data-appearance-name] bdi').textContent = name;
});

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
  if (originalPreset === 'wg-guard-neutral') delete root.dataset.visualPreset;
  else root.dataset.visualPreset = originalPreset;
});
