// State compositions use real routes against the shared ephemeral service fixtures.
const qa=require('./web-qa.cjs');
const assert=(ok,message)=>{if(!ok)throw new Error('contract: '+message);};
module.exports=async({browser,seed,final})=>{
  const cases=JSON.parse(seed.states || '[]');
  const allWidths=[320,360,390,430,768,799,800,959,960,961,1024,1280,1440,1920,2560,3440];
  const variants=final ? ['en','fa'].flatMap(lang=>['light','dark'].flatMap(theme=>allWidths.map(width=>({lang,theme,width})))) : [{lang:'en',theme:'light',width:1440},{lang:'fa',theme:'dark',width:390}];
  let cells=0,maxHTML=0; const failures=[];
  const performanceSummary={};
  let fromReached=!process.env.WG_TEST_UI_STATE_FROM;
  for(const state of cases){
    if(process.env.WG_TEST_UI_STATE && state.Name!==process.env.WG_TEST_UI_STATE)continue;
    if(!process.env.WG_TEST_UI_STATE && !fromReached){
      fromReached=state.Name===process.env.WG_TEST_UI_STATE_FROM;
      if(!fromReached)continue;
    }
    const context=await browser.newContext({reducedMotion:'reduce'}); await qa.install(context);
    if(state.Session)await context.addCookies([{name:'wg_session',value:state.Session,url:state.Base}]);
    const page=await context.newPage(); let lastLocale=''; let errors=0;
    page.on('dialog',dialog=>dialog.type()==='beforeunload'?dialog.accept():dialog.dismiss());
    page.on('pageerror',()=>errors++);
    try{
      for(const {lang,theme,width} of variants){
        if(!qa.variant(lang,theme,width))continue;
        const label=state.Name+' '+lang+'/'+theme+'/'+width;
        await page.setViewportSize({width,height:900});
        await page.setExtraHTTPHeaders({'X-WG-QA-Client':label});
        await context.addCookies([{name:'wg_theme',value:theme,url:state.Base},{name:'wg_locale',value:lang,url:state.Base}]);
        if(state.Session && lang!==lastLocale){
          await page.request.post(state.Base+'/prefs/locale',{form:{_csrf:state.CSRF,locale:lang}}); lastLocale=lang;
        }
        const path=state.Path+(state.Public?(state.Path.includes('?')?'&':'?')+'lang='+lang:'');
        const response=await page.goto(state.Base+path);
        assert(response.status()===state.Status,label+' HTTP status');
        await page.waitForFunction(()=>document.documentElement.dataset.ui==='ready');
        const cleanup=state.Prepare?await require('./web-state-actions.cjs').prepare(page,state,label):null;
        assert(await page.locator('html').getAttribute('dir')===(lang==='fa'?'rtl':'ltr'),label+' direction');
        assert(await page.locator('html').getAttribute('data-theme')===theme,label+' theme');
        assert(await page.locator('main h1').count()===1,label+' primary heading');
        const fitsViewport=await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth);
        if(!fitsViewport){
          const geometry=await page.evaluate(()=>{
            const rect=el=>{
              const r=el?.getBoundingClientRect();
              return r?{tag:el.tagName,classes:typeof el.className==='string'?el.className:'',left:Math.round(r.left),right:Math.round(r.right),width:Math.round(r.width)}:null;
            };
            return{
              viewport:{innerWidth,clientWidth:document.documentElement.clientWidth,scrollWidth:document.documentElement.scrollWidth},
              landmarks:['body','.shell','.main','.content','.collection','.users-table','.users-table tbody','dialog[open]','.drawer-body'].map(selector=>rect(selector==='body'?document.body:document.querySelector(selector))),
              elements:[...document.querySelectorAll('main,dialog[open],dialog[open] *,main *')].filter(el=>{const r=el.getBoundingClientRect();return r.right>innerWidth+1||r.left< -1;}).map(el=>{const r=el.getBoundingClientRect();return{tag:el.tagName,classes:typeof el.className==='string'?el.className:'',parent:typeof el.parentElement?.className==='string'?el.parentElement.className:'',left:Math.round(r.left),right:Math.round(r.right),width:Math.round(r.width)};}).slice(0,16),
            };
          });
          console.log('State overflow '+label+': '+JSON.stringify(geometry));
        }
        assert(fitsViewport,label+' viewport overflow');
        assert(errors===0,label+' JavaScript errors');
        maxHTML=Math.max(maxHTML,require('node:zlib').gzipSync(await page.content()).length);
        qa.merge(performanceSummary,await qa.measure(page));
        await qa.scan(page,label,width===390||width===1440);
        if(cleanup)await cleanup();
        cells++;
      }
    }catch(error){
      failures.push(state.Name);
      console.log('State failure '+state.Name+': '+(error.message.startsWith('contract:')?error.message:'browser operation failed; sensitive details suppressed'));
    }finally{await context.close();}
    if(final)console.log('State matrix progress: '+cells+' cells; '+state.Name+' complete');
  }
  console.log('Checked state compositions '+browser.version()+': '+cells+' cells; max rendered HTML gzip '+maxHTML+' B; observed page maxima '+JSON.stringify(performanceSummary));
  assert(failures.length===0,'state cases failed: '+failures.join(', '));
  assert(cells>0,'state filters selected no cells');
};
