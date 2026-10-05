/* Lightweight chart inspection for SSR SVGs. Exact values remain available
 * in native tables; this layer adds pointer, touch and keyboard discovery. */
let chartSequence = 0;
let activeChart = null;
const { displayDigits } = await import(document.querySelector('meta[name="ui-presentation-module"]').content);

function bindChart(svg) {
  if (svg.dataset.chartBound === '1') return;
  let points;
  try {
    points = JSON.parse(svg.dataset.chartPoints || '[]');
  } catch {
    return;
  }
  if (!Array.isArray(points) || points.length === 0) return;

  const host = svg.closest('.interactive-chart') || svg.parentElement;
  if (!host) return;
  svg.dataset.chartBound = '1';
  host.classList.add('interactive-chart');

  const tooltip = document.createElement('div');
  tooltip.className = 'chart-tooltip';
  tooltip.id = `chart-tooltip-${++chartSequence}`;
  tooltip.setAttribute('role', 'status');
  tooltip.setAttribute('aria-live', 'polite');
  tooltip.setAttribute('aria-hidden', 'true');
  if (typeof tooltip.showPopover === 'function') tooltip.setAttribute('popover','manual');
  host.append(tooltip);
  svg.setAttribute('aria-describedby', tooltip.id);

  const cursor = svg.querySelector('[data-chart-cursor]');
  const markers = [...svg.querySelectorAll('[data-chart-marker]')].map((marker, index) => {
    marker.remove();
    const dot = document.createElement('span'); dot.className = 'chart-dot chart-dot--' + (index === 0 ? 'primary' : 'secondary');
    dot.hidden = true; dot.setAttribute('aria-hidden','true'); host.append(dot); return dot;
  });
  let selected = points.length - 1;
  let keyboardActive = false;
  let pinned = false;
  let closeTimer = null;

  const hide = () => {
    clearTimeout(closeTimer);
    if (typeof tooltip.hidePopover === 'function' && tooltip.matches(':popover-open')) tooltip.hidePopover();
    host.classList.remove('is-inspecting');
    tooltip.classList.remove('is-visible');
    tooltip.setAttribute('aria-hidden', 'true');
    markers.forEach(marker => { marker.hidden = true; });
    pinned = false;
    if (activeChart?.svg === svg) activeChart = null;
  };

  const show = (index, pointer = null) => {
    clearTimeout(closeTimer);
    if (activeChart?.svg !== svg) { activeChart?.hide(); activeChart = {svg, host, hide}; }
    if (selected === index && tooltip.classList.contains('is-visible')) return;
    selected = Math.max(0, Math.min(points.length - 1, index));
    const point = points[selected];
    const view = svg.viewBox.baseVal;
    const svgRect = svg.getBoundingClientRect();
    const hostRect = host.getBoundingClientRect();
    const x = svgRect.left - hostRect.left + point.x / view.width * svgRect.width;
    const availableY = (point.y || []).filter(value => typeof value === 'number');
    const yValue = availableY.length ? Math.min(...availableY) : view.height / 2;
    const y = svgRect.top - hostRect.top + yValue / view.height * svgRect.height;

    tooltip.replaceChildren();
    const title = document.createElement('bdi'); title.className = 'chart-tooltip-title'; title.dir = 'ltr';
    title.textContent = displayDigits(point.label || point.title || ''); tooltip.append(title);
    for (const series of point.series || []) {
      const row = document.createElement('div'); row.className = 'chart-tooltip-row';
      const name = document.createElement('span'); name.className = 'chart-tooltip-name';
      const swatch = document.createElement('span'); swatch.className = 'chart-tooltip-swatch' + (series.index === 0 ? '' : ' chart-tooltip-swatch--secondary');
      swatch.setAttribute('aria-hidden','true'); name.append(swatch, document.createTextNode(series.name));
      const value = document.createElement('bdi'); value.className = 'chart-tooltip-value'; value.textContent = displayDigits(series.value);
      row.append(name, value); tooltip.append(row);
    }
    tooltip.classList.add('is-visible');
    tooltip.setAttribute('aria-hidden', 'false');
    if (typeof tooltip.showPopover === 'function' && !tooltip.matches(':popover-open')) tooltip.showPopover();
    host.classList.add('is-inspecting');
    const width = tooltip.offsetWidth;
    const anchorX = pointer?.clientX ?? hostRect.left + x;
    const anchorY = pointer?.clientY ?? hostRect.top + y;
    const safeX = Math.max(8, Math.min(innerWidth - width - 8, anchorX + width + 24 <= innerWidth - 8 ? anchorX + 24 : anchorX - width - 24));
    tooltip.style.left = `${safeX}px`;
    const height = tooltip.offsetHeight;
    const top = anchorY - height - 18 >= 8 ? anchorY - height - 18 : anchorY + 18;
    tooltip.style.top = `${Math.max(8, Math.min(innerHeight - height - 8, top))}px`;

    if (cursor) {
      cursor.setAttribute('x1', String(point.x));
      cursor.setAttribute('x2', String(point.x));
    }
    markers.forEach((marker, markerIndex) => {
      const markerY = point.y?.[markerIndex];
      const visible = typeof markerY === 'number';
      marker.hidden = !visible;
      if (visible) {
        marker.style.left = `${x}px`;
        marker.style.top = `${svgRect.top - hostRect.top + markerY / view.height * svgRect.height}px`;
      }
    });
  };

  const indexAtPointer = event => {
    const rect = svg.getBoundingClientRect();
    const viewWidth = svg.viewBox.baseVal.width;
    const x = Math.max(0, Math.min(viewWidth, (event.clientX - rect.left) / rect.width * viewWidth));
    let nearest = 0;
    for (let index = 1; index < points.length; index++) {
      if (Math.abs(points[index].x - x) < Math.abs(points[nearest].x - x)) nearest = index;
    }
    return nearest;
  };

  svg.addEventListener('pointermove', event => {
    if (event.pointerType !== 'mouse') return;
    keyboardActive = false;
    show(indexAtPointer(event), event);
  });
  svg.addEventListener('pointerdown', event => {
    keyboardActive = false;
    if (pinned && selected === indexAtPointer(event)) { hide(); return; }
    show(indexAtPointer(event), event);
    pinned = true;
  });
  const scheduleClose = () => { if (!keyboardActive && !pinned) closeTimer = setTimeout(hide, 200); };
  svg.addEventListener('pointerleave', scheduleClose);
  tooltip.addEventListener('pointerenter', () => clearTimeout(closeTimer));
  tooltip.addEventListener('pointerleave', scheduleClose);
  svg.addEventListener('focus', () => {
    keyboardActive = true;
    show(selected);
  });
  svg.addEventListener('blur', () => {
    keyboardActive = false;
    hide();
  });
  svg.addEventListener('keydown', event => {
    let next = selected;
    if (event.key === 'ArrowLeft') next--;
    else if (event.key === 'ArrowRight') next++;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = points.length - 1;
    else if (event.key === 'Escape') {
      hide();
      event.preventDefault();
      return;
    } else return;
    event.preventDefault();
    keyboardActive = true;
    show(next);
  });
}

function bindCharts(root = document) {
  root.querySelectorAll?.('[data-chart-interactive]').forEach(bindChart);
  if (root.matches?.('[data-chart-interactive]')) bindChart(root);
}

bindCharts();
document.addEventListener('pointerdown', event => { if (activeChart && !activeChart.host.contains(event.target)) activeChart.hide(); }, true);
window.addEventListener('resize', () => activeChart?.hide());
window.addEventListener('scroll', () => activeChart?.hide(), true);
document.body.addEventListener('htmx:beforeSwap', () => activeChart?.hide());
document.body.addEventListener('htmx:afterSwap', () => bindCharts(document));
document.body.addEventListener('htmx:afterSettle', () => bindCharts(document));
