// Supplemental development-only interaction checks against the disposable seed.
// No URLs, DOM contents, field values, configs, QR images or credentials are logged.
const assert = (ok, message) => { if (!ok) throw new Error('contract: ' + message); };

module.exports = async ({ browser, seed, engine }) => {
  const engineName = ['chromium', 'firefox', 'webkit'].includes(engine) ? engine : 'browser';
  const failures = [];
  let step = 'setup', cells = 0, targetChecks = 0, runtimeErrors = 0;
  const touchOptions = { hasTouch: true, ...(engine === 'firefox' ? {} : { isMobile: true }) };
  const ready = async page => {
    await page.waitForFunction(() => document.documentElement.dataset.ui === 'ready');
    await page.evaluate(async () => {
      await document.fonts.ready;
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    });
  };
  const navigate = async (page, path) => {
    const response = await page.goto(seed.url + path);
    assert(response?.status() === 200, step + ': page status');
    await ready(page);
  };
  const instrument = page => {
    page.setDefaultTimeout(12000);
    page.on('pageerror', () => runtimeErrors++);
  };
  const noOverflow = async page => assert(await page.evaluate(() =>
    document.documentElement.scrollWidth <= innerWidth + 1), step + ': horizontal overflow');
  const fits = async (page, selector) => assert(await page.locator(selector).evaluate(el => {
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && r.left >= -1 && r.top >= -1 && r.right <= innerWidth + 1 && r.bottom <= innerHeight + 1;
  }), step + ': overlay outside viewport');
  const focusExposed = async (page, selector) => assert(await page.locator(selector).evaluate(el => {
    const r = el.getBoundingClientRect();
    if (el !== document.activeElement || r.top < -1 || r.bottom > innerHeight + 1) return false;
    // Testing the center avoids shadows/borders while detecting sticky chrome occlusion.
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return hit === el || el.contains(hit);
  }), step + ': focused control covered by chrome');

  const reducedMotion = async (page, scope = 'body') => {
    const count = await page.locator(scope).evaluate(root => {
      const visible = el => el.getClientRects().length && getComputedStyle(el).visibility !== 'hidden';
      return [root, ...root.querySelectorAll('*')].filter(visible).filter(el => {
        const style = getComputedStyle(el);
        return [style.transitionDuration, style.animationDuration].some(value =>
          value.split(',').some(duration => parseFloat(duration) > 0.001));
      }).length;
    });
    assert(count === 0, step + ': reduced motion leaves active transitions or animations');
  };

  // Effective target geometry uses a checkbox/radio's actual associated label,
  // never its non-clickable parent padding. Native modal background is excluded.
  const hitTargets = async (page, label, scope = 'body') => {
    const result = await page.locator(scope).evaluate(root => {
      const modal = document.querySelector('dialog:modal');
      const candidates = [...root.querySelectorAll('button,a[href],input:not([type="hidden"]),select,textarea,summary')];
      const visible = el => {
        // The skip link is intentionally absent from the touch surface until focus.
        if (el.classList.contains('sr-only') && el !== document.activeElement) return false;
        const style = getComputedStyle(el);
        return el.getClientRects().length > 0 && style.visibility !== 'hidden' && style.display !== 'none' &&
          !el.closest('[inert]') && !el.disabled && el.getAttribute('aria-disabled') !== 'true' &&
          (!modal || modal.contains(el));
      };
      const standalone = '.btn,.icon-btn,.row-link,.entity-link,.brand,.locale-choice';
      const isProseLink = el => el.tagName === 'A' && !el.matches(standalone) &&
        !el.closest('nav,.menu,.pagination,.toolbar,.inline-actions,.form-actions,.settings-nav') &&
        el.closest('p,.hint,.section-description,.callout,dd,li') && getComputedStyle(el).display === 'inline';
      const identify = (el, index) => {
        // Only known static hook names are emitted; all attribute values are omitted.
        const hooks = [
          ['#btn-drawer', 'drawer-trigger'], ['[data-close-nav]', 'drawer-close'],
          ['.nav a', 'navigation-link'], ['.brand', 'brand-link'],
          ['.locale-choice', 'locale-control'], ['[data-theme-choice]', 'theme-choice'],
          ['[data-theme-menu] > button', 'theme-trigger'], ['[data-calendar]', 'calendar-trigger'],
          ['.cal-day', 'calendar-day'], ['[data-cal-prev]', 'calendar-previous'],
          ['[data-cal-next]', 'calendar-next'], ['[data-cal-clear]', 'calendar-clear'],
          ['[data-qr]', 'qr-trigger'], ['[data-close-modal]', 'dialog-close'],
          ['.chip-btn', 'preset-chip'], ['.check-row input', 'form-check-row'],
          ['.settings-secret-clear input', 'settings-secret-clear'],
          ['.row-link', 'entity-row-link'], ['.entity-link', 'entity-name-link'],
          ['.settings-savebar button', 'settings-save'], ['.settings-nav a', 'settings-section-link'],
          ['.check-label input', 'labeled-checkbox'], ['.checkbox', 'checkbox'],
        ];
        const match = hooks.find(([selector]) => el.matches(selector));
        return (match ? match[1] : el.tagName.toLowerCase()) + '[' + index + ']';
      };
      let checked = 0;
      const short = [];
      candidates.forEach((el, index) => {
        if (!visible(el) || isProseLink(el)) return;
        checked++;
        const areas = [el];
        if (el.matches('input[type="checkbox"],input[type="radio"]')) areas.push(...el.labels || []);
        const rects = areas.filter(visible).map(area => area.getBoundingClientRect());
        if (rects.some(r => r.width >= 43.99 && r.height >= 43.99)) return;
        const best = rects.sort((a, b) => b.width * b.height - a.width * a.height)[0];
        short.push({ control: identify(el, index), width: Math.round(best?.width * 10) / 10 || 0, height: Math.round(best?.height * 10) / 10 || 0 });
      });
      return { checked, short };
    });
    targetChecks += result.checked;
    if (result.short.length) {
      failures.push(label + ': ' + result.short.map(item => item.control + ' ' + item.width + 'x' + item.height).join(', '));
      console.log('Touch target findings ' + engineName + ' ' + label + ': ' + JSON.stringify(result.short));
    }
  };

  try {
    for (const lang of ['fa', 'en']) {
      for (const theme of ['light', 'dark']) {
        const context = await browser.newContext({ ...touchOptions, viewport: { width: 390, height: 844 }, reducedMotion: 'reduce' });
        try {
          await context.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }, { name: 'wg_theme', value: theme, url: seed.url }]);
          const page = await context.newPage();
          instrument(page);
          const preference = await page.request.post(seed.url + '/prefs/locale', { form: { locale: lang, _csrf: seed.csrf } });
          assert(preference.ok(), 'touch locale preference');
          for (const viewport of [{ width: 390, height: 844 }, { width: 844, height: 390 }]) {
            const cell = lang + '/' + theme + '/' + viewport.width + 'x' + viewport.height;
            await page.setViewportSize(viewport);
            step = cell + ' drawer';
            await navigate(page, '/users');
            assert(await page.locator('html').getAttribute('dir') === (lang === 'fa' ? 'rtl' : 'ltr'), step + ': direction');
            assert(await page.locator('html').getAttribute('data-theme') === theme, step + ': theme');
            await noOverflow(page);
            await hitTargets(page, cell + ' users');
            await page.locator('#btn-drawer').tap();
            assert(await page.locator('#sidebar').evaluate(el => el.classList.contains('is-open') && el.contains(document.activeElement)), step + ': open and focus');
            assert(await page.locator('.main').evaluate(el => el.inert), step + ': inert background');
            await hitTargets(page, cell + ' drawer', '#sidebar');
            await reducedMotion(page, '#sidebar');
            await page.locator('[data-close-nav]').tap();
            assert(await page.locator('#btn-drawer').evaluate(el => el === document.activeElement), step + ': close returns focus');
            assert(!await page.locator('.main').evaluate(el => el.inert), step + ': close releases background');
            await page.locator('#btn-drawer').tap();
            await page.setViewportSize({ width: 1440, height: 900 });
            await page.waitForFunction(() => !document.querySelector('.main').inert && !document.querySelector('#sidebar').inert);
            assert(await page.evaluate(() => !document.body.classList.contains('nav-open') && !document.querySelector('#sidebar').hasAttribute('aria-modal') && !document.querySelector('#scrim').classList.contains('is-open')), step + ': resize clears modal state');
            await page.setViewportSize(viewport);
            await page.waitForFunction(() => document.querySelector('#sidebar').inert);

            step = cell + ' QR';
            await navigate(page, '/users/' + seed.user);
            await hitTargets(page, cell + ' user detail');
            const qr = page.locator('[data-qr]').first();
            await qr.tap();
            await page.waitForFunction(() => document.querySelector('#qr-modal')?.open && document.querySelector('#qr-img')?.naturalWidth > 0 && !document.querySelector('#qr-img').hidden);
            await fits(page, '#qr-modal');
            await hitTargets(page, cell + ' QR dialog', '#qr-modal');
            await reducedMotion(page, '#qr-modal');
            const closeQR = page.locator('#qr-modal [data-close-modal]').first();
            await closeQR.focus();
            await focusExposed(page, '#qr-modal [data-close-modal]');
            await closeQR.tap();
            assert(await page.locator('#qr-modal').evaluate(el => !el.open), step + ': closes');
            assert(await page.locator('#qr-img').evaluate(el => !el.hasAttribute('src')), step + ': clears source');
            assert(await qr.evaluate(el => el === document.activeElement), step + ': returns focus');

            step = cell + ' calendar';
            await navigate(page, '/users/new');
            await hitTargets(page, cell + ' user form');
            const trigger = page.locator('[data-calendar="#u-expires"]');
            await trigger.tap();
            await page.waitForFunction(() => document.querySelector('#date-calendar')?.classList.contains('is-open'));
            await fits(page, '#date-calendar');
            await hitTargets(page, cell + ' calendar', '#date-calendar');
            await reducedMotion(page, '#date-calendar');
            await page.locator('#date-calendar [data-cal-day][tabindex="0"]').tap();
            assert(/^\d{4}-\d{2}-\d{2}$/.test(await page.locator('#u-expires').inputValue()), step + ': selected date stored as ISO');
            assert(await trigger.evaluate(el => el.getAttribute('aria-expanded') === 'false' && el === document.activeElement), step + ': closes and returns focus');
            await trigger.tap();
            assert(await page.locator('#date-calendar [data-cal-day][aria-pressed="true"]').count() === 1, step + ': reopens selected date');
            await page.locator('#date-calendar [data-cal-clear]').tap();
            assert(await page.locator('#u-expires').inputValue() === '', step + ': clear resets date');

            step = cell + ' settings focus';
            await navigate(page, '/settings');
            await hitTargets(page, cell + ' settings');
            await page.locator('#s-session_abs').focus();
            await focusExposed(page, '#s-session_abs');
            await noOverflow(page);
            await reducedMotion(page);
            cells++;
          }
        } finally { await context.close(); }
      }
    }

    step = 'theme preferences';
    const preferences = await browser.newContext({ ...touchOptions, viewport: { width: 390, height: 844 }, colorScheme: 'dark', reducedMotion: 'reduce' });
    try {
      const page = await preferences.newPage(); instrument(page);
      await navigate(page, '/login');
      const scheme = async () => page.locator('html').evaluate(el => getComputedStyle(el).colorScheme);
      const choose = async theme => { await page.locator('[data-theme-menu] > button').tap(); await page.locator('[data-theme-choice="' + theme + '"]').tap(); };
      assert(await page.locator('html').getAttribute('data-theme') === 'light' && await scheme() === 'light', step + ': no preference defaults Light on dark OS');
      await choose('system');
      assert(await page.locator('html').getAttribute('data-theme') === null && await scheme() === 'dark', step + ': System follows dark OS');
      await page.emulateMedia({ colorScheme: 'light' });
      assert(await scheme() === 'light', step + ': System follows live light OS');
      await page.reload(); await ready(page);
      assert(await page.locator('html').getAttribute('data-theme') === null && await scheme() === 'light', step + ': saved System survives reload');
      await choose('dark');
      assert(await scheme() === 'dark', step + ': explicit Dark overrides light OS');
      await page.emulateMedia({ colorScheme: 'dark' });
      await choose('light');
      assert(await scheme() === 'light', step + ': explicit Light overrides dark OS');
      await page.reload(); await ready(page);
      assert(await page.locator('html').getAttribute('data-theme') === 'light' && await scheme() === 'light', step + ': saved Light survives reload');
      await page.locator('[data-theme-menu] > button').tap();
      await hitTargets(page, 'theme preference menu', '[data-theme-menu]');
      await reducedMotion(page, '[data-theme-menu]');
    } finally { await preferences.close(); }

    // A 1440x900 desktop at 200% has an equivalent 720x450 CSS viewport.
    // This verifies reflow, not physical browser zoom or physical-device input.
    for (const lang of ['fa', 'en']) {
      const context = await browser.newContext({ viewport: { width: 720, height: 450 }, reducedMotion: 'reduce', isMobile: false });
      try {
        await context.addCookies([{ name: 'wg_session', value: seed.session, url: seed.url }, { name: 'wg_theme', value: 'light', url: seed.url }]);
        const page = await context.newPage(); instrument(page);
        await page.request.post(seed.url + '/prefs/locale', { form: { locale: lang, _csrf: seed.csrf } });
        for (const path of ['/users/new', '/settings']) {
          step = 'desktop 200% equivalent viewport ' + lang + (path === '/settings' ? ' settings' : ' user form');
          await navigate(page, path); await noOverflow(page);
          const field = path === '/settings' ? '#s-session_abs' : '#u-duration';
          await page.locator(field).focus(); await focusExposed(page, field);
        }
      } finally { await context.close(); }
    }
    assert(runtimeErrors === 0, 'supplemental interactions raised JavaScript errors');
    assert(failures.length === 0, '44px touch targets failed in ' + failures.length + ' composition checks; safe identities printed above');
    console.log('PASS ' + engineName + ' supplemental interactions: ' + cells + ' touch locale/theme/orientation cells; ' + targetChecks + ' effective targets; drawer/resize, QR fit/close, calendar selection, saved Light/Dark/System and live OS changes, reduced motion, Settings focus, desktop 200% equivalent CSS viewport reflow (not physical zoom/devices).');
  } catch (error) {
    const safe = error.message?.startsWith('contract:') ? error.message : 'browser operation failed; sensitive details suppressed';
    throw new Error('contract: supplemental ' + engineName + ' ' + step + ': ' + safe);
  }
};
