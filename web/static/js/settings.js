/* Settings-only enhancement. Native anchors/forms remain fully usable. */
const form = document.querySelector("[data-settings-form]");
if (form) {
  const controls = [...form.elements].filter(el => el.name && el.name !== "_csrf" && !el.disabled);
  const snapshot = () => controls.map(el => el.type === "checkbox" ? el.checked : el.value);
  const initial = snapshot();
  const retry = form.dataset.settingsRetry === "true";
  let dirty = retry;
  let submitting = false;
  const status = form.querySelector("[data-settings-status]");
  const update = () => {
    dirty = retry || snapshot().some((value, i) => value !== initial[i]);
    const message = dirty ? form.dataset.settingsDirty : form.dataset.settingsClean;
    if (status.textContent !== message) status.textContent = message;
    form.classList.toggle("is-dirty", dirty);
  };
  form.addEventListener("input", update);
  form.addEventListener("change", update);
  // Engines differ in how native focus scrolling accounts for a sticky footer.
  form.addEventListener("focusin", event => {
    const target = event.target;
    const bar = form.querySelector('.settings-savebar');
    if (!(target instanceof HTMLElement) || !bar || bar.contains(target)) return;
    // WebKit may apply native focus scrolling after the first frame callback.
    // Check the resulting geometry in the following frame before correcting it.
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (document.activeElement !== target) return;
      const field = target.getBoundingClientRect(), footer = bar.getBoundingClientRect();
      const header = document.querySelector('.topbar')?.getBoundingClientRect();
      const clipped = field.top < 0 || field.bottom > innerHeight;
      const underHeader = header && field.top < header.bottom && field.bottom > header.top;
      const underFooter = field.bottom > footer.top && field.top < footer.bottom;
      if (clipped || underHeader || underFooter) target.scrollIntoView({block:'center',behavior:'auto'});
    }));
  });
  form.addEventListener("submit", () => { submitting = true; });
  window.addEventListener("pageshow", () => { submitting = false; update(); });
  window.addEventListener("beforeunload", event => {
    if (dirty && !submitting) { event.preventDefault(); event.returnValue = ""; }
  });
  // Hash targets can be fields inside Advanced. Reveal before moving focus.
  form.addEventListener("click", event => {
    const link = event.target.closest('a[href^="#"]');
    if (!link) return;
    const target = document.getElementById(link.hash.slice(1));
    if (!target) return;
    const details = target.closest("details");
    if (details) details.open = true;
    if (target.matches("input, select")) target.focus();
  });
  const error = form.querySelector("#form-errors");
  if (error) error.focus();
  update();
}
