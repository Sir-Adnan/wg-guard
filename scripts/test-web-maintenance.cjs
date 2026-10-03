const fs = require('fs');
const path = require('path');
const assert = require('assert/strict');
const playwright = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const qa = require('./web-qa.cjs');
const seed = JSON.parse(fs.readFileSync(0, 'utf8'));

(async () => {
 const engine = process.env.WG_TEST_BROWSER_ENGINE || 'chromium';
 const browser = await playwright[engine].launch({headless:true,...(engine==='chromium'?{channel:process.env.WG_TEST_BROWSER_CHANNEL||'chrome'}:{})});
 let cells=0;
 try {
  for (const lang of ['fa','en']) for (const theme of ['light','dark']) for (const width of [320,390,768,1440]) {
   if (!qa.variant(lang,theme,width)) continue;
   const context=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'});
   await qa.install(context);
   await context.addCookies([{name:'wg_session',value:seed.session,url:seed.url},{name:'wg_theme',value:theme,url:seed.url}]);
   await context.request.post(seed.url+'/prefs/locale',{form:{locale:lang,_csrf:seed.csrf}});
   const page=await context.newPage();let errors=0;page.on('pageerror',()=>errors++);
   for (const tab of ['overview','versions','operations','recovery']) {
    await page.goto(seed.url+'/updates?tab='+tab);
    await page.locator('[data-maintenance-page]').waitFor();
    await page.evaluate(()=>document.fonts.ready);
    const geometry=await page.evaluate(()=>({overflow:document.documentElement.scrollWidth>innerWidth+1,heading:!!document.querySelector('h1'),badCopy:/updates\.[a-z_]+/.test(document.querySelector('main').innerText)}));
    assert(!geometry.overflow,`overflow ${lang}/${theme}/${width}/${tab}`);
    assert(geometry.heading&&!geometry.badCopy,`copy ${lang}/${theme}/${width}/${tab}`);
    await qa.scan(page,`maintenance-${lang}-${theme}-${width}-${tab}`);
    if (tab==='versions') {
     assert.equal(await page.locator('.release-row').count(),10); if(width<=800) await page.locator('[data-responsive-disclosure] > summary').click();
     await page.locator('[data-density-toggle]').click();
     assert.equal(await page.locator('[data-density-toggle]').getAttribute('aria-pressed'),'true');
     await page.locator('[data-density-toggle]').click();
     await page.locator('.technical-disclosure').first().locator('summary').click();
     assert(await page.locator('.release-notes').isVisible());
     if(process.env.WG_UI_SCREENSHOT_DIR) {
      fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR,{recursive:true});
      await page.evaluate(()=>scrollTo(0,0)); await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`maintenance-${engine}-${lang}-${theme}-${width}.png`),fullPage:true});
     }
    }
   }
   assert.equal(errors,0,'browser runtime errors');
   if(lang==='en'&&theme==='light'&&width===390) {
    await page.goto(seed.url+'/updates?tab=versions&version=v0.1.9');
    await page.locator('[data-responsive-disclosure] > summary').click();
    await page.locator('[data-maintenance-filter] input[name=q]').fill('v0.1.9');
    await page.locator('[data-maintenance-filter] button[type=submit]').click();
    await page.waitForURL(/q=v0\.1\.9/);
    assert.equal(await page.locator('.release-row').count(),1,'filter did not replace the server-rendered list');
    assert.equal(await page.locator('[data-responsive-disclosure]').getAttribute('open'),null,'responsive behavior did not survive the HTMX swap');
    await page.locator('button[name=operation][value=preflight]').click();
    await page.waitForURL(/tab=operations/);
    assert(await page.locator('[data-maintenance-poll]').count(),'operation does not reconnect');
    await context.request.post(seed.url+'/_qa/finish');
    await page.locator('[aria-busy="false"] .maintenance-operation').waitFor({timeout:15000});
    await page.goto(seed.url+'/updates?tab=versions&version=v0.1.9');
    assert(await page.locator('button[name=operation][value=execute]').isEnabled(),'checked destination not actionable');
    await page.locator('button[name=operation][value=execute]').click();
    await page.locator('#confirm-dialog[open]').waitFor();
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#confirm-dialog[open]').count(),0,'confirmation could not be dismissed');
   }
   cells++;await context.close();
  }
 } finally {await browser.close();}
 console.log(`Maintenance browser: ${cells} locale/theme/viewport cells passed (${engine}).`);
})().catch(error=>{console.error(String(error.message).replaceAll(seed.session,'[session]').replaceAll(seed.csrf,'[csrf]'));process.exit(1);});
