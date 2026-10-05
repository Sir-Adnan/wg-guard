// Opt-in regression checks against real rendered forms and a disposable DB.
const fs = require('node:fs'), path = require('node:path'), assert = require('node:assert/strict');
const pw = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const qa = require('./web-qa.cjs');
const seed = JSON.parse(fs.readFileSync(0, 'utf8'));

async function guidanceGeometry(page, scope, cell) {
  const faults = await page.locator(scope).evaluate(root => {
    const visible = el => el && el.getClientRects().length > 0;
    const faults = [];
    if (root.querySelector('.field-label-row > .feature-toggle')) faults.push('checkbox card mistaken for section heading');
    for (const trigger of root.querySelectorAll('.field-help-trigger')) {
      if (!visible(trigger)) continue;
      const anchor = trigger.closest('.field-label-row,legend,.form-section-head');
      const title = anchor?.querySelector('label,h2,h3') || (anchor?.tagName === 'LEGEND' ? anchor : null);
      if (!title) { faults.push('unanchored help'); continue; }
      const a = title.getBoundingClientRect(), b = trigger.getBoundingClientRect();
      const gap = Math.max(b.left - a.right, a.left - b.right, 0);
      if (gap > 20 || b.top >= a.bottom || a.top >= b.bottom) faults.push('help separated from title');
      if (b.width < 44 || b.height < 44) faults.push('help touch target');
      if (!document.getElementById(trigger.getAttribute('aria-controls'))) faults.push('missing description');
    }
    for (const row of root.querySelectorAll('.field-row')) {
      const inputs = [...row.children].map(field => field.querySelector('input:not([type=hidden]),select')).filter(visible);
      if (inputs.length !== 2) continue;
      const [a,b] = inputs.map(el => el.getBoundingClientRect());
      if (a.right <= b.left || b.right <= a.left) {
        if (Math.abs(a.top - b.top) > 1) faults.push('paired input vertical misalignment');
      }
    }
    return faults;
  });
  assert.deepEqual(faults, [], `form geometry ${cell}: ${faults.join(', ')}`);
}

async function helpInteractions(page, selector, cell) {
  const button = page.locator(selector).first();
  await button.scrollIntoViewIfNeeded();
  await button.hover();
  await page.locator('.field-help-content:popover-open').waitFor();
  await button.click();
  const rect = await page.locator('.field-help-content:popover-open').boundingBox();
  const viewport = page.viewportSize();
  assert(rect.x >= 0 && rect.y >= 0 && rect.x + rect.width <= viewport.width + 1 && rect.y + rect.height <= viewport.height + 1, `help outside viewport ${cell}`);
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('.field-help-content:popover-open').count(), 0, `Escape ${cell}`);
  assert(await button.evaluate(el => document.activeElement === el), `return focus ${cell}`);
  await page.keyboard.press('Enter');
  await page.locator('.field-help-content:popover-open').waitFor();
  await page.locator('h1').click();
  assert.equal(await page.locator('.field-help-content:popover-open').count(), 0, `outside dismissal ${cell}`);
}

(async () => {
  const engine = process.env.WG_TEST_BROWSER_ENGINE || 'chromium';
  const browser = await pw[engine].launch({headless:true, ...(engine === 'chromium' ? {channel:'chrome'} : {})});
  let cells = 0;
  try {
    for (const [lang,theme,width] of [['fa','dark',1440],['en','light',1440],['fa','light',768],['en','dark',390],['fa','dark',320]]) {
      if (!qa.variant(lang,theme,width)) continue;
      const context = await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'});
      await qa.install(context);
      await context.addCookies([{name:'wg_session',value:seed.session,url:seed.url},{name:'wg_theme',value:theme,url:seed.url}]);
      await context.request.post(seed.url+'/prefs/locale',{form:{locale:lang,_csrf:seed.csrf}});
      const page = await context.newPage(); let runtimeErrors = 0;
      page.on('pageerror', () => runtimeErrors++);
      const cell = `${engine}/${lang}/${theme}/${width}`;
      for (const route of ['/users/new','/templates/new',`/interfaces/${seed.iface}/edit`,'/backups','/updates','/dashboard','/appearance','/settings/domains']) {
        await page.goto(seed.url+route);
        await page.waitForFunction(() => document.documentElement.dataset.ui === 'ready');
        await page.evaluate(() => document.fonts.ready);
        const helps = await page.locator('.field-help-trigger').count();
        await page.evaluate(() => document.body.dispatchEvent(new CustomEvent('htmx:afterSwap',{detail:{target:document.querySelector('main')}})));
        assert.equal(await page.locator('.field-help-trigger').count(),helps,`duplicate help after swap ${cell}`);
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth+1), `overflow ${cell}/${route.split('/')[1]}`);
        await guidanceGeometry(page,'main',cell);
        await qa.scan(page,`guidance-${cell}-${route.split('/')[1]}`);
        if (route === '/templates/new') await helpInteractions(page,'legend .field-help-trigger',cell);
        if (route === '/backups') {
          const layout = await page.locator('#new-backup').evaluate(card => {
            const rect = card.getBoundingClientRect(), workspace = card.closest('.backup-workspace').getBoundingClientRect();
            const header = card.querySelector('.section-head').getBoundingClientRect(), form = card.querySelector('form').getBoundingClientRect();
            return {width:rect.width,workspace:workspace.width,headerBottom:header.bottom,formTop:form.top,sideBySide:header.right <= form.left || form.right <= header.left};
          });
          assert(Math.abs(layout.width-layout.workspace) <= 1, `backup leaves half workspace blank ${cell}`);
          assert(width >= 1440 ? layout.sideBySide : layout.formTop >= layout.headerBottom, `backup composition ${cell}`);
          if (width >= 1440) assert((await page.locator('#new-backup').boundingBox()).height < 180,`backup wastes vertical space ${cell}`);
        }
        if (route === '/users/new') { await creationModes(page,'main',cell); await presetLayout(page,'main',cell); }
        if (route === '/dashboard') await chartInspection(page,cell);
        if (route === '/appearance') {
          assert(await page.locator('#appearance-style').isVisible(),'appearance style tab absent '+cell);
          await page.locator('.visual-preset-choice[data-choice="claude-plus"]').click();
          assert.equal(await page.locator('[data-appearance-name]').textContent(),'Claude +','live appearance title '+cell);
          await page.locator('.appearance-default-disclosure > summary').click();
          assert(await page.locator('#appearance-panel-mode').isVisible(),'panel defaults not reachable '+cell);
          await page.locator('#appearance-numbers-tab').click();
          assert(await page.locator('#personal-digits').isVisible() && !await page.locator('#appearance-style').isVisible(),'numeral tab isolation '+cell);
          await page.locator('#appearance-style-tab').click();
          if (process.env.WG_UI_SCREENSHOT_DIR) await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`appearance-redesign-${cell.replaceAll('/','-')}.png`),fullPage:true});
        }
        if (route === '/settings/domains') {
          assert.equal(await page.locator('.nav a[aria-current="page"]').count(),1,'domains current navigation duplicated '+cell);
          assert.equal(await page.locator('.nav a[aria-current="page"]').getAttribute('href'),'/settings/domains','domains navigation missing '+cell);
          if (width >= 1440) {
            const group=page.locator('.nav-group').first(); await group.locator('summary').click();
            await page.locator('[data-toggle-rail]').click();
            assert(await page.locator('.nav a[href="/users"]').isVisible(),'collapsed group hides rail destinations '+cell);
            await page.locator('[data-toggle-rail]').click();
            assert(!await group.evaluate(el=>el.open),'expanded rail lost disclosure choice '+cell);
          }
        }
      }
      await page.goto(seed.url+'/interfaces');
      await page.evaluate(() => document.fonts.ready);
      const pool = await page.locator('.iface-network').first().evaluate(network => {
        const address = network.querySelector('bdi'), description = network.querySelector('.cell-sub');
        return {addressBottom:address.getBoundingClientRect().bottom,descriptionTop:description.getBoundingClientRect().top,direction:getComputedStyle(address).direction,address:address.textContent};
      });
      assert(pool.descriptionTop >= pool.addressBottom && pool.direction === 'ltr' && /^[0-9./]+$/.test(pool.address), `CIDR/capacity collision ${cell}`);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth+1), `interface collection overflow ${cell}`);
      await qa.scan(page,`interface-collection-${cell}`);
      if (process.env.WG_UI_SCREENSHOT_DIR) {
        fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR,{recursive:true});
        await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`interface-list-${engine}-${lang}-${width}.png`),fullPage:true});
      }
      await page.goto(seed.url+'/users');
      await page.locator('[data-open-modal="create-drawer"]').click();
      await page.locator('#create-drawer[open]').waitFor();
      await creationModes(page,'#create-drawer',`drawer/${cell}`);
      await presetLayout(page,'#create-drawer',`drawer/${cell}`);
      await guidanceGeometry(page,'#create-drawer',`drawer/${cell}`);
      const accountHelp = page.locator('#create-drawer #user-account .form-section-head .field-help-trigger');
      await accountHelp.click();
      await page.locator('.field-help-content:popover-open').waitFor();
      await page.keyboard.press('Escape');
      assert(await page.locator('#create-drawer').evaluate(el => el.open), `help Escape closed drawer ${cell}`);
      await qa.scan(page,`create-drawer-${cell}`);
      if (process.env.WG_UI_SCREENSHOT_DIR) {
        await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`user-drawer-${engine}-${lang}-${width}.png`),fullPage:true});
        await page.goto(seed.url+'/backups');
        await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`backup-create-${engine}-${lang}-${width}.png`),fullPage:true});
      }
      // A hint containing the server error must not disappear into hidden help.
      await page.goto(seed.url+'/users/new');
      await page.locator('#u-username').fill('x');
      await Promise.all([page.waitForNavigation(),page.locator('.form-actions button[type="submit"]').click()]);
      assert(await page.locator('#username-hint .field-error').isVisible(), `validation hidden ${cell}`);
      assert.equal(await page.locator('.field-help-content .field-error').count(),0,`error became help ${cell}`);
      assert.equal(runtimeErrors,0,`browser runtime error ${cell}`);
      cells++; await context.close();
    }
    for (const [lang,width] of [['fa',390],['en',1440]]) {
      const context = await browser.newContext({viewport:{width,height:900},javaScriptEnabled:false});
      await context.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page = await context.newPage();
      await page.goto(seed.url+'/users/new?lang='+lang);
      assert(await page.locator('[data-fill-preset]').first().isHidden(),'inert native quick-fill shown');
      assert(await page.locator('#u-traffic').isVisible(),'native manual quota inaccessible');
      await page.goto(seed.url+'/templates/new?lang='+lang);
      assert(await page.locator('form .section-description').first().isVisible(),'native instructions absent');
      await page.goto(seed.url+'/backups?lang='+lang);
      assert(await page.locator('#b-password-help').isVisible(),'native backup instructions absent');
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth+1),'native backup overflow');
      await context.close();
    }
    // A browser lacking Popover still gets positioned, touch-operable controls.
    const touch = await browser.newContext({viewport:{width:390,height:800},hasTouch:true});
    await touch.addInitScript(() => Object.defineProperty(HTMLElement.prototype,'showPopover',{value:undefined,configurable:true}));
    await touch.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
    const page = await touch.newPage(); await page.goto(seed.url+'/templates/new?lang=fa');
    await page.locator('legend .field-help-trigger').first().tap();
    const help = page.locator('.field-help-content:not([hidden])');
    assert(await help.isVisible(),'touch fallback did not open');
    const rect = await help.boundingBox();
    assert(rect.x >= 0 && rect.y >= 0 && rect.x+rect.width <= 391 && rect.y+rect.height <= 801,'touch fallback outside viewport');
    await page.locator('legend .field-help-trigger').first().tap();
    assert.equal(await page.locator('.field-help-content:not([hidden])').count(),0,'touch fallback did not close');
    await page.goto(seed.url+'/users?lang=fa');
    await page.locator('[data-open-modal="create-drawer"]').tap();
    const quick=page.locator('#create-drawer #user-limits .select-trigger');
    await quick.tap();
    const options=page.locator('#'+await quick.getAttribute('aria-controls'));
    assert(await options.isVisible(),'touch select fallback did not open');
    const optionBox=await options.boundingBox();
    assert(optionBox.x>=0&&optionBox.y>=0&&optionBox.x+optionBox.width<=391&&optionBox.y+optionBox.height<=801,'touch select fallback outside viewport '+JSON.stringify(optionBox));
    await options.getByRole('option',{name:'100 GB',exact:true}).tap();
    assert.equal(await page.locator('#create-drawer #u-traffic').inputValue(),'100','touch select did not fill quota');
    assert(await page.locator('#create-drawer').evaluate(el=>el.open),'touch select closed drawer');
    await page.goto(seed.url+'/dashboard?lang=fa');
    const chart = page.locator('.resource-card .sparkline').first(); await chart.scrollIntoViewIfNeeded();
    const chartBox = await chart.boundingBox();
    await chart.tap({position:{x:chartBox.width/3,y:chartBox.height/2}});
    const tooltip = page.locator('.chart-tooltip.is-visible'); await tooltip.waitFor();
    const tooltipBox = await tooltip.boundingBox();
    assert(tooltipBox.x >= 0 && tooltipBox.y >= 0 && tooltipBox.x+tooltipBox.width <= 391 && tooltipBox.y+tooltipBox.height <= 801,'touch chart inspector outside viewport');
    await page.locator('h1').tap();
    assert.equal(await page.locator('.chart-tooltip.is-visible').count(),0,'touch chart outside dismissal');
    await touch.close();
  } finally { await browser.close(); }
  console.log(`Guidance layout: ${cells} representative cells, 2 native cells and 1 touch/Popover fallback cell passed (${engine}).`);
})().catch(error => {console.error(String(error.message).replaceAll(seed.session,'[session]').replaceAll(seed.csrf,'[csrf]').replaceAll(seed.iface,'[iface]').replaceAll(seed.template,'[template]'));process.exit(1);});

async function creationModes(page, scope, cell) {
  const root = page.locator(scope), quota = root.locator('[name="traffic_limit_value"]');
  await root.locator('[name="username"]').fill('retained'); await quota.fill('70');
  await root.locator('[data-user-create-tab="template"]').click();
  assert(!await root.locator('#user-limits').isVisible(),'manual limits remain in template mode '+cell);
  await root.locator('[data-user-template]').selectOption(seed.template);
  assert(await quota.isDisabled(),'hidden manual limits submitted '+cell);
  assert(await root.locator('#user-limits .select-trigger').isDisabled(),'inactive preset trigger remains enabled '+cell);
  assert(await root.locator('[data-user-template-preview]').isVisible(),'template preview absent '+cell);
  if (process.env.WG_UI_SCREENSHOT_DIR && cell.includes('/fa/dark/1440')) await root.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`user-template-${cell.replaceAll('/','-')}.png`)});
  await root.locator('[data-user-create-tab="template"]').focus(); await page.keyboard.press('Home');
  assert(await root.locator('#user-limits').isVisible(),'keyboard did not select Standard '+cell);
  assert.equal(await quota.inputValue(),'70','manual quota lost while switching '+cell);
  assert(await root.locator('#user-limits .select-trigger').isEnabled(),'manual preset remains disabled '+cell);
  assert.equal(await root.locator('[name="username"]').inputValue(),'retained','shared account lost '+cell);
  assert(await root.locator('[data-user-template]').isDisabled(),'hidden template overrides Standard '+cell);
}

async function presetLayout(page,scope,cell) {
  const root=page.locator(scope), groups=root.locator('.unit-group .select-trigger');
  assert.equal(await groups.count(),2,'duplicate or missing preset enhancement '+cell);
  const geometry=await groups.evaluateAll(nodes=>nodes.map(group=>{
    const outer=group.getBoundingClientRect(); return {overflow:group.scrollWidth>group.clientWidth+1,inside:outer.width>=44&&outer.height>=44};
  }));
  assert(geometry.every(g=>!g.overflow&&g.inside),'preset clipping or undersized targets '+cell);
  const quota=root.locator('#u-traffic');
  const trigger=root.locator('#user-limits .select-trigger');
  const list=page.locator('#'+await trigger.getAttribute('aria-controls'));
  await trigger.click();
  const box=await list.boundingBox(), viewport=page.viewportSize();
  assert(box.x>=0&&box.y>=0&&box.x+box.width<=viewport.width+1&&box.y+box.height<=viewport.height+1,'select list outside viewport '+cell);
  if (process.env.WG_UI_SCREENSHOT_DIR && cell.includes('/fa/dark/')) await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`compact-select-${cell.replaceAll('/','-')}.png`)});
  await list.getByRole('option',{name:'100 GB',exact:true}).click();
  assert.equal(await quota.inputValue(),'100','quota preset value '+cell);
  assert.equal(await root.locator('#user-limits [data-fill-preset]').inputValue(),'gb:100','quota selected state '+cell);
  assert.equal(await trigger.locator('.select-value').textContent(),'100 GB','trigger does not reflect quota '+cell);
  assert.equal(await trigger.getAttribute('aria-expanded'),'false','selection did not dismiss '+cell);
  await trigger.press('Enter'); await page.keyboard.press('Escape');
  assert(await trigger.evaluate(el=>document.activeElement===el),'select Escape focus return '+cell+' '+JSON.stringify(await trigger.evaluate(el=>({open:el.closest('dialog')?.open,expanded:el.getAttribute('aria-expanded'),focus:document.activeElement?.id,tag:document.activeElement?.tagName,classes:document.activeElement?.className,controls:document.activeElement?.getAttribute('aria-controls')}))));
  if (scope==='#create-drawer') assert(await root.evaluate(el=>el.open),'select Escape closed drawer '+cell);
  await trigger.press('Home'); await page.keyboard.press('Enter');
  assert.equal(await quota.inputValue(),'20','select Home did not skip placeholder '+cell);
  await trigger.press('1'); await page.keyboard.press('0'); await page.keyboard.press('0'); await page.keyboard.press('Enter');
  assert.equal(await quota.inputValue(),'100','select typeahead '+cell);
  await trigger.press('End');
  assert(await list.isVisible(),'End lost scrollable select '+cell);
  const highlighted=list.locator('[data-highlighted]');
  const bottom=await highlighted.boundingBox(), scroller=await list.boundingBox();
  assert(bottom.y>=scroller.y&&bottom.y+bottom.height<=scroller.y+scroller.height+1,'last preset cannot be reached '+cell);
  await page.keyboard.press('Escape');
  await quota.fill('101');
  assert.equal(await root.locator('#user-limits [data-fill-preset]').inputValue(),'','manual input left stale preset '+cell);
  await trigger.click(); await quota.click();
  assert.equal(await trigger.getAttribute('aria-expanded'),'false','select outside dismissal '+cell);
  const durationTrigger=root.locator('#user-timing .select-trigger');
  await durationTrigger.click();
  const duration=page.locator('#'+await durationTrigger.getAttribute('aria-controls'));
  await duration.getByRole('option').filter({hasText:/^3 /}).click();
  assert.equal(await root.locator('[name="duration_unit"]').inputValue(),'months','duration preset unit '+cell);
  assert.equal(await root.locator('[name="duration_value"]').inputValue(),'3','duration preset value '+cell);
}

async function chartInspection(page, cell) {
  for (const selector of ['.resource-card .sparkline','#chart-card .chart']) {
    const chart = page.locator(selector).first(); await chart.scrollIntoViewIfNeeded();
    const bounds = await chart.boundingBox(); await chart.hover({position:{x:bounds.width/2,y:bounds.height/2}});
    const host = chart.locator('..'), tooltip = host.locator('.chart-tooltip.is-visible');
    await tooltip.waitFor();
    await tooltip.hover(); await page.waitForTimeout(250);
    assert(await tooltip.isVisible(),'tooltip cannot be hovered '+cell);
    assert(await tooltip.locator('.chart-tooltip-row').count() > 0,'unstructured tooltip '+cell);
    const box = await tooltip.boundingBox();
    assert(box.x >= 0 && box.y >= 0 && box.x+box.width <= page.viewportSize().width+1,'tooltip overflow '+cell);
    const dots = await host.locator('.chart-dot:not([hidden])').evaluateAll(nodes => nodes.map(n => {const b=n.getBoundingClientRect(); return Math.abs(b.width-b.height) < 1;}));
    assert(dots.every(Boolean),'elliptical markers '+cell);
    if (process.env.WG_UI_SCREENSHOT_DIR) await tooltip.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`tooltip-${selector.includes('sparkline')?'live':'traffic'}-${cell.replaceAll('/','-')}.png`)});
    await chart.focus(); await page.keyboard.press('Home');
    assert(await tooltip.isVisible(),'chart keyboard inspection absent '+cell);
    await page.keyboard.press('Escape'); assert.equal(await host.locator('.chart-tooltip.is-visible').count(),0,'chart Escape '+cell);
  }
  assert.equal(await page.locator('.resource-gauge').count(),3,'CPU/memory/disk radial gauges '+cell);
  if (process.env.WG_UI_SCREENSHOT_DIR) await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`charts-${cell.replaceAll('/','-')}.png`),fullPage:true});
}
