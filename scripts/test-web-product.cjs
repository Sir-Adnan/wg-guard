// Optional real-browser milestone checks against TestBrowserPhase10's isolated
// database. Ephemeral capabilities arrive on stdin and never enter diagnostics.
const playwright = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const assert = (ok, message) => { if (!ok) throw new Error('contract: ' + message); };
let stage = 'launch';
(async () => {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const seed = JSON.parse(input);
  const suite = process.env.WG_TEST_UI_SUITE || '10.2';
  const engine = process.env.WG_TEST_BROWSER_ENGINE || 'chromium';
  const browser = await playwright[engine].launch({
    ...(engine === 'chromium' ? { channel: process.env.WG_TEST_BROWSER_CHANNEL || 'chrome' } : {}), headless: true,
  });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
    await context.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }]);
    const page = await context.newPage();
    let runtimeErrors = 0;
    page.on('pageerror', () => runtimeErrors++);
    const goto = async path => {
      const response = await page.goto(seed.url + path);
      assert(response.status() === 200, 'expected successful page');
      await page.waitForFunction(() => document.documentElement.dataset.ui === 'ready');
    };
    const submit = async form => {
      await Promise.all([page.waitForNavigation(), form.locator('button[type="submit"]').last().click()]);
    };
    const locale = async value => {
      await goto('/plans');
      await page.request.post(seed.url + '/prefs/locale', { form: {
        locale: value, _csrf: await page.locator('meta[name="csrf-token"]').getAttribute('content'),
      } });
    };
    if (suite === '10.2' || suite === 'final') {
      stage = 'read-only operational pages';
      const readerContext = await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: 'reduce' });
      await readerContext.addCookies([{ name: 'wg_session', value: seed.reader, url: seed.url }]);
      const readerPage = await readerContext.newPage();
      for (const path of ['/plans', '/interfaces']) {
        const response = await readerPage.goto(seed.url + path);
        assert(response.status() === 200, 'read-only list is available');
        assert(await readerPage.locator('main a[href$="/new"], main a[href$="/edit"], main form[method="post"]').count() === 0, 'read-only list has no write controls');
      }
      await readerContext.close();
      stage = 'plan failed submission retains input';
      await goto('/plans/new');
      const form = page.locator('form[action="/plans"]');
      await form.locator('[name="name"]').fill('Retained / حفظ');
      await form.locator('[name="device_limit"]').evaluate(el => { el.type = 'text'; el.value = 'invalid'; });
      await submit(form);
      assert(await page.locator('[name="name"]').inputValue() === 'Retained / حفظ', 'plan name survives validation');
      assert(await page.locator('[name="device_limit"]').inputValue() === 'invalid', 'invalid plan value survives browser sanitization');
      assert(await page.locator('[role="alert"]').count() > 0, 'plan error is announced');
      stage = 'interface failed submission retains input';
      await goto('/interfaces/new');
      const ifaceForm = page.locator('form[action="/interfaces"]');
      await ifaceForm.locator('[name="name"]').fill('awg7');
      await ifaceForm.locator('[name="listen_port"]').evaluate(el => { el.type = 'text'; el.value = 'invalid'; });
      await submit(ifaceForm);
      assert(await page.locator('[name="name"]').inputValue() === 'awg7', 'interface name survives validation');
      assert(await page.locator('[name="listen_port"]').inputValue() === 'invalid', 'invalid interface value survives browser sanitization');
      stage = 'generated interface profile round-trip';
      await goto('/interfaces/new');
      await page.locator('[name="name"]').fill('awg7');
      await page.locator('[data-generate-obf="recommended"]').click();
      await page.waitForFunction(() => document.querySelector('[data-profile-policy]').value === 'recommended');
      const headers = await page.locator('[name^="obf_h"]').evaluateAll(inputs => inputs.filter(el => /^obf_h[1-4]$/.test(el.name)).map(el => [el.name, el.value]));
      assert(headers.length === 4 && headers.every(([, value]) => value !== ''), 'all generated headers are populated');
      await submit(page.locator('form[action="/interfaces"]'));
      const interfaceRow = page.locator('tr').filter({ hasText: 'awg7' });
      await interfaceRow.locator('[aria-haspopup="menu"]').click();
      await interfaceRow.locator('a[role="menuitem"][href$="/edit"]').click();
      for (const [name, value] of headers) assert(await page.locator('[name="' + name + '"]').inputValue() === value, 'generated header round-trip');
      assert(await page.locator('[name="obf_hpk"]').inputValue() === '', 'stored header protection secret is not rendered');
      stage = 'native interface edit without JavaScript';
      const editURL = page.url();
      const nativeContext = await browser.newContext({ javaScriptEnabled: false });
      await nativeContext.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }]);
      const nativePage = await nativeContext.newPage();
      await nativePage.goto(editURL);
      stage = 'native interface header edit';
      const changedHeader = String(Number(headers.find(([name]) => name === 'obf_h1')[1]) + 1);
      await nativePage.locator('[name="obf_h1"]').fill(changedHeader);
      stage = 'native interface header submit';
      await Promise.all([nativePage.waitForNavigation(), nativePage.locator('form[action$="/edit"] button[type="submit"]').click()]);
      assert(new URL(nativePage.url()).pathname === '/interfaces', 'native generated edit saves');
      await nativePage.goto(editURL);
      assert(await nativePage.locator('[name="obf_h1"]').inputValue() === changedHeader, 'native edited header persists');
      assert(await nativePage.locator('[data-profile-policy]').inputValue() === 'custom', 'native edited profile becomes custom');
      stage = 'native interface disable';
      await nativePage.locator('[data-obf-toggle]').uncheck();
      await Promise.all([nativePage.waitForNavigation(), nativePage.locator('form[action$="/edit"] button[type="submit"]').click()]);
      assert(new URL(nativePage.url()).pathname === '/interfaces', 'native plain transition saves');
      await nativePage.goto(editURL);
      assert(!await nativePage.locator('[data-obf-toggle]').isChecked(), 'native plain transition persists');
      await nativeContext.close();
      stage = 'plan create edit and status action';
      await goto('/plans/new');
      await page.locator('[name="name"]').fill('Browser plan');
      await page.locator('[name="traffic_limit_value"]').fill('12.5');
      await submit(page.locator('form[action="/plans"]'));
      const row = page.locator('tr').filter({ hasText: 'Browser plan' });
      assert(await row.count() === 1, 'created plan appears once');
      await row.locator('[aria-haspopup="menu"]').click();
      await row.locator('a[role="menuitem"][href$="/edit"]').click();
      assert(await page.locator('[name="traffic_limit_value"]').inputValue() === '12.5', 'quota round-trips');
      await page.locator('[name="name"]').fill('Browser plan updated');
      await submit(page.locator('form[action$="/edit"]'));
      const updated = page.locator('tr').filter({ hasText: 'Browser plan updated' });
      await updated.locator('[aria-haspopup="menu"]').click();
      await Promise.all([page.waitForNavigation(), updated.locator('form[action$="/disable"] button').click()]);
      const disabled = page.locator('tr').filter({ hasText: 'Browser plan updated' });
      assert(await disabled.locator('form[action$="/enable"]').count() === 1, 'disabled plan offers enable');
      const seededPlan = page.locator('tr').filter({ hasText: 'Monthly / ماهانه' });
      assert(!/Kbps/.test(await seededPlan.locator('td').nth(3).innerText()), 'device count is not a bandwidth value');
    }
    const routes = ['/interfaces', '/interfaces/new', '/interfaces/' + seed.iface + '/edit', '/plans', '/plans/new', '/plans/' + seed.plan + '/edit'];
    let cells = 0;
    let maxHTMLGzip = 0;
    for (const lang of ['en', 'fa']) {
      await locale(lang);
      for (const theme of ['light', 'dark']) {
        await context.addCookies([{ name: 'wg_theme', value: theme, url: seed.url }]);
        for (const width of suite === 'final' ? [320, 360, 390, 430, 768, 799, 800, 959, 960, 961, 1024, 1280, 1440, 1920, 2560, 3440] : [390, 1440]) {
          await page.setViewportSize({ width, height: 900 });
          for (let index = 0; index < routes.length; index++) {
            stage = suite + ' composition ' + index + ' ' + lang + '/' + theme + '/' + width;
            await goto(routes[index]);
            assert(await page.locator('html').getAttribute('dir') === (lang === 'fa' ? 'rtl' : 'ltr'), 'page direction');
            assert(await page.locator('html').getAttribute('data-theme') === theme, 'page theme');
            assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'page viewport overflow');
            assert(await page.locator('main h1').count() === 1, 'single primary page heading');
            assert(await page.locator('input:not([type="hidden"]),select,textarea').evaluateAll(elements => elements.every(el => el.labels?.length || el.getAttribute('aria-label') || el.getAttribute('aria-labelledby'))), 'form controls have names');
            maxHTMLGzip = Math.max(maxHTMLGzip, require('node:zlib').gzipSync(await page.content()).length);
            if (process.env.WG_UI_SCREENSHOT_DIR && ((width === 1440 && lang === 'en' && theme === 'light') || (width === 390 && lang === 'fa' && theme === 'dark'))) {
              const fs = require('node:fs'), path = require('node:path');
              fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR, { recursive: true });
              await page.screenshot({ path: path.join(process.env.WG_UI_SCREENSHOT_DIR, suite + '-' + index + '-' + width + '.png'), fullPage: true });
            }
            cells++;
          }
        }
      }
    }
    assert(runtimeErrors === 0, 'page JavaScript errors');
    console.log('PASS ' + engine + ' ' + browser.version() + ' Phase ' + suite + ': ' + cells + ' composition cells; form preservation and plan mutations; max rendered HTML gzip ' + maxHTMLGzip + ' B');
    await context.close();
  } finally { await browser.close(); }
})().catch(error => {
  console.error('FAIL ' + stage + (error.message.startsWith('contract:') ? ': ' + error.message : ' (browser operation failed; sensitive details suppressed)'));
  process.exitCode = 1;
});
