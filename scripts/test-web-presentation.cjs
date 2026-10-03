const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const playwright=require(process.env.WG_TEST_PLAYWRIGHT||'playwright');
const qa=require('./web-qa.cjs');const seed=JSON.parse(fs.readFileSync(0,'utf8'));
(async()=>{
 const engine=process.env.WG_TEST_BROWSER_ENGINE||'chromium';
 const browser=await playwright[engine].launch({headless:true,...(engine==='chromium'?{channel:process.env.WG_TEST_BROWSER_CHANNEL||'chrome'}:{})});let cells=0;
 try{for(const lang of ['fa','en'])for(const theme of ['light','dark'])for(const width of [320,390,768,1440]){
  if(!qa.variant(lang,theme,width))continue;
  const context=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'});await qa.install(context);
  await context.addCookies([{name:'wg_session',value:seed.session,url:seed.url},{name:'wg_theme',value:theme,url:seed.url}]);
  await context.request.post(seed.url+'/prefs/locale',{form:{locale:lang,_csrf:seed.csrf}});
  const page=await context.newPage();let errors=0;page.on('pageerror',()=>errors++);
  for(const route of ['/appearance','/settings','/users','/users/new','/interfaces/new']){
   await page.goto(seed.url+route);await page.locator('h1').waitFor();await page.evaluate(()=>document.fonts.ready);
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`overflow ${lang}/${theme}/${width}/${route}`);
   await qa.scan(page,`presentation-${lang}-${theme}-${width}-${route.replaceAll('/','-')}`);
  }
  await page.locator('#iface-tab-pools').click();
  await page.locator('[data-pool-proposed]').filter({hasText:/10\./}).waitFor();
  assert(await page.locator('#iface-panel-pools').isVisible(),'pool tab did not open');
  await page.locator('[name="pool_mode"][value="custom"]').check();
  await page.locator('[data-pool-size]').selectOption('22');
  await page.waitForFunction(()=>[...document.querySelector('[data-pool-preset]').options].some(option=>option.value.endsWith('/22')));
  const available=await page.locator('[data-pool-preset]').evaluate(select=>[...select.options].find(option=>option.value&&!option.disabled)?.value);
  assert(available,'available preset missing');await page.locator('[data-pool-preset]').selectOption(available);
  assert.equal(await page.locator('#i-subnet').inputValue(),available,'preset did not populate CIDR');
  await qa.scan(page,`pool-editor-${lang}-${theme}-${width}`);
  await page.locator('.field-help-trigger').filter({visible:true}).first().click();
  await page.locator('.field-help-content:popover-open').waitFor();
  const bounds=await page.locator('.field-help-content:popover-open').boundingBox();assert(bounds.x>=0&&bounds.x+bounds.width<=width+1,'help popover outside viewport');
  await page.keyboard.press('Escape');assert.equal(await page.locator('.field-help-content:popover-open').count(),0,'help could not dismiss');
  await page.locator('#iface-tab-profile').click();await qa.scan(page,`profile-editor-${lang}-${theme}-${width}`);
  if(process.env.WG_UI_SCREENSHOT_DIR){fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR,{recursive:true});await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`interface-${engine}-${lang}-${theme}-${width}.png`),fullPage:true});}
  await context.request.post(seed.url+'/appearance/digits/me',{form:{digits:'persian',_csrf:seed.csrf}});
  await page.goto(seed.url+'/appearance');assert.equal(await page.locator('html').getAttribute('data-digits'),'persian','Persian preference missing');
  await qa.scan(page,`digits-persian-${lang}-${theme}-${width}`);
  await context.request.post(seed.url+'/appearance/digits/me',{form:{digits:'latin',_csrf:seed.csrf}});
  assert.equal(errors,0,'browser runtime errors');cells++;await context.close();
 }}finally{await browser.close();}
 console.log(`Presentation browser: ${cells} locale/theme/viewport cells passed (${engine}).`);
})().catch(error=>{console.error(String(error.message).replaceAll(seed.session,'[session]').replaceAll(seed.csrf,'[csrf]'));process.exit(1)});
