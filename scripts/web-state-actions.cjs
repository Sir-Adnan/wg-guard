// Prepare interactive states on real routes. Diagnostics never include page data.
const { createHash } = require('node:crypto');
let sequence = 0;

async function submit(page, form, expectedStatus) {
  const [response] = await Promise.all([
    page.waitForNavigation(),
    form.locator('button[type="submit"]').last().click(),
  ]);
  if (expectedStatus && response?.status() !== expectedStatus) throw new Error('unexpected form response status');
}

async function disclose(details) {
  if (await details.getAttribute('open') === null) {
    await details.locator(':scope > summary').click();
  }
}

async function userMenu(page) {
  const trigger = page.locator('.page-head .menu-anchor > button[aria-haspopup="menu"]').first();
  if (await trigger.getAttribute('aria-expanded') !== 'true') await trigger.click();
}

async function closeDialog(page, id) {
  const close = page.locator('#' + id + '[open] [data-close-modal]').first();
  if (await close.count()) await close.click();
}

async function cancelRestore(page) {
  const cancel = page.locator('form[action="/backups/restore/cancel"]');
  if (await cancel.count()) {
    await submit(page, cancel.first());
    if (await page.locator('.backup-review,.backup-pending').count()) throw new Error('restore state was not cleared');
  }
}

async function prepare(page, state, label) {
  const mode = state.Prepare;
  if (!mode) return;
  const unique = 'qa-' + createHash('sha256').update(String(label)).digest('hex').slice(0, 12) + '-' + (++sequence);
  let stage = 'initial controls';
  let cleanup;
  try {
    switch (mode) {
      case 'advanced':
        await page.locator('main details').evaluateAll(details => details.forEach(detail => { detail.open = true; }));
        break;

      case 'user-create':
        await page.locator('a[href="/users/new"][data-open-modal="create-drawer"]').click();
        await page.locator('#create-drawer[open]').waitFor();
        cleanup = () => closeDialog(page, 'create-drawer');
        break;

      case 'device-dialog':
      case 'renew-dialog':
      case 'traffic-dialog': {
        const id = { 'device-dialog': 'device-modal', 'renew-dialog': 'renew-modal', 'traffic-dialog': 'traffic-modal' }[mode];
        if (mode !== 'device-dialog') await userMenu(page);
        await page.locator('[data-open-modal="' + id + '"]').first().click();
        await page.locator('#' + id + '[open]').waitFor();
        cleanup = () => closeDialog(page, id);
        break;
      }

      case 'destructive-dialog':
        await userMenu(page);
        await page.locator('.page-head form[action^="/users/"][action$="/delete"] button[type="submit"]').click();
        await page.locator('#confirm-dialog[open]').waitFor();
        cleanup = () => closeDialog(page, 'confirm-dialog');
        break;

      case 'calendar':
        await page.locator('[data-calendar]').first().click();
        await page.locator('.calendar.is-open [data-cal-day][tabindex="0"]').waitFor();
        cleanup = async () => { if (await page.locator('.calendar.is-open').count()) await page.keyboard.press('Escape'); };
        break;

      case 'qr-open':
      case 'qr-loading':
      case 'qr-error': {
        const pattern = /\/devices\/[^/?]+\/qr(?:\?|$)/;
        const held = new Set();
        const intercept = mode === 'qr-loading' ? route => { held.add(route); } : route => route.abort();
        cleanup = async () => {
          await closeDialog(page, 'qr-modal');
          if (mode !== 'qr-open') await page.unroute(pattern, intercept);
          await Promise.all([...held].map(route => route.abort().catch(() => {})));
        };
        if (mode !== 'qr-open') await page.route(pattern, intercept);
        stage = 'QR trigger';
        await page.locator('[data-qr]').first().click();
        await page.locator('#qr-modal[open]').waitFor();
        stage = 'QR state';
        if (mode === 'qr-open') {
          await page.waitForFunction(() => {
            const image = document.querySelector('#qr-img');
            return image && !image.hidden && image.complete && image.naturalWidth > 0;
          });
        } else if (mode === 'qr-error') {
          await page.locator('[data-qr-retry]:visible').waitFor();
        } else {
          await page.waitForFunction(() => document.querySelector('#qr-img')?.hidden && !!document.querySelector('[data-qr-state]')?.textContent.trim());
        }
        break;
      }

      case 'download-error': {
        const pattern = /\/devices\/[^/?]+\/config(?:\?|$)/;
        const intercept = route => route.abort();
        await page.route(pattern, intercept);
        cleanup = () => page.unroute(pattern, intercept);
        await page.locator('[data-config-download]').first().click();
        await page.locator('[data-download-state][role="alert"]').first().waitFor();
        break;
      }

      case 'invalid-user':
        await page.locator('#u-username').fill(unique);
        await page.locator('input[name="device_limit"]').fill('invalid-limit');
        stage = 'user validation submit';
        await submit(page, page.locator('form[action="/users"]'), 422);
        await page.locator('input[name="device_limit"][aria-invalid="true"]').waitFor();
        break;

      case 'invalid-plan':
        await page.locator('#p-name').fill('QA plan');
        await page.locator('#p-device-limit').fill('invalid-limit');
        stage = 'plan validation submit';
        await submit(page, page.locator('form[action="/plans"]'), 422);
        await page.locator('#p-device-limit[aria-invalid="true"]').waitFor();
        break;

      case 'invalid-interface':
        await page.locator('#i-name').fill('awg9');
        await page.locator('#i-port').fill('invalid-port');
        stage = 'interface validation submit';
        await submit(page, page.locator('form[action="/interfaces"]'), 422);
        await page.locator('#i-port[aria-invalid="true"]').waitFor();
        break;

      case 'invalid-settings':
        await page.locator('#s-mtu').fill('invalid-mtu');
        stage = 'settings validation submit';
        await submit(page, page.locator('[data-settings-form]'), 200);
        await page.locator('#s-mtu[aria-invalid="true"]').waitFor();
        break;

      case 'invalid-token':
      case 'token-secret': {
        await disclose(page.locator('#token-create'));
        await page.locator('#ops-name').fill(unique);
        await page.locator('#ops-expires_days').fill(mode === 'invalid-token' ? 'invalid-expiry' : '1');
        const scope = page.locator('input[name="scopes"][value="users.read"]');
        await disclose(page.locator('.ops-scope-group').filter({ has: scope }));
        await scope.check();
        stage = 'token form submit';
        await submit(page, page.locator('form[action="/tokens/create"]'), mode === 'invalid-token' ? 422 : 200);
        if (mode === 'invalid-token') await page.locator('#ops-expires_days[aria-invalid="true"]').waitFor();
        else await page.locator('#token-once').waitFor();
        break;
      }

      case 'webhook-secret':
        await disclose(page.locator('#webhook-create'));
        await page.locator('#ops-url').fill('https://example.com/' + unique);
        await page.locator('input[name="events"][value="user.created"]').check();
        stage = 'webhook form submit';
        await submit(page, page.locator('form[action="/webhooks/create"]'), 200);
        await page.locator('#hook-secret').waitFor();
        break;

      case 'schedule-error':
        await page.locator('#sch-name').fill('QA schedule');
        await page.locator('#sch-kind').selectOption('interval');
        await page.locator('#sch-interval').fill('invalid-interval');
        stage = 'schedule validation submit';
        await submit(page, page.locator('[data-sched-form]'), 422);
        await page.locator('#sch-interval[aria-invalid="true"]').waitFor();
        break;

      case 'restore-review':
      case 'restore-pending':
        await Promise.all([
          page.waitForNavigation(),
          page.locator('.backup-archives a[href^="/backups?restore="]').first().click(),
        ]);
        stage = 'restore preview submit';
        cleanup = () => cancelRestore(page);
        await submit(page, page.locator('form[action="/backups/restore"]'));
        await page.locator('.backup-review').waitFor();
        if (mode === 'restore-pending') {
          stage = 'restore pending confirmation';
          await submit(page, page.locator('form[action="/backups/restore/confirm"]'));
          await page.locator('.backup-pending').waitFor();
        }
        break;

      default:
        throw new Error('unsupported state preparation');
    }
  } catch {
    if (cleanup) await cleanup().catch(() => {});
    throw new Error('contract: state preparation failed at ' + stage);
  }
  if (cleanup) return async () => {
    try { await cleanup(); }
    catch { throw new Error('contract: state preparation cleanup failed'); }
  };
}

module.exports = { prepare };
