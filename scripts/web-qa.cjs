// Shared development-only accessibility and browser performance observations.
const AxeBuilder = process.env.WG_TEST_AXE ? require(process.env.WG_TEST_AXE).default : null;
exports.variant = (lang, theme, width) => (!process.env.WG_TEST_UI_LANG || process.env.WG_TEST_UI_LANG===lang) && (!process.env.WG_TEST_UI_THEME || process.env.WG_TEST_UI_THEME===theme) && (!process.env.WG_TEST_UI_WIDTH || Number(process.env.WG_TEST_UI_WIDTH)===width);
exports.install = async context => {
  if (process.env.WG_TEST_UI_PERF_FONT_DELAY) await context.route(/\.woff2(?:\?|$)/, async route => {
    await new Promise(resolve => setTimeout(resolve, Number(process.env.WG_TEST_UI_PERF_FONT_DELAY)));
    await route.continue();
  });
  await context.addInitScript(() => {
  const supported = PerformanceObserver.supportedEntryTypes || [];
  const metrics = window.__wgQA = { cls: supported.includes('layout-shift') ? 0 : null, longTasks: supported.includes('longtask') ? 0 : null, longTaskMS: supported.includes('longtask') ? 0 : null, largestShift: null };
  const rect = value => ({x:value.x,y:value.y,width:value.width,height:value.height});
  if (metrics.cls !== null) new PerformanceObserver(list => { for (const entry of list.getEntries()) if (!entry.hadRecentInput) {
    metrics.cls += entry.value;
    if (!metrics.largestShift || entry.value > metrics.largestShift.value) metrics.largestShift = {value:entry.value,at:entry.startTime,sources:(entry.sources || []).map(source => ({tag:source.node?.nodeName || '',classes:source.node?.classList ? [...source.node.classList].join(' ') : '',previous:rect(source.previousRect),current:rect(source.currentRect)}))};
  } }).observe({type:'layout-shift',buffered:true});
  if (metrics.longTasks !== null) new PerformanceObserver(list => { for (const entry of list.getEntries()) { metrics.longTasks++; metrics.longTaskMS += entry.duration; } }).observe({type:'longtask',buffered:true});
  });
};
exports.measure = async (page, label = '') => {
  await page.evaluate(async () => { await document.fonts.ready; await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))); });
  const result = await page.evaluate(() => {
    const resources=performance.getEntriesByType('resource');
    const nav=performance.getEntriesByType('navigation')[0];
    return {...window.__wgQA, requests:resources.length+1, decodedBytes:resources.reduce((sum,r)=>sum+r.decodedBodySize,0), loadMS:nav?.duration || null};
  });
  if (label) result.cell = label;
  if (process.env.WG_TEST_UI_CHART_TRACE) {
    const chart = await page.evaluate(() => {
      const plot = document.querySelector('.traffic-plot');
      if (!plot) return null;
      const visible = selector => [...plot.querySelectorAll(selector)].filter(el => el.getClientRects().length);
      const x = visible('.traffic-tick-label').map(el => el.getBoundingClientRect());
      const svg = plot.querySelector('svg').getBoundingClientRect();
      return {labelFontPX:[...new Set(visible('.traffic-y-axis > span,.traffic-tick-label').map(el => parseFloat(getComputedStyle(el).fontSize)))],plotWidth:svg.width,plotHeight:svg.height,xLabels:x.length,xOverlap:x.some((rect,i) => i>0 && rect.left<x[i-1].right),plotInsideViewport:svg.left>=0&&svg.right<=innerWidth};
    });
    if (chart) console.log('Chart geometry '+label+': '+JSON.stringify(chart));
  }
  if (process.env.WG_TEST_UI_PERF_TRACE) console.log('Performance probe '+label+': '+JSON.stringify(result));
  return result;
};
exports.scan = async (page, label, fullContrast = true) => {
  if (!AxeBuilder) return;
  let scan = new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21a','wcag21aa','wcag22aa']);
  if (!fullContrast) scan=scan.disableRules(['color-contrast']);
  const result=await scan.analyze();
  if (result.violations.length) {
    // Attribute values, DOM contents and capability URLs never enter diagnostics.
    const violations=result.violations.map(v=>({rule:v.id,impact:v.impact,nodes:v.nodes.length,targets:v.nodes.slice(0,3).map(n=>n.target.map(s=>String(s).replace(/\[[^\]]*\]/g,'[attribute]'))),contrast:v.id==='color-contrast'?v.nodes.map(n=>n.any.map(c=>({ratio:c.data?.contrastRatio,foreground:c.data?.fgColor,background:c.data?.bgColor}))):undefined}));
    console.log('accessibility '+label+': '+JSON.stringify(violations));
    throw new Error('contract: accessibility '+result.violations.map(v=>v.id).join(', '));
  }
};
exports.merge = (summary, m) => {
  if (m.cls !== null && m.cls !== undefined && m.cls > (summary.cls || 0)) {
    summary.clsCell = m.cell || null;
    summary.largestShift = m.largestShift;
  }
  for (const key of ['cls','longTasks','longTaskMS','requests','decodedBytes','loadMS']) {
    if (m[key] !== null && m[key] !== undefined) summary[key]=Math.max(summary[key] || 0,m[key]);
    else if (!(key in summary)) summary[key]=null;
  }
};
