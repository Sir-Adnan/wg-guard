// Development-only isolated browser matrix; no host or real certificate changes.
const pw = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const assert = (ok,message) => {if(!ok) throw new Error(message);};
(async()=>{
  let input='';for await(const chunk of process.stdin)input+=chunk;
  const seed=JSON.parse(input),engine=process.env.WG_TEST_BROWSER_ENGINE||'chromium';
  const browser=await pw[engine].launch({...engine==='chromium'?{channel:'chrome'}:{},headless:true});
  let stage='launch';const errors=[];
  try{
    for(const lang of ['fa','en'])for(const theme of ['light','dark'])for(const width of [320,390,768,1440]){
      const ctx=await browser.newContext({viewport:{width,height:900},reducedMotion:'reduce'});
      await ctx.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.name));
      for(const route of ['/updates?tab=operations','/backups','/settings/domains']){
        stage=`${engine} ${lang} ${theme} ${width} ${route}`;
        await page.goto(seed.url+route+(route.includes('?')?'&':'?')+`lang=${lang}&theme=${theme}`);
        const receipts=page.locator('.operation-receipt');assert(await receipts.count()===1,'one shared receipt');
        const state=await receipts.getAttribute('data-operation-state');
        assert(state===(route.startsWith('/updates')?'queued':route==='/backups'?'awaiting_restart':''),'truthful operation state');
        if(route==='/backups')assert(await page.locator('code').filter({hasText:'sudo wg-guard restart --yes'}).count()===1,'explicit restart action');
        assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'responsive workspace without horizontal overflow');
        assert(!await page.locator('body').innerText().then(t=>t.includes('operations.state.')||t.includes('operations.next.')),'localized shared receipt');
        const target=page.locator('main button:not([disabled]),main a').first();if(await target.count()){await target.focus();await page.keyboard.press('Tab');assert(await page.evaluate(()=>document.activeElement!==document.body),'keyboard navigation');}
      }
      await ctx.close();
    }
    for(const lang of ['fa','en']){
      const ctx=await browser.newContext({javaScriptEnabled:false,viewport:{width:390,height:900}});await ctx.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page=await ctx.newPage();for(const route of ['/updates?tab=operations','/backups','/settings/domains']){stage=`${engine} ${lang} native ${route}`;await page.goto(seed.url+route+(route.includes('?')?'&':'?')+`lang=${lang}`);assert(await page.locator('.operation-receipt').isVisible(),'native status/action guidance');}await ctx.close();
    }
    assert(errors.length===0,'no runtime browser errors');
    console.log(`Operational journey passed: ${engine}, 48 fa/en Light/Dark responsive cells, keyboard, queued/awaiting-restart/idle states and 6 native fallback cells`);
  }catch(e){throw new Error(stage+': '+e.message);}finally{await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1;});
