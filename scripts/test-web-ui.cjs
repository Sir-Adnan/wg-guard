// Optional development-only browser checks, invoked by TestBrowserFoundation.
// Credentials belong to the ephemeral Go test server; never print payloads/URLs.
const playwright = require(process.env.WG_TEST_PLAYWRIGHT || 'playwright');
const assert = (ok, message) => { if (!ok) throw new Error('contract: ' + message); };
let stage = 'launch';
(async () => {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const seed = JSON.parse(input);
  const engine = process.env.WG_TEST_BROWSER_ENGINE || 'chromium';
  const browser = await playwright[engine].launch({ ...(engine === 'chromium' ? {channel: process.env.WG_TEST_BROWSER_CHANNEL || 'chrome'} : {}), headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: 'reduce' });
    await context.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }]);
    const page = await context.newPage();
    const runtimeErrors = [];
    page.on('pageerror', error => runtimeErrors.push(error.name + ': ' + error.message.replaceAll(seed.session, '[session]').replaceAll(seed.sub, '[subscription]')));
    const assertOpenMenuAligned = async (anchor, message) => {
      const geometry = await anchor.evaluate(el => {
        const trigger = el.querySelector(':scope > button');
        const menu = el.querySelector(':scope > .menu');
        const triggerRect = trigger.getBoundingClientRect();
        const menuRect = menu.getBoundingClientRect();
        const style = getComputedStyle(menu);
        return {
          dir: document.documentElement.dir,
          open: menu.classList.contains('is-open'),
          visible: style.visibility === 'visible' && Number(style.opacity) > 0 && menuRect.width > 0 && menuRect.height > 0,
          visibility: style.visibility,
          opacity: style.opacity,
          triggerLeft: triggerRect.left,
          triggerRight: triggerRect.right,
          menuLeft: menuRect.left,
          menuRight: menuRect.right,
          viewportWidth: innerWidth,
        };
      });
      const edgeGap = geometry.dir === 'rtl'
        ? Math.abs(geometry.menuLeft - geometry.triggerLeft)
        : Math.abs(geometry.menuRight - geometry.triggerRight);
      assert(geometry.open && geometry.visible, message + ' must be visibly open (' + JSON.stringify(geometry) + ')');
      assert(geometry.menuLeft >= 0 && geometry.menuRight <= geometry.viewportWidth, message + ' must stay inside viewport');
      assert(edgeGap <= 1, message + ' must align to its trigger (' + JSON.stringify(geometry) + ')');
    };
    stage = 'mobile drawer focus';
    await page.goto(seed.url + '/users');
    await page.locator('#btn-drawer').click();
    assert(await page.evaluate(() => document.querySelector('#sidebar').contains(document.activeElement)), 'drawer must receive focus');
    assert(await page.locator('.main').evaluate(el => el.inert), 'modal drawer background must be inert');
    await page.keyboard.press('Shift+Tab');
    assert(await page.evaluate(() => document.querySelector('#sidebar').contains(document.activeElement)), 'drawer must contain reverse Tab');
    await page.keyboard.press('Escape');
    assert(await page.locator('#btn-drawer').evaluate(el => el === document.activeElement), 'drawer returns focus');
    assert(!await page.locator('.main').evaluate(el => el.inert), 'drawer releases background');
    stage = 'resize and desktop menu keyboard';
    await page.locator('#btn-drawer').click();
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.waitForFunction(() => !document.querySelector('.main').inert);
    assert(!await page.locator('.main').evaluate(el => el.inert), 'desktop resize releases mobile modal state');
    const theme = page.locator('[data-theme-menu] > button');
    await theme.focus();
    await page.keyboard.press('ArrowDown');
    assert(await page.locator('[data-theme-choice="light"]').evaluate(el => el === document.activeElement), 'menu ArrowDown opens and focuses first item');
    await assertOpenMenuAligned(page.locator('[data-theme-menu]'), 'desktop theme menu');
    assert(!await page.locator('.nav-close').isVisible(), 'desktop sidebar close control stays hidden');
    await page.keyboard.press('End');
    assert(await page.locator('[data-theme-choice="system"]').evaluate(el => el === document.activeElement), 'menu End focuses last item');
    await page.keyboard.press('Escape');
    assert(await theme.evaluate(el => el === document.activeElement), 'menu returns focus');
    stage = 'dynamic dialog behavior';
    await page.evaluate(() => {
      const host = document.createElement('div');
      host.innerHTML = '<button id="test-open" data-open-modal="test-dialog">Open</button><dialog id="test-dialog"><button data-close-modal>Close</button></dialog>';
      document.querySelector('#main').append(host);
    });
    await page.locator('#test-open').click();
    await page.locator('#test-dialog [data-close-modal]').click();
    assert(!await page.locator('#test-dialog').evaluate(el => el.open), 'dialogs inserted after load close through delegation');
    assert(await page.locator('#test-open').evaluate(el => el === document.activeElement), 'dialog restores invoker focus');
    stage = 'menu dialog focus return';
    await page.goto(seed.url + '/users');
    const detailHref = await page.locator('.row-link[href^="/users/"]').first().getAttribute('href');
    await page.goto(seed.url + detailHref);
    const actionsTrigger = page.locator('.page-head .menu-anchor > button');
    stage = 'menu dialog focus return: open renew';
    await actionsTrigger.focus();
    await page.keyboard.press('ArrowDown');
    assert(await page.locator('[data-open-modal="renew-modal"]').evaluate(el => el === document.activeElement), 'keyboard menu opens on renew action');
    await page.keyboard.press('Enter');
    assert(await page.locator('#renew-modal').evaluate(el => el.open), 'renew action opens dialog');
    stage = 'menu dialog focus return: close renew';
    await page.locator('#renew-modal [data-close-modal]').first().click();
    assert(await actionsTrigger.isVisible() && await actionsTrigger.evaluate(el => el === document.activeElement), 'renew dialog returns focus to visible menu trigger');
    stage = 'menu dialog focus return: open delete';
    await actionsTrigger.click();
    await page.locator('.page-head .menu form[data-confirm] button').last().click();
    assert(await page.locator('#confirm-dialog').evaluate(el => el.open), 'menu delete opens confirmation dialog');
    stage = 'menu dialog focus return: close delete';
    await page.locator('#confirm-dialog [data-close-modal]').first().click();
    assert(await actionsTrigger.isVisible() && await actionsTrigger.evaluate(el => el === document.activeElement), 'confirmation returns focus to visible menu trigger');
    stage = 'submission guard and recovery';
    await page.evaluate(() => {
      const form = document.createElement('form');
      form.id = 'test-submit'; form.method = 'post'; form.action = '/test-slow';
      form.innerHTML = '<input name="_csrf" value="' + document.querySelector('meta[name="csrf-token"]').content + '"><button name="choice" value="keep">Send</button>';
      document.querySelector('#main').append(form);
    });
    let posts = [];
    await page.route('**/test-slow', async route => {
      posts.push(route.request().postData());
      await new Promise(resolve => setTimeout(resolve, 180));
      await route.fulfill({ status: 204 });
    });
    await page.locator('#test-submit button').click({ noWaitAfter: true });
    await page.evaluate(() => document.querySelector('#test-submit').requestSubmit(document.querySelector('#test-submit button')));
    await page.waitForTimeout(250);
    assert(posts.length === 1 && posts[0].includes('choice=keep'), 'pending guard preserves submitter and prevents duplicate POST');
    await page.evaluate(() => window.dispatchEvent(new Event('pageshow')));
    assert(!await page.locator('#test-submit').evaluate(el => el.hasAttribute('aria-busy')), 'back navigation clears pending state');
    stage = 'confirmation submitter and HTMX failure recovery';
    await page.evaluate(() => {
      const form = document.querySelector('#test-submit');
      form.setAttribute('data-confirm', '');
      form.dataset.confirmTitle = 'Confirm test'; form.dataset.confirmLabel = 'Continue';
    });
    await page.locator('#test-submit button').click();
    await page.locator('[data-confirm-ok]').click({ noWaitAfter: true });
    await page.waitForTimeout(250);
    assert(posts.length === 2 && posts[1].includes('choice=keep'), 'confirmation preserves the initiating submit button');
    await page.evaluate(() => {
      window.dispatchEvent(new Event('pageshow'));
      const form = document.querySelector('#test-submit');
      form.removeAttribute('data-confirm');
      form.setAttribute('hx-post', '/test-hx-fail'); form.setAttribute('hx-swap', 'none');
      window.htmx.process(form);
    });
    await page.route('**/test-hx-fail', async route => {
      await new Promise(resolve => setTimeout(resolve, 120));
      await route.fulfill({ status: 500, body: 'test failure' });
    });
    await page.locator('#test-submit button').click();
    await page.waitForFunction(() => !document.querySelector('#test-submit').hasAttribute('aria-busy'));
    assert(!await page.locator('#test-submit button').getAttribute('aria-disabled'), 'HTMX error releases submitter');
    assert(await page.locator('.toast--err').count() > 0, 'HTMX error has visible feedback');
    stage = 'locale/theme/viewport representative compositions';
    for (const locale of ['fa', 'en']) {
      await page.request.post(seed.url + '/prefs/locale', { form: { locale, _csrf: await page.locator('meta[name="csrf-token"]').getAttribute('content') } });
      for (const mode of ['light', 'dark']) {
        await context.addCookies([{ name: 'wg_theme', value: mode, url: seed.url }]);
        for (const width of [390, 1440]) {
          await page.setViewportSize({ width, height: 900 });
          for (const path of ['/users', '/users/new']) {
            await page.goto(seed.url + path);
            assert(await page.locator('html').getAttribute('dir') === (locale === 'fa' ? 'rtl' : 'ltr'), 'locale direction');
            assert(await page.locator('html').getAttribute('data-theme') === mode, 'theme persists across layouts');
            assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'representative surface has viewport overflow');
            if (path === '/users') {
              await page.waitForFunction(() => document.documentElement.dataset.ui === 'ready');
              await page.locator('[data-theme-menu] > button').click();
              if (width === 1440) await assertOpenMenuAligned(page.locator('[data-theme-menu]'), locale + ' desktop theme menu');
              await page.keyboard.press('Escape');
            }
          }
        }
      }
    }
    stage = 'anonymous login/public/error compositions';
    const anonymous = await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: 'reduce' });
    const publicPage = await anonymous.newPage();
    publicPage.on('pageerror', error => runtimeErrors.push(error.name + ': ' + error.message.replaceAll(seed.sub, '[subscription]')));
    for (const locale of ['fa', 'en']) {
      for (const mode of ['light', 'dark']) {
        await anonymous.addCookies([
          { name: 'wg_locale', value: locale, url: seed.url },
          { name: 'wg_theme', value: mode, url: seed.url },
        ]);
        for (const width of [390, 1440]) {
          await publicPage.setViewportSize({ width, height: 900 });
          const loginResponse = await publicPage.goto(seed.url + '/login');
          assert(loginResponse.status() === 200 && new URL(publicPage.url()).pathname === '/login', 'anonymous login remains a public page');
          assert(await publicPage.locator('form[action="/login"]').isVisible(), 'anonymous login form is visible');
          assert(await publicPage.locator('form[action="/login"] .input-adj').evaluateAll(groups => groups.every(group => {
            const input = group.querySelector('input'), action = group.querySelector('button');
            const inputRect = input.getBoundingClientRect(), actionRect = action.getBoundingClientRect();
            return inputRect.right <= actionRect.left + 1 || actionRect.right <= inputRect.left + 1;
          })), 'login field action never overlaps its text input');
          assert(await publicPage.locator('html').getAttribute('dir') === (locale === 'fa' ? 'rtl' : 'ltr'), 'anonymous login direction');
          assert(await publicPage.locator('html').getAttribute('data-theme') === mode, 'anonymous login theme');
          assert(await publicPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'anonymous login viewport overflow');
          for (const path of [seed.sub + '?lang=' + locale, '/not-a-page?lang=' + locale]) {
            const response = await publicPage.goto(seed.url + path);
            assert(response.status() === (path.startsWith('/not-a-page') ? 404 : 200), 'public surface status');
            assert(await publicPage.locator('html').getAttribute('dir') === (locale === 'fa' ? 'rtl' : 'ltr'), 'public locale direction');
            assert(await publicPage.locator('html').getAttribute('data-theme') === mode, 'public theme persists');
            assert(await publicPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'public surface has viewport overflow');
          }
        }
      }
    }
    await anonymous.close();
    stage = '320px shell and reduced motion';
    await page.setViewportSize({ width: 320, height: 568 });
    await page.goto(seed.url + '/users');
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), '320px shell overflows');
    assert(await page.locator('.shell').evaluate(el => getComputedStyle(el).transitionDuration.split(',').every(v => parseFloat(v) <= 0.01)), 'reduced motion disables shell transition');
    assert(runtimeErrors.length === 0, 'shared UI raised a runtime error: ' + runtimeErrors.join('; '));
    // Optional disposable design-review images; only synthetic list data, no open secret views.
    if (process.env.WG_UI_SCREENSHOT_DIR) {
      const fs = require('node:fs'), path = require('node:path');
      fs.mkdirSync(process.env.WG_UI_SCREENSHOT_DIR, { recursive: true });
      for (const [width, theme, locale] of [[1440, 'light', 'en'], [390, 'dark', 'fa']]) {
        await page.goto(seed.url + '/users');
        await page.request.post(seed.url + '/prefs/locale', { form: { locale, _csrf: await page.locator('meta[name="csrf-token"]').getAttribute('content') } });
        await context.addCookies([{ name: 'wg_theme', value: theme, url: seed.url }]);
        await page.setViewportSize({ width, height: 900 });
        await page.goto(seed.url + '/users');
        await page.screenshot({ path: path.join(process.env.WG_UI_SCREENSHOT_DIR, 'shell-' + width + '-' + locale + '-' + theme + '.png') });
      }
    }
    console.log('PASS ' + engine + ' ' + browser.version() + ' foundation: focus, visible aligned menus, menu-to-dialog return, dynamic dialog, submit/recovery, authenticated locale mutation, anonymous login/public/error, fa/en × light/dark × 390/1440; 320 shell; reduced motion');
  } finally { await browser.close(); }
})().catch(error => {
  console.error('FAIL ' + stage + (error.message.startsWith('contract:') ? ': ' + error.message : ' (browser operation failed; sensitive details suppressed)'));
  process.exitCode = 1;
});
