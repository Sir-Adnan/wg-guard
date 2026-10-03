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
   const page=await context.newPage(); let errors=0; page.on('pageerror',()=>errors++);
   for (const route of ['/cleanup','/interfaces','/interfaces/'+seed.interface+'/edit']) {
    await page.goto(seed.url+route);
    await page.locator('h1').waitFor(); await page.evaluate(()=>document.fonts.ready);
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`overflow ${lang}/${theme}/${width}/${route}`);
    await qa.scan(page,`cleanup-${lang}-${theme}-${width}-${route.replaceAll('/','-')}`);
   }
   await page.goto(seed.url+'/cleanup');
   await page.locator('#cleanup-before').fill('2026-03-15');
   await page.locator('[data-calendar="#cleanup-before"]').click();
   await page.locator('#date-calendar.is-open').waitFor();
   assert(await page.locator('#date-calendar [data-cal-prev]').isEnabled(),'historical month navigation disabled');
   await page.locator('#date-calendar [data-cal-prev]').click();
   assert(await page.locator('#date-calendar.is-open').count(),'historical navigation closed calendar');
   await page.locator('[data-cal-day="10"]').click();
   assert.match(await page.locator('#cleanup-before').inputValue(),/^2026-/,'historical date not selected');
   await page.locator('#cleanup-before').fill('');
   await page.locator('form[action="/cleanup/preview"] button[type=submit]').click();
   await page.waitForURL(/\/cleanup\/preview$/);
   await page.locator('.cleanup-preview').waitFor();
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`preview overflow ${lang}/${theme}/${width}`);
   await qa.scan(page,`cleanup-preview-${lang}-${theme}-${width}`);
   if (process.env.WG_UI_SCREENSHOT_DIR) {
    fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR,{recursive:true});
    await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`cleanup-${engine}-${lang}-${theme}-${width}.png`),fullPage:true});
   }
   await page.locator('form[action="/cleanup/execute"] button').click();
   await page.locator('#confirm-dialog[open]').waitFor();
   await page.keyboard.press('Escape');
   assert.equal(await page.locator('#confirm-dialog[open]').count(),0,'confirmation could not be dismissed');
   assert.equal(errors,0,'browser runtime errors');
   cells++; await context.close();
  }
 } finally {await browser.close();}
 console.log(`Cleanup browser: ${cells} locale/theme/viewport cells passed (${engine}).`);
})().catch(error=>{console.error(String(error.message).replaceAll(seed.session,'[session]').replaceAll(seed.csrf,'[csrf]'));process.exit(1);});
