// Optional real-browser milestone checks against TestBrowserPhase10's isolated
// database. Ephemeral capabilities arrive on stdin and never enter diagnostics.
const playwright = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const qa = require('./web-qa.cjs');
const assert = (ok, message) => { if (!ok) throw new Error('contract: ' + message); };
let stage = 'launch';
(async () => {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const seed = JSON.parse(input);
  const suite = process.env.WG_TEST_UI_SUITE || '10.2';
  const group = process.env.WG_TEST_UI_GROUP || 'all';
  const engine = process.env.WG_TEST_BROWSER_ENGINE || 'chromium';
  const browser = await playwright[engine].launch({
    ...(engine === 'chromium' ? { channel: process.env.WG_TEST_BROWSER_CHANNEL || 'chrome' } : {}), headless: true,
  });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
    await qa.install(context);
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
      await page.request.post(seed.url + '/prefs/locale', { form: {
        locale: value, _csrf: seed.csrf,
      } });
    };
    if (process.env.WG_TEST_UI_STATE_MODE === 'preview' || group === 'states') {
      await require('./test-web-states.cjs')({browser,seed,final:suite==='final'});
      await context.close(); return;
    }
    if(group==='interactions'){
      await require('./test-web-interactions.cjs')({browser,seed,engine});
      await context.close();return;
    }
    if ((suite === '10.5' || suite === '10.6' || suite === 'final') && (group==='all' || group==='public')) {
      stage = 'authentication and public workflows';
      await require('./test-web-public.cjs')({ browser, seed, final: suite === 'final', compositionsOnly: suite === '10.6' });
      if (suite === '10.5' || group==='public') { await context.close(); return; }
    }
    if (suite === '10.3-users-native') {
      stage = 'native user detail labels and device retry';
      const nativeContext = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 390, height: 844 } });
      await nativeContext.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }]);
      const nativePage = await nativeContext.newPage();
      const response = await nativePage.goto(seed.url + '/users/' + seed.user);
      assert(response.status() === 200, 'native detail loads');
      assert(await nativePage.evaluate(() => {
        const ids = [...document.querySelectorAll('[id]')].map(el => el.id);
        return new Set(ids).size === ids.length;
      }), 'native detail IDs are unique');
      stage = 'native device disclosure';
      await nativePage.locator('details').filter({ has: nativePage.locator('#d-name-native') }).locator('summary').click();
      assert(await nativePage.locator('label[for="d-name-native"]').evaluate(el => el.control?.id === 'd-name-native' && el.control.getClientRects().length > 0), 'native label addresses visible device field');
      const nativeForm = nativePage.locator('form').filter({ has: nativePage.locator('#d-name-native') });
      stage = 'native device invalid submit';
      await Promise.all([nativePage.waitForNavigation(), nativeForm.locator('button[type="submit"]').click()]);
      assert(await nativePage.locator('#d-name[aria-invalid="true"]').count() === 1, 'native device validation offers retry');
      stage = 'native device corrected retry';
      assert(await nativePage.locator('#d-name').isVisible(), 'native retry input visible');
      await nativePage.locator('#d-name').fill('Native device');
      stage = 'native device retry submit';
      await Promise.all([nativePage.waitForNavigation(), nativePage.locator('form').filter({ has: nativePage.locator('#d-name') }).locator('button[type="submit"]').click()]);
      assert(await nativePage.locator('main').getByText('Native device', { exact: true }).count() === 1, 'native device creation completes');
      await nativeContext.close();
      await context.close();
      console.log('PASS ' + engine + ' ' + browser.version() + ' Phase 10.3-users-native: unique IDs, associated labels, native validation and device creation');
      return;
    }
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
    await page.waitForFunction(() => document.querySelector('[data-profile-policy]').value === 'suggested');
    assert(await page.locator('#awg-advanced').getAttribute('open') !== null, 'default expert profile opens advanced parameters');
    assert(await page.locator('[name="obf_random_trailers"]').isChecked() && await page.locator('[name="obf_disable_cookies"]').isChecked(), 'generated profile enables approved advanced flags');
      await page.locator('[name="name"]').fill('awg7');
    await page.locator('[data-generate-obf="performance"]').click();
    await page.waitForFunction(() => document.querySelector('[data-profile-policy]').value === 'performance');
      const headers = await page.locator('[name^="obf_h"]').evaluateAll(inputs => inputs.filter(el => /^obf_h[1-4]$/.test(el.name)).map(el => [el.name, el.value]));
      assert(headers.length === 4 && headers.every(([, value]) => value !== ''), 'all generated headers are populated');
      await submit(page.locator('form[action="/interfaces"]'));
      const interfaceRow = page.locator('tr').filter({ hasText: 'awg7' });
      await interfaceRow.locator('[aria-haspopup="menu"]').click();
      await interfaceRow.locator('a[role="menuitem"][href$="/edit"]').click();
      for (const [name, value] of headers) assert(await page.locator('[name="' + name + '"]').inputValue() === value, 'generated header round-trip');
      assert(await page.locator('[data-profile-policy]').inputValue() === 'performance', 'generated preset provenance round-trips');
      assert(await page.locator('[name="obf_hpk"]').inputValue() === '', 'stored header protection secret is not rendered');
      stage = 'native interface edit without JavaScript';
      const editURL = page.url();
    const nativeContext = await browser.newContext({ javaScriptEnabled: false, reducedMotion: 'reduce' });
      await nativeContext.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }]);
      const nativePage = await nativeContext.newPage();
      const submitNativeInterface = async () => {
        const button = nativePage.locator('form[action$="/edit"] button[type="submit"]');
        await button.scrollIntoViewIfNeeded();
        await Promise.all([nativePage.waitForNavigation(), button.click()]);
      };
      await nativePage.goto(editURL);
      stage = 'native interface header edit';
      const changedHeader = String(Number(headers.find(([name]) => name === 'obf_h1')[1]) + 1);
      await nativePage.locator('[name="obf_h1"]').fill(changedHeader);
      stage = 'native interface header submit';
      await submitNativeInterface();
      assert(new URL(nativePage.url()).pathname === '/interfaces', 'native generated edit saves');
      await nativePage.goto(editURL);
      assert(await nativePage.locator('[name="obf_h1"]').inputValue() === changedHeader, 'native edited header persists');
      assert(await nativePage.locator('[data-profile-policy]').inputValue() === 'custom', 'native edited profile becomes custom');
      stage = 'native interface disable';
      await nativePage.locator('label[for="i-obf-enabled"]').click();
      assert(!await nativePage.locator('[data-obf-toggle]').isChecked(), 'native switch label disables profile');
      await submitNativeInterface();
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
    if (['10.3', '10.3-users', 'final'].includes(suite)) {
      stage = 'eight-character username generation';
      await goto('/users/new');
      await page.locator('[data-generate="#u-username"]').click();
      const generatedUsername = await page.locator('[name="username"]').inputValue();
      assert(/^[A-Za-z]{5}[0-9]{3}$/.test(generatedUsername), 'generated username is one short word plus digits');
      stage = 'user invalid input preservation';
      await goto('/users/new');
      const userForm = page.locator('form[action="/users"]');
      await userForm.locator('[name="username"]').fill('browser-user');
      await userForm.locator('[name="note"]').fill('Line one\nخط دوم');
      await userForm.locator('[name="device_limit"]').evaluate(el => { el.type = 'text'; el.value = 'invalid'; });
      await submit(userForm);
      assert(await page.locator('[name="username"]').inputValue() === 'browser-user', 'user name survives validation');
      assert(await page.locator('[name="note"]').inputValue() === 'Line one\nخط دوم', 'multiline note survives validation');
      assert(await page.locator('[name="device_limit"]').inputValue() === 'invalid', 'invalid user limit survives validation');
      stage = 'user creation and device QR';
      await goto('/users/new');
      await page.locator('[name="username"]').fill('browser-user');
      await page.locator('[name="device_limit"]').fill('1');
      await submit(page.locator('form[action="/users"]'));
      assert(await page.locator('main h1').innerText() === 'browser-user', 'created user detail');
      const detailURL = page.url();
      stage = 'user device QR opens';
      const qr = page.locator('[data-qr]').first();
      await qr.click();
      await page.waitForFunction(() => [...document.querySelectorAll('dialog[open] img')].some(img => img.complete && img.naturalWidth > 0));
      stage = 'user device QR close focus';
      await page.locator('dialog[open] [data-close-modal]').first().click();
      assert(await qr.evaluate(el => el === document.activeElement), 'QR returns focus');
      stage = 'user device config download';
      const configLink = page.locator('a[download][href$="/config"]').first();
      const config = await page.request.get(new URL(await configLink.getAttribute('href'), seed.url).href);
      assert(config.status() === 200 && !!config.headers()['content-disposition'], 'config remains a download');
      stage = 'subscription revoke and restore';
      const revoke = page.locator('form[action$="/sub/revoke"]');
      await revoke.locator('button[type="submit"]').click();
      await Promise.all([page.waitForNavigation(), page.locator('[data-confirm-ok]').click()]);
      assert(await page.locator('form[action$="/sub/restore"]').count() === 1, 'revoked subscription offers restore');
      await submit(page.locator('form[action$="/sub/restore"]'));
      assert(await page.locator('#sub-url').inputValue() !== '', 'restored subscription is shareable');
      stage = 'user edit validation';
      await page.goto(detailURL.replace(/\?.*$/, '') + '/edit');
      await page.locator('[name="note"]').fill('Changed\nmultiline note');
      await page.locator('[name="device_limit"]').evaluate(el => { el.type = 'text'; el.value = 'invalid'; });
      await submit(page.locator('form[action$="/edit"]'));
      assert(await page.locator('[name="note"]').inputValue() === 'Changed\nmultiline note', 'edit values survive validation');
      stage = 'live user search and zero-selection state';
      await goto('/users');
      assert(await page.locator('[data-bulk-bar]').isHidden(), 'bulk controls stay hidden with no selection');
      await page.locator('#users-search').fill('ali');
      await page.waitForFunction(() => new URL(location.href).searchParams.get('q') === 'ali' && document.querySelectorAll('#users-results tbody tr').length === 1);
    assert(await page.locator('#users-results tbody tr').filter({ hasText: 'alice' }).count() === 1, 'live search returns the matching user');
    assert(await page.locator('#users-results tbody tr').filter({ hasText: 'browser-user' }).count() === 0, 'live search removes nonmatches');
      stage = 'users filtered empty';
      await goto('/users?q=no-such-browser-user');
      assert(await page.locator('main a[href="/users"]').count() > 0, 'filtered empty has clear action');
      stage = 'calendar keyboard';
      await goto('/users/new');
      const calendarTrigger = page.locator('[data-calendar]').first();
      await calendarTrigger.click();
      assert(await page.locator('[data-cal-day]:focus').count() === 1, 'calendar focuses a day');
      const dayBefore = await page.locator('[data-cal-day]:focus').getAttribute('data-cal-day');
      await page.keyboard.press(await page.locator('html').getAttribute('dir') === 'rtl' ? 'ArrowLeft' : 'ArrowRight');
      assert(await page.locator('[data-cal-day]:focus').getAttribute('data-cal-day') !== dayBefore, 'calendar arrow moves day focus');
      await page.keyboard.press('Home');
      await page.keyboard.press('PageDown');
      assert(await page.locator('[data-cal-day]:focus').count() === 1, 'calendar month move retains day focus');
      await page.keyboard.press('Escape');
      assert(await calendarTrigger.evaluate(el => el === document.activeElement), 'calendar returns trigger focus');
      stage = 'QR failure and retry';
      await goto('/users/' + seed.user);
      await page.route('**/devices/*/qr*', route => route.abort());
      await page.locator('[data-qr]').first().click();
      await page.waitForFunction(() => document.querySelector('[data-qr-retry]') && !document.querySelector('[data-qr-retry]').hidden);
      assert(await page.locator('[data-qr-state]').isVisible(), 'QR failure is visible');
      await page.unroute('**/devices/*/qr*');
      await page.locator('[data-qr-retry]').click();
      await page.waitForFunction(() => document.querySelector('#qr-img').naturalWidth > 0);
      await page.locator('dialog[open] [data-close-modal]').first().click();
      assert(!await page.locator('#qr-img').getAttribute('src'), 'closed QR clears sensitive image source');
    }
    if (['10.3', '10.3-dashboard', 'final'].includes(suite)) {
      stage = 'dashboard chart navigation and accessible data';
      await page.emulateMedia({ colorScheme: 'dark' });
      await goto('/dashboard');
      assert(await page.locator('#telemetry-card').getAttribute('data-health') === 'healthy', 'normal dashboard fixture stays healthy');
      assert(await page.locator('html').getAttribute('data-theme') === 'light', 'unsaved theme stays light on a dark OS');
      assert(await page.locator('.resource-card .sparkline').count() === 5, 'all live chart families render');
      const liveChart = page.locator('.resource-card .sparkline').first();
      await liveChart.hover({ position: { x: 140, y: 52 } });
      await page.waitForFunction(() => document.querySelector('.resource-card .chart-tooltip.is-visible')?.textContent.trim().length > 0);
      const liveTooltip = page.locator('.resource-card .chart-tooltip.is-visible');
      assert(await liveTooltip.count() === 1, 'live chart reveals an exact hover value');
      assert(await liveTooltip.evaluate(el => { const r=el.getBoundingClientRect(); return r.left>=0 && r.right<=innerWidth && r.top>=0; }), 'live chart tooltip stays inside the viewport');
      await liveChart.focus();
      await page.keyboard.press('ArrowLeft');
      assert(await page.locator('.resource-card .chart-tooltip.is-visible').count() === 1, 'live chart supports keyboard inspection');
      await page.locator('.live-data > summary').click();
      await page.locator('.live-data a[hx-get]').click();
      await page.waitForFunction(() => document.querySelectorAll('.sample-data-table tbody tr').length > 0);
      const sampleRows = await page.locator('.sample-data-table tbody tr').count();
      assert(sampleRows >= Number(seed.samples || 24) && sampleRows <= 180, 'on-demand bounded live samples');
      await page.locator('.live-data > summary').click();
      await page.locator('#traffic-range-7d').focus();
      await page.keyboard.press('Enter');
      await page.waitForFunction(() => document.querySelector('#traffic-range-7d')?.getAttribute('aria-current') === 'true');
      assert(await page.locator('#traffic-range-7d').evaluate(el => el === document.activeElement), 'chart range swap retains keyboard focus');
      await page.locator('#traffic-data > summary').click();
      assert(await page.locator('#traffic-data tbody tr').count() === 7, 'chart exposes each daily bucket without hover');
      await page.locator('#chart-card .chart').hover({ position: { x: 320, y: 100 } });
      await page.waitForFunction(() => document.querySelector('#chart-card .chart-tooltip.is-visible')?.textContent.trim().length > 0);
      const trafficTooltip = page.locator('#chart-card .chart-tooltip.is-visible');
      assert(await trafficTooltip.count() === 1, 'traffic chart reveals the nearest bucket on hover');
      assert(await trafficTooltip.evaluate(el => { const r=el.getBoundingClientRect(); return r.left>=0 && r.right<=innerWidth && r.top>=0; }), 'traffic tooltip stays inside the viewport');
    }
    if (['10.4', '10.4-backups', 'final'].includes(suite)) {
      stage = 'backup creation and restore preview';
      await goto('/backups?create=1');
      assert(await page.locator('[data-telegram-ready="true"]').count() === 0, 'unconfigured Telegram is not ready');
      await submit(page.locator('form[action="/backups/create"]'));
      const archive = page.locator('.backup-archives tbody tr').first();
      assert(await archive.count() === 1, 'created archive is listed');
      await archive.locator('a[href*="?restore="]').click();
      await submit(page.locator('form[action="/backups/restore"]'));
      assert(await page.locator('.backup-review').count() === 1, 'restore requires review');
      await submit(page.locator('form[action="/backups/restore/cancel"]'));
      assert(await page.locator('.backup-pending').count() === 0, 'cancelled preview is not a pending restore');
      stage = 'schedule validation and disabled edit';
      await goto('/backups?schedule=new');
      await page.locator('#sch-name').fill('Browser schedule');
      await page.locator('#sch-kind').selectOption('interval');
      await page.locator('#sch-enabled').selectOption('0');
      await page.locator('#sch-interval').fill('bad-hours');
      await page.locator('#sch-retention').fill('bad-retention');
      stage = 'schedule invalid submit';
      await submit(page.locator('[data-sched-form]'));
      assert(await page.locator('#sch-interval').inputValue() === 'bad-hours', 'schedule interval survives invalid submission');
      assert(await page.locator('#sch-retention').inputValue() === 'bad-retention', 'schedule retention survives invalid submission');
      await page.locator('#sch-interval').fill('6');
      await page.locator('#sch-retention').fill('2');
      stage = 'schedule corrected submit';
      await submit(page.locator('[data-sched-form]'));
      const scheduleRow = page.locator('tbody tr').filter({ hasText: 'Browser schedule' });
      stage = 'schedule open edit';
      await scheduleRow.locator('a[href*="?schedule="]').click();
      assert(await page.locator('#sch-enabled').inputValue() === '0', 'disabled schedule stays disabled in editor');
      const nativeContext = await browser.newContext({ javaScriptEnabled: false });
      await nativeContext.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }]);
      const nativePage = await nativeContext.newPage();
      stage = 'schedule native edit load';
      await nativePage.goto(page.url());
      await nativePage.locator('#sch-name').fill('Browser schedule edited');
      stage = 'schedule native edit submit';
      await Promise.all([nativePage.waitForNavigation(), nativePage.locator('[data-sched-form] button[type="submit"]').click()]);
      stage = 'schedule native saved result';
      assert(new URL(nativePage.url()).pathname === '/backups', 'native schedule update redirects successfully');
      await nativePage.locator('tbody tr').filter({ hasText: 'Browser schedule edited' }).locator('a[href*="?schedule="]').click();
      stage = 'schedule native status';
      assert(await nativePage.locator('#sch-enabled').inputValue() === '0', 'native save preserves paused schedule');
      await nativeContext.close();
    }
    const adminRoutes = ['/admins', '/tokens', '/webhooks', '/audit'];
    if (['10.4', '10.4-settings', 'final'].includes(suite)) {
      stage = 'settings dirty and validation';
      await goto('/settings');
      const settings = page.locator('[data-settings-form]');
      assert(await settings.locator('.settings-field').count() === 34, 'all registry settings have one editor');
      await page.locator('#s-node_id').focus();
      for (let i = 0; i < 9; i++) {
        await page.keyboard.press('Tab');
        await page.waitForFunction(() => {
          const active = document.activeElement, bar = document.querySelector('.settings-savebar').getBoundingClientRect();
          const box = active.getBoundingClientRect();
          return active.closest('.settings-savebar') || box.bottom <= bar.top || box.top >= bar.bottom;
        });
      }
      await page.locator('#s-node_id').fill('Browser node');
      await page.locator('details').filter({ has: page.locator('#s-rate_limit') }).locator('summary').click();
      await page.locator('#s-rate_limit').fill('invalid-rate');
      await page.locator('#s-mtu').fill('invalid-mtu');
      assert(await settings.evaluate(el => el.classList.contains('is-dirty')), 'settings track unsaved edits');
      await submit(settings);
      assert(await page.locator('#s-node_id').inputValue() === 'Browser node', 'settings preserve identity on error');
      assert(await page.locator('#s-rate_limit').inputValue() === 'invalid-rate' && await page.locator('#s-mtu').inputValue() === 'invalid-mtu', 'all invalid setting values remain exact');
      assert(await page.locator('#form-errors').evaluate(el => el === document.activeElement), 'settings error summary receives focus');
      await page.locator('#s-mtu').fill('1420');
      await page.locator('details').filter({ has: page.locator('#s-rate_limit') }).evaluate(el => { el.open = true; });
      await page.locator('#s-rate_limit').fill('120');
      await submit(page.locator('[data-settings-form]'));
      assert(await page.locator('#s-node_id').inputValue() === 'Browser node', 'settings save completed');
      assert(!await page.locator('[data-settings-form]').evaluate(el => el.classList.contains('is-dirty')), 'saved settings are clean');
    }
    if (['10.4', '10.4-admin', 'final'].includes(suite)) {
      stage = 'administrator create and wildcard permissions';
      await goto('/admins');
      await page.locator('#admin-create > summary').click();
      await page.locator('#ops-username').fill('browser-admin');
      await page.locator('#ops-password').fill(require('node:crypto').randomBytes(24).toString('hex'));
      const family = page.locator('#admin-create .ops-scope-group').filter({ has: page.locator('input[value="users.*"]') });
      await family.locator('summary').click();
      await family.locator('input[value="users.*"]').check();
      await submit(page.locator('form[action="/admins/create"]'));
      await page.locator('.ops-account').filter({ hasText: 'browser-admin' }).locator('a[href*="?edit="]').click();
      const adminEdit = new URL(page.url()).pathname + new URL(page.url()).search;
      assert(await page.locator('input[value="users.*"]').isChecked(), 'stored family wildcard remains selected');
      await submit(page.locator('form[action$="/permissions"]'));
      await goto(adminEdit);
      assert(await page.locator('input[value="users.*"]').isChecked(), 'wildcard survives permission save');
      adminRoutes.push(adminEdit);
      stage = 'token invalid expiry and one-time result';
      await goto('/tokens');
      await page.locator('#token-create > summary').click();
      await page.locator('#ops-name').fill('Browser token');
      await page.locator('#ops-expires_days').fill('invalid-days');
      const tokenFamily = page.locator('#token-create .ops-scope-group').filter({ has: page.locator('input[value="users.*"]') });
      await tokenFamily.locator('summary').click();
      await tokenFamily.locator('input[value="users.*"]').check();
      await submit(page.locator('form[action="/tokens/create"]'));
      assert(await page.locator('#ops-name').inputValue() === 'Browser token' && await page.locator('#ops-expires_days').inputValue() === 'invalid-days', 'token form retains invalid expiry and name');
      assert(await page.locator('input[value="users.*"]').isChecked(), 'token validation retains scopes');
      await page.locator('#ops-expires_days').fill('7');
      await submit(page.locator('form[action="/tokens/create"]'));
      assert((await page.locator('#token-once').inputValue()).length > 0, 'token secret appears once');
      await goto('/tokens');
      assert(await page.locator('#token-once').count() === 0, 'token secret is not redisplayed');
      stage = 'webhook create and disabled edit';
      await goto('/webhooks');
      await page.locator('#webhook-create > summary').click();
      await page.locator('#ops-url').fill('https://example.com/events');
      await page.locator('input[name="events"][value="user.created"]').check();
      await submit(page.locator('form[action="/webhooks/create"]'));
      assert((await page.locator('#hook-secret').inputValue()).length > 0, 'webhook signing secret appears once');
      await goto('/webhooks');
      const hookLink = page.locator('.ops-account a[href^="/webhooks/"]').first();
      const hookPath = await hookLink.getAttribute('href');
      await hookLink.click();
      await page.locator('input[name="enabled"]').uncheck();
      await submit(page.locator('form[action$="/update"]'));
      await goto(hookPath);
      assert(!await page.locator('input[name="enabled"]').isChecked(), 'unchecked webhook state persists');
      assert(await page.locator('#hook-secret').count() === 0, 'webhook secret is not redisplayed');
      adminRoutes.push(hookPath);
      stage = 'audit filtering';
      await goto('/audit');
      await page.locator('#audit-action').fill('admins.');
      await Promise.all([page.waitForNavigation(), page.locator('.ops-audit-filter button[type="submit"]').click()]);
      assert(await page.locator('#audit-action').inputValue() === 'admins.', 'audit retains filter');
    }
    if (suite === 'final') {
      const fragments = [];
      for (const [name, path] of [['live', '/dashboard/live'], ['samples', '/dashboard/live?view=history'], ['traffic', '/dashboard/chart?range=30d'], ['user-form', '/users/new']]) {
        const response = await page.request.get(seed.url + path, {headers: {'HX-Request': 'true'}});
        assert(response.status() === 200, 'measured fragment is available');
        const body = await response.body();
        fragments.push({name, raw: body.length, gzip: require('node:zlib').gzipSync(body).length});
      }
      console.log('Observed fragment sizes (bytes): ' + JSON.stringify(fragments));
    }
    const operationalRoutes = ['/interfaces', '/interfaces/new', '/interfaces/' + seed.iface + '/edit', '/plans', '/plans/new', '/plans/' + seed.plan + '/edit'];
    const userRoutes = ['/users', '/users/new', '/users/' + seed.user, '/users/' + seed.user + '/edit', '/users/bulk'];
    const dashboardRoutes = ['/dashboard', '/dashboard?range=7d', '/dashboard?range=30d'];
    const backupRoutes = ['/backups', '/backups?create=1', '/backups?schedule=new'];
    const routes = suite === '10.2' ? operationalRoutes : suite === '10.3-users' ? userRoutes : suite === '10.3-dashboard' ? dashboardRoutes : suite === '10.3' ? [...userRoutes, ...dashboardRoutes] : suite === '10.4-backups' ? backupRoutes : suite === '10.4-settings' ? ['/settings'] : suite === '10.4-admin' ? adminRoutes : suite === '10.4' ? [...backupRoutes, '/settings', ...adminRoutes] : [...operationalRoutes, ...userRoutes, ...dashboardRoutes, ...backupRoutes, '/settings', ...adminRoutes];
    let cells = 0;
    let maxHTMLGzip = 0;
    const performanceSummary = {};
    const failures=[];
    for (const lang of ['en', 'fa']) {
      await locale(lang);
      for (const theme of ['light', 'dark']) {
        await context.addCookies([{ name: 'wg_theme', value: theme, url: seed.url }]);
        for (const width of suite === 'final' ? [320, 360, 390, 430, 768, 799, 800, 959, 960, 961, 1024, 1280, 1440, 1920, 2560, 3440] : [390, 1440]) {
          if(!qa.variant(lang,theme,width))continue;
          await page.setViewportSize({ width, height: 900 });
          for (let index = 0; index < routes.length; index++) {
            if (process.env.WG_TEST_UI_PAGE && routes[index] !== process.env.WG_TEST_UI_PAGE) continue;
            stage = suite + ' composition ' + index + ' ' + lang + '/' + theme + '/' + width;
            try {
            await goto(routes[index]);
            if(routes[index].startsWith('/dashboard'))assert(await page.locator('#telemetry-card').getAttribute('data-health')==='healthy','normal dashboard state stays healthy');
            assert(await page.locator('html').getAttribute('dir') === (lang === 'fa' ? 'rtl' : 'ltr'), 'page direction');
            assert(await page.locator('html').getAttribute('data-theme') === theme, 'page theme');
            const fits = await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
            if (!fits) console.log('overflow geometry: ' + JSON.stringify(await page.evaluate(() => [...document.querySelectorAll('main,main section,main table,main .table-wrap,main .card,main .backup-workspace')].filter(el => {const r=el.getBoundingClientRect();return r.right>innerWidth+1||r.left< -1;}).map(el => ({ tag: el.tagName, classes: el.className, width: Math.round(el.getBoundingClientRect().width),right:Math.round(el.getBoundingClientRect().right),columns:getComputedStyle(el).gridTemplateColumns })).slice(0, 12))));
            assert(fits, 'page viewport overflow');
            assert(await page.locator('main h1').count() === 1, 'single primary page heading');
            assert(await page.locator('input:not([type="hidden"]),select,textarea').evaluateAll(elements => elements.every(el => el.labels?.length || el.getAttribute('aria-label') || el.getAttribute('aria-labelledby'))), 'form controls have names');
            maxHTMLGzip = Math.max(maxHTMLGzip, require('node:zlib').gzipSync(await page.content()).length);
            if (suite === 'final' || (suite === '10.6' && ((lang === 'en' && theme === 'light' && width === 1440) || (lang === 'fa' && theme === 'dark' && width === 390)))) {
              qa.merge(performanceSummary, await qa.measure(page, stage));
              await qa.scan(page, stage, width === 390 || width === 1440);
            }
            if (process.env.WG_UI_SCREENSHOT_DIR && ['/users', '/users/new', '/dashboard', '/interfaces', '/interfaces/new', '/plans', '/plans/new', '/backups', '/backups?schedule=new', '/settings', '/admins', '/audit', '/tokens', '/webhooks'].includes(routes[index]) && ((width === 1440 && lang === 'en' && theme === 'light') || (width === 390 && lang === 'fa' && theme === 'dark') || (suite === 'final' && width === 320 && lang === 'en' && theme === 'light' && routes[index] === '/dashboard'))) {
              const fs = require('node:fs'), path = require('node:path');
              fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR, { recursive: true });
              await page.screenshot({ path: path.join(process.env.WG_UI_SCREENSHOT_DIR, suite + '-' + index + '-' + width + '.png'), fullPage: true });
            }
            } catch(error) {
              if(suite!=='final')throw error;
              failures.push(stage);
              console.log('Matrix failure '+stage+': '+(error.message.startsWith('contract:')?error.message:'browser operation failed; sensitive details suppressed'));
            }
            cells++;
            if(suite==='final' && cells%128===0)console.log('Main matrix progress: '+cells+' cells checked');
          }
        }
      }
    }
    assert(runtimeErrors === 0, 'page JavaScript errors');
    console.log('Checked ' + engine + ' ' + browser.version() + ' Phase ' + suite + ': ' + cells + ' composition cells; max rendered HTML gzip ' + maxHTMLGzip + ' B');
    if (Object.keys(performanceSummary).length) console.log('Observed page maxima before accessibility instrumentation: '+JSON.stringify(performanceSummary));
    if (suite === 'final' && group==='all') {
      try{await require('./test-web-states.cjs')({browser,seed,final:true});}catch(error){failures.push('state matrix'); console.log(error.message);}
      try{await require('./test-web-interactions.cjs')({browser,seed,engine});}catch(error){failures.push('interactions'); console.log(error.message.startsWith('contract:')?error.message:'interaction operation failed');}
    }
    assert(failures.length===0,'matrix failures: '+failures.join('; '));
    if(suite==='final')assert(cells>0,'matrix filters selected no cells');
    console.log('PASS Phase '+suite+' '+engine+' '+browser.version());
    await context.close();
  } finally { await browser.close(); }
})().catch(error => {
  const category = error.message.includes('strict mode violation') ? 'ambiguous locator' : error.message.includes('not a valid URL') ? 'invalid request URL' : error.message.includes('has been closed') ? 'browser closed' : error.name === 'TimeoutError' ? 'timeout' : 'browser operation failed';
  console.error('FAIL ' + stage + (error.message.startsWith('contract:') ? ': ' + error.message : ' (' + category + '; sensitive details suppressed)'));
  process.exitCode = 1;
});
