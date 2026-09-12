// Auth/public browser contract; reuses the product fixture and never logs capabilities.
const assert = (ok, message) => { if (!ok) throw new Error('contract: ' + message); };
module.exports = async ({ browser, seed, final }) => {
  let step = 'launch';
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
  const page = await context.newPage();
  let errors = 0;
  page.on('pageerror', () => errors++);
  const navigate = async (base, path, status = 200) => {
    const response = await page.goto(base + path);
    assert(response.status() === status, step + ' HTTP status');
    await page.waitForFunction(() => document.documentElement.dataset.ui === 'ready');
  };
  const submit = async form => Promise.all([page.waitForNavigation(), form.locator('button[type="submit"]').last().click()]);
  let cells = 0, maxHTML = 0;
  try {
    step = 'login validation';
    await navigate(seed.url, '/login?next=%2Fdashboard');
    await page.locator('#f-username').fill('browser-login');
    await page.locator('#f-password').fill('invalid-input');
    await submit(page.locator('form[action="/login"]'));
    assert(await page.locator('#f-username').inputValue() === 'browser-login', 'login keeps username');
    assert(await page.locator('#f-password').inputValue() === '', 'login clears password');
    assert(await page.locator('#auth-message').evaluate(el => el === document.activeElement), 'login feedback focus');
    await page.locator('#f-password').fill('visibility-probe');
    await page.locator('[data-toggle-password="f-password"]').click();
    assert(await page.locator('#f-password').getAttribute('type') === 'text', 'password reveal works with shared module');
    await page.locator('[data-toggle-password="f-password"]').click();
    await page.locator('#f-password').fill('');

    step = 'public download and QR';
    let qrRequests = 0;
    page.on('request', request => { if (/\/qr(?:\?|$)/.test(request.url())) qrRequests++; });
    await navigate(seed.url, seed.sub + '?lang=en');
    assert(qrRequests === 0, 'public QR is not eagerly requested');
    await page.route('**/devices/*/config?*', route => route.abort());
    await page.locator('[data-config-download]').first().click();
    await page.waitForFunction(() => document.querySelector('[data-download-state]')?.getAttribute('role') === 'alert');
    assert(await page.locator('[data-download-state]').first().isVisible(), 'public download failure is visible');
    await page.unroute('**/devices/*/config?*');
    const [download] = await Promise.all([page.waitForEvent('download'), page.locator('[data-config-download]').first().click()]);
    assert(download.suggestedFilename().endsWith('.conf'), 'public configuration downloads as a file');
    await page.locator('[data-qr]').first().click();
    await page.waitForFunction(() => document.querySelector('#qr-img')?.naturalWidth > 0);
    await page.locator('#qr-modal [data-close-modal]').click();
    assert(!await page.locator('#qr-img').getAttribute('src'), 'public QR close clears image');
    await page.route('**/devices/*/qr?*', route => route.abort());
    await page.locator('[data-qr]').first().click();
    await page.waitForFunction(() => !document.querySelector('[data-qr-retry]').hidden);
    await page.unroute('**/devices/*/qr?*');
    await page.locator('[data-qr-retry]').click();
    await page.waitForFunction(() => document.querySelector('#qr-img')?.naturalWidth > 0);
    await page.locator('#qr-modal [data-close-modal]').click();

    step = 'auth and public compositions';
    const widths = final ? [320,360,390,430,768,799,800,959,960,961,1024,1280,1440,1920,2560,3440] : [390,1440];
    const surfaces = [
      ['login', seed.url, '/login', 200], ['expired-login', seed.url, '/login?expired=1', 200],
      ['limited-login', seed.url, '/login?e=rate', 200], ['onboarding', seed.setupURL, '/onboarding', 200],
      ['not-found', seed.url, '/not-a-page', 404], ['subscription', seed.url, seed.sub, 200],
      ['unavailable-subscription', seed.url, '/sub/not-a-capability', 404],
    ];
    for (const lang of ['en','fa']) {
      for (const theme of ['light','dark']) {
        await context.addCookies([{ name:'wg_locale', value:lang, url:seed.url }, { name:'wg_theme', value:theme, url:seed.url }]);
        for (const width of widths) {
          await page.setViewportSize({ width, height:900 });
          for (const [name, base, path, status] of surfaces) {
            step = name + ' ' + lang + '/' + theme + '/' + width;
            const target = name.includes('subscription') ? path + '?lang=' + lang : path;
            await navigate(base, target, status);
            assert(await page.locator('html').getAttribute('dir') === (lang === 'fa' ? 'rtl':'ltr'), step + ' direction');
            assert(await page.locator('html').getAttribute('data-theme') === theme, step + ' theme');
            assert(await page.locator('main h1').count() === 1, step + ' one heading');
            assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), step + ' viewport overflow');
            assert(await page.locator('input:not([type="hidden"]),select,textarea').evaluateAll(elements => elements.every(el => el.labels?.length || el.getAttribute('aria-label'))), step + ' control names');
            maxHTML = Math.max(maxHTML, require('node:zlib').gzipSync(await page.content()).length);
            if (process.env.WG_UI_SCREENSHOT_DIR && ['login','onboarding','subscription'].includes(name) && ((width ===1440 && lang==='en' && theme==='light') || (width===390 && lang==='fa' && theme==='dark'))) {
              const fs=require('node:fs'), pathModule=require('node:path');
              fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR,{recursive:true});
              await page.screenshot({path:pathModule.join(process.env.WG_UI_SCREENSHOT_DIR,'10.5-'+name+'-'+width+'.png'),fullPage:true});
            }
            cells++;
          }
        }
      }
    }
    step = 'onboarding validation and completion';
    await navigate(seed.setupURL, '/onboarding');
    await page.locator('#o-username').fill('new-owner');
    await page.locator('#o-password').fill('short');
    await page.locator('#o-password-confirm').fill('short');
    await submit(page.locator('form[action="/onboarding"]'));
    assert(await page.locator('#o-password').getAttribute('aria-invalid') === 'true', 'onboarding marks field');
    assert(await page.locator('#o-password').inputValue() === '', 'onboarding clears password');
    const password=require('node:crypto').randomBytes(24).toString('hex');
    await page.locator('#o-password').fill(password);
    await page.locator('#o-password-confirm').fill(password);
    await submit(page.locator('form[action="/onboarding"]'));
    assert(await page.locator('.dashboard-page').count()===1,'onboarding reaches workspace');
    await context.clearCookies();
    step = 'login successful safe return';
    await navigate(seed.url, '/login?next=%2Fdashboard');
    await page.locator('#f-username').fill('browser-login');
    await page.locator('#f-password').fill(seed.loginPassword);
    await submit(page.locator('form[action="/login"]'));
    assert(new URL(page.url()).pathname === '/dashboard','login honors safe return');
    assert(errors===0,'auth/public JavaScript errors');
    console.log('PASS authentication/public '+browser.version()+': '+cells+' composition cells; native HTTP statuses, onboarding, login, lazy QR and download recovery; max rendered HTML gzip '+maxHTML+' B');
  } catch(error) {
    throw new Error(error.message.startsWith('contract:') ? error.message : 'contract: '+step+' operation failed (sensitive details suppressed)');
  } finally { await context.close(); }
  // Shared QR extraction also affects the existing admin user detail.
  const admin = await browser.newContext({ reducedMotion:'reduce' });
  await admin.addCookies([{name:'wg_session',value:seed.session,url:seed.url}]);
  const adminPage=await admin.newPage();
  try {
    await adminPage.goto(seed.url+'/users/'+seed.user);
    await adminPage.locator('[data-qr]').first().click();
    await adminPage.waitForFunction(()=>document.querySelector('#qr-img')?.naturalWidth>0);
    await adminPage.locator('#qr-modal [data-close-modal]').click();
    assert(!await adminPage.locator('#qr-img').getAttribute('src'),'shared admin QR clears image');
  } finally { await admin.close(); }
};
