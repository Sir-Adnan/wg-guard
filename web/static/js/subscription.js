/* Public download enhancement. Native anchors remain the no-JavaScript path. */
document.addEventListener('click', async event => {
  const link = event.target.closest('[data-config-download]');
  if (!link || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  if (link.getAttribute('aria-busy') === 'true') return;
  const state = link.closest('[data-sub-device]').querySelector('[data-download-state]');
  link.setAttribute('aria-busy', 'true');
  link.setAttribute('aria-disabled', 'true');
  state.setAttribute('role', 'status');
  state.textContent = state.dataset.loading;
  try {
    const response = await fetch(link.href, { cache: 'no-store', credentials: 'same-origin', redirect: 'error' });
    if (!response.ok || !response.headers.get('content-type')?.startsWith('application/octet-stream') || !response.headers.get('content-disposition')?.startsWith('attachment;')) {
      state.setAttribute('role', 'alert');
      state.textContent = response.status === 429 ? state.dataset.rateLimit : state.dataset.error;
      return;
    }
    const blob = new Blob([await response.arrayBuffer()], { type: 'application/octet-stream' });
    const objectURL = URL.createObjectURL(blob);
    const download = document.createElement('a');
    download.href = objectURL;
    download.download = /filename="([^"]+)"/.exec(response.headers.get('content-disposition'))?.[1] || 'wg-guard.conf';
    document.body.append(download);
    download.click();
    download.remove();
    setTimeout(() => URL.revokeObjectURL(objectURL), 1000);
    state.textContent = state.dataset.success;
  } catch {
    state.setAttribute('role', 'alert');
    state.textContent = state.dataset.error;
  } finally {
    link.removeAttribute('aria-busy');
    link.removeAttribute('aria-disabled');
  }
});

/* AmneziaVPN handoff. Only the Android app registers vpn://, so Android keeps
 * the native link (the server redirects it to the key). iOS imports a .vpn
 * file from the share sheet; desktop imports it with Add → File. Without
 * JavaScript the same link downloads the file on every non-Android platform. */
const platform = (() => {
  const ua = navigator.userAgent || '';
  if (/Android/i.test(ua)) return 'android';
  return /iPhone|iPad|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) ? 'ios' : 'desktop';
})();

const announce = (node, text, alert = false) => {
  const state = node.closest('[data-sub-device]')?.querySelector('[data-download-state]');
  if (!state) return;
  state.setAttribute('role', alert ? 'alert' : 'status');
  state.textContent = alert ? state.dataset.error : state.dataset[text] || text;
};

async function amneziaFile(link) {
  const response = await fetch(link.dataset.amneziaOpen, { cache: 'no-store', credentials: 'same-origin', redirect: 'error' });
  const disposition = response.headers.get('content-disposition') || '';
  if (!response.ok || !disposition.startsWith('attachment;')) throw new Error(String(response.status));
  const name = /filename="([^"]+)"/.exec(disposition)?.[1] || 'wg-guard.vpn';
  return new File([await response.blob()], name, { type: 'text/plain' });
}

function saveFile(file) {
  const url = URL.createObjectURL(file);
  const anchor = Object.assign(document.createElement('a'), { href: url, download: file.name });
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// iOS shares only from a user gesture. The file is fetched on the first touch;
// when the fetch outlives the gesture, the next tap shares the cached file.
const prepared = new WeakMap();
const prepare = link => {
  if (!prepared.has(link)) prepared.set(link, amneziaFile(link).catch(error => { prepared.delete(link); throw error; }));
  return prepared.get(link);
};
document.addEventListener('pointerdown', event => {
  const link = event.target.closest('[data-amnezia-open]');
  if (link && platform === 'ios') prepare(link).catch(() => {});
});

document.addEventListener('click', async event => {
  const link = event.target.closest('[data-amnezia-open]');
  if (!link || platform === 'android' || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  let file;
  try {
    file = await (platform === 'ios' ? prepare(link) : amneziaFile(link));
  } catch {
    announce(link, 'error', true);
    return;
  }
  if (platform === 'ios' && navigator.canShare?.({ files: [file] })) {
    try {
      await navigator.share({ files: [file] });
    } catch (error) {
      if (error?.name === 'NotAllowedError') announce(link, 'amneziaAgain');
    }
    return;
  }
  saveFile(file);
  announce(link, platform === 'ios' ? 'amneziaIos' : 'amneziaDesktop');
});

/* Copy the AmneziaVPN key (Add → Insert key). Safari keeps the clipboard
 * permission of the tap only when the pending text is handed over at once, so
 * the fetch goes inside a ClipboardItem where supported. */
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-amnezia-copy]');
  if (!button) return;
  const key = fetch(button.dataset.amneziaCopy, { cache: 'no-store', credentials: 'same-origin', redirect: 'error' })
    .then(async response => {
      const text = (await response.text()).trim();
      if (!response.ok || !text.startsWith('vpn://')) throw new Error('unavailable');
      return text;
    });
  try {
    if (window.ClipboardItem && navigator.clipboard?.write) {
      await navigator.clipboard.write([new ClipboardItem({ 'text/plain': key.then(text => new Blob([text], { type: 'text/plain' })) })]);
    } else {
      await navigator.clipboard.writeText(await key);
    }
    announce(button, 'copied');
  } catch {
    announce(button, 'error', true);
  }
});

/* Copy a guide command or path. Without clipboard access the text is selected
 * so the visitor can copy it by hand; the result is announced politely. */
document.querySelectorAll('[data-sub-copy]').forEach(button => { button.hidden = false; });
document.addEventListener('click', async event => {
  const button = event.target.closest('[data-sub-copy]');
  if (!button) return;
  const code = button.parentElement.querySelector('code');
  const status = document.querySelector('[data-sub-copy-status]');
  let copied = false;
  try { await navigator.clipboard.writeText(code.textContent.trim()); copied = true; } catch {
    getSelection()?.selectAllChildren(code);
  }
  if (status) status.textContent = copied ? status.dataset.copied : status.dataset.failed;
  if (!copied) return;
  button.dataset.copied = 'true';
  clearTimeout(button.copyReset);
  button.copyReset = setTimeout(() => { delete button.dataset.copied; }, 1500);
});

/* Section tabs are CSS radio tabs; mirror the chosen section in the address so a
 * link like #guide opens that tab, without moving the page. */
(() => {
  const radios = [...document.querySelectorAll('input[name="sub-section"]')];
  const fromHash = radios.find(radio => '#' + radio.value === location.hash);
  if (fromHash) fromHash.checked = true;
  document.addEventListener('change', event => {
    if (event.target.matches?.('input[name="sub-section"]')) history.replaceState(null, '', '#' + event.target.value);
  });
})();

/* Open the setup guide on the visitor's platform. */
(() => {
  const ua = navigator.userAgent || '';
  const touchMac = /Macintosh/.test(ua) && navigator.maxTouchPoints > 1;
  const os = /Android/i.test(ua) ? 'android' : (/iPhone|iPad|iPod/.test(ua) || touchMac) ? 'ios'
    : /Windows/.test(ua) ? 'windows' : /Macintosh|Mac OS X/.test(ua) ? 'macos' : /Linux|X11/.test(ua) ? 'linux' : '';
  const input = os && document.getElementById('os-' + os);
  if (input) input.checked = true;
  // Keep the chosen tab visible inside the horizontally scrolling tab row
  // without moving the page (scrollIntoView would also scroll vertically).
  const reveal = radio => {
    const label = radio && document.querySelector(`label[for="${radio.id}"]`);
    const row = label?.parentElement;
    if (!row) return;
    const r = row.getBoundingClientRect(), l = label.getBoundingClientRect();
    const delta = l.left < r.left ? l.left - r.left - 8 : l.right > r.right ? l.right - r.right + 8 : 0;
    if (delta) row.scrollBy({ left: delta });
  };
  reveal(document.querySelector('input[name="os"]:checked'));
  document.addEventListener('change', event => {
    if (event.target.matches?.('input[name="os"]')) reveal(event.target);
  });
})();

/* Visitor's own page style: an allowlisted preset ID in a cookie scoped to
 * /sub/ (the server ignores unknown IDs). Blank restores the provider default. */
document.addEventListener('click', event => {
  const choice = event.target.closest('[data-sub-preset]');
  if (!choice) return;
  const id = choice.dataset.subPreset;
  const secure = location.protocol === 'https:' ? '; Secure' : '';
  document.cookie = 'wg_sub_preset=' + encodeURIComponent(id) + '; Path=/sub/; Max-Age=' + (id ? 31536000 : 0) + '; SameSite=Lax' + secure;
  location.reload();
});
