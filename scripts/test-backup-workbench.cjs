// Optional isolated browser check. No host, real credentials or TLS changes.
const pw = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const path = require('node:path');
const assert = (ok, message) => { if (!ok) throw new Error(message); };
(async () => {
  let input = ''; for await (const chunk of process.stdin) input += chunk;
  const seed = JSON.parse(input), engine = process.env.WG_TEST_BROWSER_ENGINE || 'chromium';
  const browser = await pw[engine].launch({ ...(engine === 'chromium' ? { channel: 'chrome' } : {}), headless: true });
  let stage = 'launch'; const errors = [];
  const review = '/backups?preview=' + encodeURIComponent(seed.preview);
  try {
    if (!process.env.WG_TEST_BACKUP_NATIVE_ONLY) for (const lang of ['fa','en']) for (const theme of ['light','dark']) for (const width of [320,390,768,1440]) {
      const ctx = await browser.newContext({ viewport: {width,height:900}, reducedMotion:'reduce' });
      await ctx.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page = await ctx.newPage(); page.on('pageerror', e => errors.push(e.name));
      for (const route of ['/backups','/backups?tab=restore',review,'/backups?schedule=new','/backups?tab=delivery']) {
        stage = `${engine} ${lang} ${theme} ${width} ${route.split('?')[0]}`;
        await page.goto(seed.url + route + (route.includes('?')?'&':'?') + `lang=${lang}&theme=${theme}`);
        await page.waitForFunction(() => document.documentElement.dataset.ui === 'ready');
        assert(await page.locator('.workspace-tabs a[aria-current]').count() === 1, 'one current section');
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth+1), 'workspace horizontal overflow');
        assert(!await page.locator('main').innerText().then(t=>/backups\.[a-z_]+|users\.devices_label/.test(t)), 'untranslated copy');
        for (const label of await page.locator('main input:not([type=hidden]),main select').evaluateAll(nodes=>nodes.map(n=>Boolean(n.labels?.length || n.getAttribute('aria-label'))))) assert(label,'unlabelled input');
        if (route === review) {
          assert(await page.locator('.backup-report-counts > div').count() === 6, 'source inventory groups');
          await page.locator('.backup-review .technical-disclosure > summary').click();
          assert(await page.locator('.backup-fingerprint').innerText().then(t=>/^[a-f0-9]{64}$/.test(t)), 'canonical fingerprint');
          assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth+1), 'expanded verification overflow');
          if(process.env.WG_UI_SCREENSHOT_DIR && lang==='fa' && theme==='light' && [390,1440].includes(width)) await page.screenshot({path:path.join(process.env.WG_UI_SCREENSHOT_DIR,`backup-review-${engine}-${width}.png`),fullPage:true});
        }
        // Safari's default Tab policy can skip links. Exercise activation of
        // the native current-section anchor without assuming an OS preference.
        const current=page.locator('.workspace-tabs a[aria-current]'); await current.focus();
        assert(await current.evaluate(el=>document.activeElement===el),'section anchor can receive keyboard focus');
        await Promise.all([page.waitForNavigation(),page.keyboard.press('Enter')]);
      }
      await ctx.close();
    }
    for (const lang of ['fa','en']) {
      stage = `${engine} ${lang} native archive pagination`;
      const ctx = await browser.newContext({javaScriptEnabled:false,viewport:{width:390,height:900}});
      await ctx.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page=await ctx.newPage(); await page.goto(seed.url+'/backups?lang='+lang);
      assert(await page.locator('.backup-archives tbody tr').count()===25,'first archive page bound');
      await Promise.all([page.waitForNavigation(),page.locator('.backup-pagination a[href*="cursor="]').click()]);
      assert(await page.locator('.backup-archives tbody tr').count()===3,'cursor page bound');
      stage = `${engine} ${lang} native verification`;
      await page.goto(seed.url+'/backups?tab=restore&restore='+encodeURIComponent(seed.archive));
      const verifyButton=page.locator('button[formaction="/backups/verify"]');
      const navigation=page.waitForNavigation({timeout:10000}).catch(()=>null);
      // Native keyboard activation avoids WebKit's script-disabled pointer
      // stability instrumentation and still submits with this button's action.
      await verifyButton.focus(); await page.keyboard.press('Enter');
      if(!await navigation) {
        const state=await verifyButton.evaluate(el=>({disabled:el.disabled,valid:el.form.checkValidity(),action:new URL(el.formAction).pathname,invalid:[...el.form.elements].filter(n=>!n.checkValidity()).map(n=>n.name)}));
        throw new Error('native verify did not navigate '+JSON.stringify(state));
      }
      assert(await page.locator('.backup-review').count()===1 && await page.locator('form[action="/backups/restore/confirm"]').count()===0,'verification has no restore confirmation');
      stage = `${engine} ${lang} native saved review`;
      await page.goto(seed.url+review); await page.reload();
      assert(await page.locator('form[action="/backups/restore/confirm"]').count()===1,'saved review survives reload without JS');
      await ctx.close();
    }
    stage = `${engine} final approval`;
    const ctx=await browser.newContext({viewport:{width:390,height:900}});await ctx.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
    const page=await ctx.newPage(); await page.goto(seed.url+review);
    await Promise.all([page.waitForNavigation(),page.locator('form[action="/backups/restore/confirm"] button').click()]);
    assert(await page.locator('.operation-receipt').getAttribute('data-operation-state')==='awaiting_restart','truthful approval state');
    assert(await page.locator('input[name="pending"]').inputValue().then(t=>/^[a-f0-9]{64}$/.test(t)),'conditional cancellation identity');
    stage = `${engine} final cancellation`;
    await Promise.all([page.waitForNavigation(),page.locator('.backup-pending form button').click()]);
    assert(await page.locator('.backup-pending').count()===0,'approved restore cancellation');
    await ctx.close(); assert(errors.length===0,'browser runtime errors');
    console.log(`Backup workbench passed: ${engine}, ${process.env.WG_TEST_BACKUP_NATIVE_ONLY ? "native-flow debug only" : "80 fa/en Light/Dark responsive cells, expanded report/keyboard"}, cursor pagination, native verification/reload, approval and conditional cancellation`);
  } catch (e) { throw new Error(stage+': '+e.message); } finally { await browser.close(); }
})().catch(e=>{console.error(e.message);process.exitCode=1;});
