// Scoped checkbox groups share one interaction; deletion policy stays on the server.
const groups = [...document.querySelectorAll('[data-choice-group]')];
function sync(group, userChange = false) {
  const items = [...group.querySelectorAll('[data-choice-item]')];
  const master = group.querySelector('[data-choice-all]');
  const selected = items.filter(item => item.checked).length;
  master.indeterminate = selected > 0 && selected < items.length;
  // Keep the explicit all-owner grant on load; do not broaden a node-only default.
  if (userChange) master.checked = selected === items.length && items.length > 0;
  const count = group.querySelector('[data-choice-count]');
  if (count) count.textContent = count.dataset.countFormat.replace('%d', new Intl.NumberFormat(document.documentElement.lang || 'en', {numberingSystem:document.documentElement.dataset.digits==='persian'?'arabext':'latn'}).format(selected));
  const summary = group.querySelector('[data-choice-summary]');
  if (summary) {
    const labels = items.filter(item => item.checked).map(item => item.closest('label')?.querySelector('span')?.textContent.trim() || '');
    summary.textContent = labels.length === 1 ? labels[0] : labels.length ? count.textContent : summary.dataset.empty;
  }
}
function userFields() {
  const users = document.querySelector('[name="kinds"][value="users"]');
  document.querySelectorAll('[data-cleanup-user-fields]').forEach(field => { field.hidden = !users?.checked; });
}
for (const group of groups) {
  sync(group);
  group.addEventListener('change', event => {
    if (event.target.matches('[data-choice-all]')) group.querySelectorAll('[data-choice-item]').forEach(item => { item.checked = event.target.checked; });
    sync(group, true);
    userFields();
  });
}
userFields();
