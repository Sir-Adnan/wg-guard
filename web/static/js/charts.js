/* Lightweight chart inspection for SSR SVGs. Exact values remain available
 * in native tables; this layer adds pointer, touch and keyboard discovery. */
let chartSequence = 0;

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
  host.append(tooltip);
  svg.setAttribute('aria-describedby', tooltip.id);

  const cursor = svg.querySelector('[data-chart-cursor]');
  const markers = [...svg.querySelectorAll('[data-chart-marker]')];
  let selected = points.length - 1;
  let keyboardActive = false;

  const hide = () => {
    host.classList.remove('is-inspecting');
    tooltip.classList.remove('is-visible');
    tooltip.setAttribute('aria-hidden', 'true');
  };

  const show = index => {
    selected = Math.max(0, Math.min(points.length - 1, index));
    const point = points[selected];
    const view = svg.viewBox.baseVal;
    const svgRect = svg.getBoundingClientRect();
    const hostRect = host.getBoundingClientRect();
    const x = svgRect.left - hostRect.left + point.x / view.width * svgRect.width;
    const availableY = (point.y || []).filter(value => typeof value === 'number');
    const yValue = availableY.length ? Math.min(...availableY) : view.height / 2;
    const y = svgRect.top - hostRect.top + yValue / view.height * svgRect.height;

    tooltip.textContent = point.title || '';
    tooltip.style.left = `${x}px`;
    tooltip.style.top = `${Math.max(8, y)}px`;
    tooltip.classList.add('is-visible');
    tooltip.setAttribute('aria-hidden', 'false');
    host.classList.add('is-inspecting');
    const halfWidth = tooltip.offsetWidth / 2;
    const safeX = Math.max(halfWidth + 4, Math.min(hostRect.width - halfWidth - 4, x));
    tooltip.style.left = `${safeX}px`;
    tooltip.style.top = `${Math.max(tooltip.offsetHeight + 18, y)}px`;

    if (cursor) {
      cursor.setAttribute('x1', String(point.x));
      cursor.setAttribute('x2', String(point.x));
    }
    markers.forEach((marker, markerIndex) => {
      const markerY = point.y?.[markerIndex];
      const visible = typeof markerY === 'number';
      marker.setAttribute('visibility', visible ? 'visible' : 'hidden');
      if (visible) {
        marker.setAttribute('cx', String(point.x));
        marker.setAttribute('cy', String(markerY));
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
    keyboardActive = false;
    show(indexAtPointer(event));
  });
  svg.addEventListener('pointerdown', event => {
    keyboardActive = false;
    show(indexAtPointer(event));
  });
  svg.addEventListener('pointerleave', () => {
    if (!keyboardActive) hide();
  });
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
document.body.addEventListener('htmx:afterSwap', () => bindCharts(document));
document.body.addEventListener('htmx:afterSettle', () => bindCharts(document));
