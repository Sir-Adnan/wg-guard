const fs=require('fs'),path=require('path'),assert=require('assert/strict');
const playwright=require(process.env.WG_TEST_PLAYWRIGHT||'playwright');
const qa=require('./web-qa.cjs');const seed=JSON.parse(fs.readFileSync(0,'utf8'));
async function checkSectionLayout(page,selector,inputSelector,cell){
 const layout=await page.locator(selector).evaluate((section,inputSelector)=>{
  const card=section.getBoundingClientRect(),legend=section.querySelector('legend:not(.sr-only),.card-title').getBoundingClientRect();
  const input=section.querySelector(inputSelector),labelNode=section.querySelector(`label[for="${input.id}"]`)||section.querySelector('label');
  const field=input.getBoundingClientRect(),label=(labelNode.closest('.field-label-row')||labelNode).getBoundingClientRect();
  const style=getComputedStyle(section.querySelector(':scope > .card-content')||section);
  return {padding:Math.min(parseFloat(style.paddingLeft),parseFloat(style.paddingRight)),insets:[legend.left-card.left,card.right-legend.right,field.left-card.left,card.right-field.right,label.left-card.left,card.right-label.right],top:legend.top-card.top,bottom:card.bottom-field.bottom,labelGap:field.top-label.bottom,legendGap:label.top-legend.bottom};
 },inputSelector);
 assert(layout.padding>=16,`card padding ${cell}: ${layout.padding}`);
 assert(layout.insets.every(inset=>inset>=15),`card content touches edge ${cell}: ${layout.insets}`);
 assert(layout.top>=15&&layout.bottom>=15,`card vertical padding ${cell}`);
 assert(layout.legendGap>=8&&layout.labelGap>=0,`card heading/field overlap ${cell}`);
}
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
   if(!await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1))console.log('TEMPDEBUG '+route+' '+JSON.stringify(await page.evaluate(()=>[...document.querySelectorAll('body *')].filter(el=>{const r=el.getBoundingClientRect();return r.right>innerWidth+1||r.left<-1}).slice(0,15).map(el=>({tag:el.tagName,id:el.id,cls:String(el.className).slice(0,80),l:Math.round(el.getBoundingClientRect().left),r:Math.round(el.getBoundingClientRect().right),w:el.scrollWidth}))))); // TEMPDEBUG
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`overflow ${lang}/${theme}/${width}/${route}`);
   await qa.scan(page,`presentation-${lang}-${theme}-${width}-${route.replaceAll('/','-')}`);
  }
  const cell=`${lang}/${theme}/${width}`;
  await checkSectionLayout(page,'#iface-panel-general > .card','#i-name',`general/${cell}`);
  await checkSectionLayout(page,'#iface-panel-general > .card','#i-endpoint',`endpoint/${cell}`);
  await page.locator('#iface-tab-pools').click();
  await page.locator('[data-pool-proposed]').filter({hasText:/10\./}).waitFor();
  assert(await page.locator('#iface-panel-pools').isVisible(),'pool tab did not open');
  await page.locator('label:has(> [name="pool_mode"][value="custom"])').click();assert(await page.locator('[name="pool_mode"][value="custom"]').isChecked(),'custom pool mode not selected');
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
  await page.locator('#obf-fields > summary').click();
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`expanded profile overflow ${cell}`);
  await checkSectionLayout(page,'#iface-panel-profile > .card','#i-obf-jc',`expanded profile/${cell}`);
  await qa.scan(page,`profile-expanded-${lang}-${theme}-${width}`);
  await context.request.post(seed.url+'/appearance/digits/me',{form:{digits:'persian',_csrf:seed.csrf}});
  await page.goto(seed.url+'/appearance');assert.equal(await page.locator('html').getAttribute('data-digits'),'persian','Persian preference missing');
  await qa.scan(page,`digits-persian-${lang}-${theme}-${width}`);
  await context.request.post(seed.url+'/appearance/digits/me',{form:{digits:'latin',_csrf:seed.csrf}});
  assert.equal(errors,0,'browser runtime errors');cells++;await context.close();
 }
 for(const [lang,width] of [['fa',390],['en',1440]]){
  if(!qa.variant(lang,'light',width))continue;
  const context=await browser.newContext({viewport:{width,height:900},javaScriptEnabled:false});
  await context.addCookies([{name:'wg_session',value:seed.session,url:seed.url},{name:'wg_theme',value:'light',url:seed.url}]);
  await context.request.post(seed.url+'/prefs/locale',{form:{locale:lang,_csrf:seed.csrf}});
  const page=await context.newPage();await page.goto(seed.url+'/interfaces/new');await page.evaluate(()=>document.fonts.ready);
  await checkSectionLayout(page,'#iface-panel-general > .card','#i-name',`no-JS general/${lang}/${width}`);
  await checkSectionLayout(page,'#iface-panel-general > .card','#i-endpoint',`no-JS endpoint/${lang}/${width}`);
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`no-JS form overflow ${lang}/${width}`);
  await context.close();
 }
 }finally{await browser.close();}
 console.log(`Presentation browser: ${cells} locale/theme/viewport cells passed (${engine}).`);
})().catch(error=>{console.error(String(error.message).replaceAll(seed.session,'[session]').replaceAll(seed.csrf,'[csrf]'));process.exit(1)});
