// Isolated synthetic health states; no host/network/credential changes.
const pw=require(process.env.WG_TEST_PLAYWRIGHT||'playwright');
const path=require('node:path');
const assert=(ok,message)=>{if(!ok)throw new Error(message);};
(async()=>{
  let input='';for await(const chunk of process.stdin)input+=chunk;
  const seed=JSON.parse(input),engine=process.env.WG_TEST_BROWSER_ENGINE||'chromium';
  const browser=await pw[engine].launch({...engine==='chromium'?{channel:'chrome'}:{},headless:true});
  let stage='launch';const errors=[];
  try{
    for(const lang of ['fa','en'])for(const theme of ['light','dark'])for(const width of [320,390,768,1440]){
      const ctx=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'});await ctx.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.name));
      for(const state of ['healthy','pending','running','unavailable']){
        stage=`${engine} ${lang} ${theme} ${width} ${state}`;
        await page.goto(seed.url+`/system?lang=${lang}&theme=${theme}&_qa_state=${state}`);
        await page.waitForFunction(()=>document.documentElement.dataset.ui==='ready');
        assert(await page.locator('[data-system-state]').count()===4,'four distinct evidence cards');
        assert(!await page.locator('main').innerText().then(t=>t.includes('system.state.')||t.includes('system.help.')),'localized state/guidance');
        assert(await page.locator('main form').count()===0,'read-only health has mutation forms');
        assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'responsive overflow');
        await page.locator('.technical-disclosure > summary').click();
        assert(await page.locator('main code').innerText()==='sudo wg-guard doctor','bounded host diagnostic guidance');
        assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'expanded detail overflow');
        const refresh=page.locator('main header a');await refresh.focus();assert(await refresh.evaluate(el=>document.activeElement===el),'keyboard focus');
        if(process.env.WG_UI_SCREENSHOT_DIR&&state==='pending'&&lang==='fa'&&theme==='light'&&[390,1440].includes(width)){
          await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`system-health-${engine}-${width}.png`),fullPage:true});
        }
      }
      await ctx.close();
    }
    for(const lang of ['fa','en']){
      const ctx=await browser.newContext({javaScriptEnabled:false,viewport:{width:390,height:900}});await ctx.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page=await ctx.newPage();await page.goto(seed.url+`/system?lang=${lang}&_qa_state=pending`);await page.locator('.technical-disclosure > summary').focus();await page.keyboard.press('Enter');assert(await page.locator('.technical-disclosure').getAttribute('open')!==null,'native disclosure');await ctx.close();
    }
    assert(errors.length===0,'browser runtime errors');console.log(`System health passed: ${engine}, 64 fa/en light/dark responsive state cells, keyboard/details, read-only forms and 2 native fallback cells`);
  }catch(e){throw new Error(stage+': '+e.message);}finally{await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1;});
