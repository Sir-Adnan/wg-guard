// Optional isolated real-browser checks; no production browser dependency.
const playwright = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const assert = (condition, message) => { if (!condition) throw new Error(message); };
(async () => {
  let input = ''; for await (const chunk of process.stdin) input += chunk;
  const seed = JSON.parse(input);
  const engine = process.env.WG_TEST_BROWSER_ENGINE || 'chromium';
  const browser = await playwright[engine].launch({ ...(engine === 'chromium' ? { channel: 'chrome' } : {}), headless: true });
  let stage = 'launch'; const errors = [];
  try {
    for (const lang of ['en', 'fa']) for (const theme of ['light', 'dark']) for (const width of [320,390,768,1440]) {
      stage = `${engine} ${lang} ${theme} ${width}`;
      const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' });
      await context.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page = await context.newPage(); page.on('pageerror', error => errors.push(error.name));
      await page.goto(seed.url + `/settings/domains?lang=${lang}&theme=${theme}`);
      assert(await page.locator('.domain-site').count() === 2, 'two independent origin cards');
      assert(!await page.locator('#domain-origin').isDisabled(), 'available owner form');
      assert(await page.locator('[data-domain-fields=automatic]').isVisible(), 'automatic default');
      await page.selectOption('#domain-method', 'manual');
      assert(await page.locator('[data-domain-fields=manual]').isVisible(), 'manual section visible');
      assert(!await page.locator('[data-domain-fields=automatic]').isVisible(), 'automatic section hidden');
      await page.locator('details.collapse').last().evaluate(el => { el.open = true; });
      await page.selectOption('#domain-source', 'paths');
      assert(await page.locator('#domain-cert-source').isVisible(), 'controlled path source');
      assert(await page.locator('#domain-cert').isDisabled(), 'unused private upload disabled');
      await page.selectOption('#domain-method', 'external');
      assert(await page.locator('[data-domain-fields=external]').isVisible(), 'external ownership guidance');
      assert(await page.locator('#domain-key-source').isDisabled(), 'inactive path does not submit');
      await page.selectOption('#domain-method', 'manual');
      await page.selectOption('#domain-source', 'upload');
      assert(await page.locator('#domain-key').isVisible(), 'upload restored');
      const geometry = await page.evaluate(() => ({ overflow: document.documentElement.scrollWidth > innerWidth + 1, fields: [...document.querySelectorAll('.domain-configure input:not([type=hidden]),.domain-configure select')].filter(el=>el.getBoundingClientRect().width && !el.closest('[hidden]')).map(el=>el.getBoundingClientRect()).every(r=>r.width>=100 && r.left>=-1 && r.right<=innerWidth+1) }));
      assert(!geometry.overflow && geometry.fields, 'contained responsive controls');
      await page.locator('#domain-origin').fill('https://sub.example.test');
      await page.locator('#domain-origin').focus(); await page.keyboard.press('Tab');
      assert(await page.evaluate(()=>document.activeElement !== document.body), 'keyboard navigation');
      if (process.env.WG_TEST_DOMAIN_SCREENSHOTS && lang === 'fa' && theme === 'light' && [320,1440].includes(width)) {
        const fs = require('fs'); fs.mkdirSync(process.env.WG_TEST_DOMAIN_SCREENSHOTS,{recursive:true});
        await page.screenshot({path:process.env.WG_TEST_DOMAIN_SCREENSHOTS+`/domains-${engine}-${width}.png`,fullPage:true});
      }
      if (process.env.WG_TEST_AXE) {
        const Axe = require(process.env.WG_TEST_AXE).default;
        const result = await new Axe({page}).withTags(['wcag2a','wcag2aa']).analyze();
        assert(result.violations.length===0, 'accessible domain workspace');
      }
      await context.close();
    }
    for (const lang of ['en','fa']) {
      stage = `${engine} ${lang} no-JavaScript`;
      const context = await browser.newContext({javaScriptEnabled:false,viewport:{width:390,height:900}});
      await context.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
      const page=await context.newPage();await page.goto(seed.url+`/settings/domains?lang=${lang}`);
      assert(await page.locator('#domain-cert').isVisible(), 'native manual import fallback');
      assert(await page.locator('#domain-challenge').isVisible(), 'native automatic fallback');
      assert(await page.locator('form.domain-configure').getAttribute('enctype')==='multipart/form-data', 'private multipart form');
      await context.close();
    }
    assert(errors.length === 0, 'no browser runtime errors');
    console.log(`Domains passed: ${engine}, 16 responsive fa/en Light/Dark cells, method/source switching, keyboard and 2 native fallback cells`);
  } catch(error) { throw new Error(stage + ': ' + error.message); }
  finally { await browser.close(); }
})().catch(error => { console.error(error.message); process.exitCode=1; });
