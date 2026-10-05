/* Domain form enhancement. Server validation and native multipart forms remain
 * authoritative; without JavaScript each labelled section stays usable. */
const form = document.querySelector('.domain-configure');
if (form) {
  const method = form.querySelector('[data-domain-method]');
  const source = form.querySelector('[data-domain-source]');
  const sync = () => {
    for (const section of form.querySelectorAll('[data-domain-fields]')) {
      const active = section.dataset.domainFields === method.value;
      section.hidden = !active;
      for (const control of section.querySelectorAll('input, select')) control.disabled = !active;
    }
    for (const section of form.querySelectorAll('[data-domain-source-fields]')) {
      const active = method.value === 'manual' && section.dataset.domainSourceFields === source.value;
      section.hidden = !active;
      for (const control of section.querySelectorAll('input')) control.disabled = !active;
    }
  };
  form.addEventListener('change', sync);
  sync();
}
