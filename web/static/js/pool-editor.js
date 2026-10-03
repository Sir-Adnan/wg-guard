const { displayDigits } = await import(document.querySelector('meta[name="ui-module"]').content);
for (const editor of document.querySelectorAll('[data-pool-editor]')) {
  const form = editor.closest('form');
  const primary = editor.querySelector('[name="subnet"]');
  const extras = editor.querySelector('[name="extra_pools"]');
  const size = editor.querySelector('[data-pool-size]');
  const picker = editor.querySelector('[data-pool-preset]');
  const extraPicker = editor.querySelector('[data-pool-extra-preset]');
  picker.disabled = Boolean(editor.dataset.poolExclude);
  const status = editor.querySelector('[data-pool-status]');
  let timer = null, request = null, choices = [];
  function mode() {
    const automatic = form.querySelector('[name="pool_mode"]:checked')?.value === 'automatic';
    primary.readOnly = Boolean(editor.dataset.poolExclude) || automatic;
    if (automatic) primary.value = '';
    editor.querySelector('[data-pool-automatic]').hidden = !automatic;
    editor.querySelector('[data-pool-picker]').hidden = automatic && !extras.value.trim();
  }
  async function refresh() {
    request?.abort(); request = new AbortController();
    const query = new URLSearchParams({name:form.querySelector('[name="name"]').value, prefix:size.value, exclude:editor.dataset.poolExclude});
    try {
      const response = await fetch('/interfaces/pools/suggestions?' + query, {signal:request.signal,headers:{Accept:'application/json'}});
      if (!response.ok) throw new Error('unavailable');
      const result = await response.json(); choices = result.choices;
      editor.querySelector('[data-pool-proposed]').textContent = result.automatic || '—';
      picker.replaceChildren(new Option(editor.dataset.poolAuto, ''));
      extraPicker.replaceChildren(new Option(editor.dataset.poolAuto, ''));
      for (const choice of choices) {
        const label = choice.cidr + ' · ' + displayDigits(choice.capacity) + ' · ' + (choice.status === 'available' ? editor.dataset.poolReady : choice.status === 'full' ? editor.dataset.poolFull : choice.status === 'current' ? editor.dataset.poolCurrent : editor.dataset.poolBlocked) + (choice.interface ? ' (' + choice.interface + ')' : '');
        const option = new Option(label, choice.cidr); option.disabled = choice.status !== 'available'; picker.append(option);
        const extraOption = new Option(label, choice.cidr); extraOption.disabled = choice.status !== 'available'; extraPicker.append(extraOption);
      }
      status.textContent = '';
    } catch(error) { if (error.name !== 'AbortError') status.textContent = editor.dataset.poolFailed; }
  }
  size.addEventListener('change', refresh);
  form.querySelector('[name="name"]').addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(refresh, 350); });
  form.querySelectorAll('[name="pool_mode"]').forEach(input => input.addEventListener('change', mode));
  picker.addEventListener('change', () => {
    if (!picker.value || editor.dataset.poolExclude) return;
    const manual = form.querySelector('[name="pool_mode"][value="custom"]'); if (manual) manual.checked = true;
    mode(); primary.value = picker.value; primary.dispatchEvent(new Event('input', {bubbles:true}));
  });
  editor.querySelector('[data-pool-add]').addEventListener('click', () => {
    if (!extraPicker.value) { extraPicker.focus(); return; }
    const existing = extras.value.split(/[\s,]+/).filter(Boolean);
    const proposed = primary.value || editor.querySelector('[data-pool-proposed]').textContent;
    if (!proposed || proposed === '—') { status.textContent = editor.dataset.poolFailed; return; }
    if (existing.includes(extraPicker.value) || proposed === extraPicker.value) { status.textContent = editor.dataset.poolBlocked; return; }
    existing.push(extraPicker.value); extras.value = existing.join('\n');
    const manual = form.querySelector('[name="pool_mode"][value="custom"]'); if (manual) manual.checked = true;
    mode(); if (!primary.value) primary.value = proposed;
  });
  mode(); refresh();
}
