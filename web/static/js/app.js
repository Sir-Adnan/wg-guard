/* WG-Guard panel behaviors — vanilla ES module, CSP-safe (no eval, no
 * inline handlers). Everything is event delegation so htmx swaps never
 * need re-initialization. Zero idle timers: the only animation is the
 * browser painting user actions; polling pauses on hidden tabs. */

(() => {
  "use strict";

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

  /* ---------- theme ---------- */

  const THEME_COOKIE = "wg_theme";

  function setTheme(choice) {
    if (choice === "light" || choice === "dark") {
      document.documentElement.dataset.theme = choice;
    } else {
      choice = "system";
      delete document.documentElement.dataset.theme;
    }
    document.cookie = THEME_COOKIE + "=" + choice +
      ";path=/;max-age=31536000;samesite=lax";
    $$("[data-theme-choice]").forEach((b) =>
      b.setAttribute("aria-pressed", String(b.dataset.themeChoice === choice)));
  }

  /* ---------- toasts ---------- */

  let toastsHost = $(".toasts");
  function toast(message, kind = "ok") {
    if (!toastsHost) {
      toastsHost = document.createElement("div");
      toastsHost.className = "toasts";
      document.body.appendChild(toastsHost);
    }
    const el = document.createElement("div");
    el.className = "toast toast--" + (kind === "err" ? "err" : "ok");
    el.setAttribute("role", "status");
    const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    icon.setAttribute("class", "icon");
    icon.innerHTML = '<use href="/assets/icons.svg#i-' +
      (kind === "err" ? "alert-circle" : "check") + '"/>';
    const text = document.createElement("span");
    text.textContent = message;
    el.append(icon, text);
    toastsHost.appendChild(el);
    setTimeout(() => {
      el.classList.add("is-leaving");
      setTimeout(() => el.remove(), 300);
    }, 3400);
  }

  /* server-driven toasts: HX-Trigger {"wg:toast": {message, kind}} */
  document.body.addEventListener("wg:toast", (e) => {
    const d = e.detail || {};
    const msg = typeof d === "string" ? d : (d.message || d.value || "");
    if (msg) toast(msg, d.kind);
  });

  /* ---------- dropdown menus ---------- */

  function closeMenus(except) {
    $$(".menu.is-open").forEach((m) => {
      if (m !== except) {
        m.classList.remove("is-open");
        m.closest(".menu-anchor")?.querySelector("button")?.setAttribute("aria-expanded", "false");
      }
    });
  }

  document.addEventListener("click", (e) => {
    const btn = e.target.closest(".menu-anchor > button");
    if (btn) {
      const menu = btn.parentElement.querySelector(".menu");
      const open = !menu.classList.contains("is-open");
      closeMenus();
      menu.classList.toggle("is-open", open);
      btn.setAttribute("aria-expanded", String(open));
      return;
    }
    if (!e.target.closest(".menu")) closeMenus();
  });

  /* ---------- dialogs ---------- */

  $$("dialog.modal").forEach((dlg) => {
    // close icon buttons inside modals
    dlg.addEventListener("click", (e) => {
      if (e.target.closest("[data-close-modal]")) dlg.close();
    });
  });

  function openModal(id) {
    const dlg = document.getElementById(id);
    if (dlg && typeof dlg.showModal === "function") dlg.showModal();
    return dlg;
  }

  /* [data-open-modal] buttons open native <dialog> by id */
  document.addEventListener("click", (e) => {
    const opener = e.target.closest("[data-open-modal]");
    if (opener) {
      e.preventDefault();
      openModal(opener.dataset.openModal);
    }
  });

  /* ---------- bulk selection (users table) ---------- */

  document.addEventListener("change", (e) => {
    const all = e.target.id === "sel-all";
    if (all) {
      $$(".js-sel").forEach((c) => { c.checked = e.target.checked; });
    }
    if (!all && !e.target.classList.contains("js-sel")) return;
    const n = $$(".js-sel:checked").length;
    $$("[data-sel-count]").forEach((el) => { el.textContent = n; });
    $$("[data-bulk-bar]").forEach((el) => el.classList.toggle("hidden", n === 0));
    const allBox = $("#sel-all");
    if (allBox) allBox.checked = n > 0 && n === $$(".js-sel").length;
  });

  /* fill selected ids + dynamic confirm message, then let the submit run */
  document.addEventListener("submit", (e) => {
    const f = e.target;
    if (!f.matches("[data-bulk-action]")) return;
    const ids = $$(".js-sel:checked").map((c) => c.value);
    if (!ids.length) { e.preventDefault(); return; }
    const input = f.querySelector('input[name="ids"]');
    if (input) input.value = ids.join(",");
    if (f.dataset.bulkConfirmMsg) {
      f.dataset.confirmMessage = f.dataset.bulkConfirmMsg.replace("%d", String(ids.length));
    }
  }, true); // capture: runs before the confirm-flow listener reads the message

  /* ---------- QR modal ---------- */

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-qr]");
    if (!btn) return;
    e.preventDefault();
    const img = $("#qr-img");
    if (img) img.src = btn.dataset.qr;
    openModal("qr-modal");
  });

  /* ---------- confirm flow ---------- */

  let pendingConfirm = null;

  document.addEventListener("submit", (e) => {
    const form = e.target;
    if (!form.matches("[data-confirm]") || form.dataset.confirmed === "1") return;
    e.preventDefault();
    pendingConfirm = form;
    const dlg = openModal("confirm-dialog");
    if (!dlg) return;
    $("[data-confirm-title]", dlg).textContent = form.dataset.confirmTitle || "";
    const msgEl = $("[data-confirm-message]", dlg);
    msgEl.textContent = form.dataset.confirmMessage || "";
    const okBtn = $("[data-confirm-ok]", dlg);
    okBtn.className = "btn " + (form.dataset.confirmKind === "ok" ? "btn--primary" : "btn--danger");
    okBtn.textContent = form.dataset.confirmLabel || "";
  });

  document.addEventListener("click", (e) => {
    if (!e.target.closest("[data-confirm-ok]")) return;
    const f = pendingConfirm;
    pendingConfirm = null;
    if (!f) return;
    f.dataset.confirmed = "1";
    f.requestSubmit();
    setTimeout(() => delete f.dataset.confirmed, 100);
  });

  /* ---------- copy to clipboard ---------- */

  document.addEventListener("click", async (e) => {
    const btn = e.target.closest("[data-copy]");
    if (!btn) return;
    const target = $(btn.dataset.copy);
    const text = target ? (target.value ?? target.textContent) : btn.dataset.copyValue;
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text.trim());
      toast(btn.dataset.copiedMsg || "Copied", "ok");
    } catch {
      /* clipboard unavailable (permissions/insecure context): silent —
       * the value stays selectable on screen */
    }
  });

  /* ---------- password visibility ---------- */

  document.addEventListener("click", (e) => {
    const t = e.target.closest("[data-toggle-password]");
    if (!t) return;
    const input = document.getElementById(t.dataset.togglePassword);
    if (!input) return;
    const show = input.type === "password";
    input.type = show ? "text" : "password";
    t.setAttribute("aria-pressed", String(show)); // CSS swaps the two icons
  });

  /* ---------- mobile drawer ---------- */

  const sidebar = $("#sidebar");
  const scrim = $("#scrim");
  function setDrawer(open) {
    if (!sidebar) return;
    sidebar.classList.toggle("is-open", open);
    scrim?.classList.toggle("is-open", open);
    $("#btn-drawer")?.setAttribute("aria-expanded", String(open));
  }
  $("#btn-drawer")?.addEventListener("click", () => setDrawer(true));
  scrim?.addEventListener("click", () => setDrawer(false));
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { closeMenus(); setDrawer(false); }
  });
  sidebar?.addEventListener("click", (e) => {
    if (e.target.closest(".nav a")) setDrawer(false);
  });

  /* ---------- htmx integration ---------- */

  document.body.addEventListener("htmx:configRequest", (e) => {
    const meta = $('meta[name="csrf-token"]');
    if (meta) e.detail.headers["X-CSRF-Token"] = meta.content;
  });

  /* dashboards marked data-pause-hidden stop polling in background tabs */
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (document.hidden && e.target.closest?.("[data-pause-hidden]")) {
      e.preventDefault();
    }
  });

  /* fallback error toast when a swap request fails outright */
  document.body.addEventListener("htmx:responseError", (e) => {
    const msg = e.detail?.xhr?.getResponseHeader("X-WG-Error");
    toast(msg || (e.detail?.headers && e.detail.headers["X-WG-Error"]) || "Error", "err");
  });

  /* obfuscation section: disable its inputs while the toggle is off */
  const obfToggle = $("[data-obf-toggle]");
  if (obfToggle) {
    const obfBox = obfToggle.closest(".collapse-body");
    const sync = () => {
      obfBox?.querySelectorAll("input:not([data-obf-toggle])").forEach((inp) => {
        inp.disabled = !obfToggle.checked;
      });
    };
    sync();
    obfToggle.addEventListener("change", sync);
  }

  /* ---------- preset chips (quota / duration quick fill) ---------- */

  document.addEventListener("click", (e) => {
    const chip = e.target.closest("[data-fill-value]");
    if (!chip) return;
    e.preventDefault();
    const field = chip.closest(".field");
    const input = field?.querySelector(".unit-group .input");
    const unit = field?.querySelector(".unit-group .select");
    if (input) {
      input.value = chip.dataset.fillValue;
      input.dispatchEvent(new Event("input", { bubbles: true }));
    }
    if (unit && chip.dataset.fillUnit) unit.value = chip.dataset.fillUnit;
  });

  /* ---------- username generator ---------- */

  const WORDS = ("amber,azure,brave,calm,coral,cosmo,crimson,dawn,delta,dune,eager,echo,ember," +
    "falcon,fjord,garnet,golden,harbor,iris,ivory,jade,lagoon,lunar,maple,meadow,nebula," +
    "nimbus,noble,ocean,onyx,opal,orbit,pearl,polar,quartz,quiet,raven,river,sage,solar," +
    "sprite,storm,tidal,topaz,umber,velvet,zephyr,zenith").split(",");
  const pick = (arr) => arr[Math.floor(Math.random() * arr.length)];

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-generate]");
    if (!btn) return;
    e.preventDefault();
    const input = document.querySelector(btn.dataset.generate);
    if (!input || input.disabled) return;
    input.value = pick(WORDS) + "-" + pick(WORDS) + "-" + Math.floor(Math.random() * 90 + 10);
    input.focus();
  });

  /* ---------- calendar date picker (fa: Jalali · en: Gregorian) ----------
   * Vanilla, CSP-safe. Opens from [data-calendar] triggers, writes an ISO
   * YYYY-MM-DD value into the bound input. Past days are disabled. */

  const CAL_DIV = (a, b) => ~~(a / b);
  const CAL_MOD = (a, b) => a - ~~(a / b) * b;
  function calG2D(gy, gm, gd) {
    let d = CAL_DIV((gy + CAL_DIV(gm - 8, 6) + 100100) * 1461, 4)
      + CAL_DIV(153 * CAL_MOD(gm + 9, 12) + 2, 5) + gd - 34840408;
    d = d - CAL_DIV(CAL_DIV(gy + 100100 + CAL_DIV(gm - 8, 6), 100) * 3, 4) + 752;
    return d;
  }
  function calD2G(jdn) {
    let j = 4 * jdn + 139361631;
    j = j + CAL_DIV(CAL_DIV(4 * jdn + 183187720, 146097) * 3, 4) * 4 - 3908;
    const i = CAL_DIV(CAL_MOD(j, 1461), 4) * 5 + 308;
    return {
      gd: CAL_DIV(CAL_MOD(i, 153), 5) + 1,
      gm: CAL_MOD(CAL_DIV(i, 153), 12) + 1,
      gy: CAL_DIV(j, 1461) - 100100 + CAL_DIV(8 - (CAL_MOD(CAL_DIV(i, 153), 12) + 1), 6),
    };
  }
  function calJalCal(jy) {
    const breaks = [-61, 9, 38, 199, 426, 686, 756, 818, 1111, 1181, 1210, 1635, 2060, 2097,
      2192, 2262, 2324, 2394, 2456, 3178];
    const gy = jy + 621;
    let leapJ = -14, jp = breaks[0], jm, jump = 0, n, i;
    for (i = 1; i < breaks.length; i++) {
      jm = breaks[i];
      jump = jm - jp;
      if (jy < jm) break;
      leapJ = leapJ + CAL_DIV(jump, 33) * 8 + CAL_DIV(CAL_MOD(jump, 33), 4);
      jp = jm;
    }
    n = jy - jp;
    leapJ = leapJ + CAL_DIV(n, 33) * 8 + CAL_DIV(CAL_MOD(n, 33) + 3, 4);
    if (CAL_MOD(jump, 33) === 4 && jump - n === 4) leapJ += 1;
    const leapG = CAL_DIV(gy, 4) - CAL_DIV((CAL_DIV(gy, 100) + 1) * 3, 4) - 150;
    const march = 20 + leapJ - leapG;
    if (jump - n < 6) n = n - jump + CAL_DIV(jump + 4, 33) * 33;
    let leap = CAL_MOD(CAL_MOD(n + 1, 33) - 1, 4);
    if (leap === -1) leap = 4;
    return { leap, gy, march };
  }
  function calD2J(jdn) {
    const gy = calD2G(jdn).gy;
    let jy = gy - 621;
    const r = calJalCal(jy);
    let k = jdn - calG2D(gy, 3, r.march);
    if (k >= 0) {
      if (k <= 185) return { jy, jm: 1 + CAL_DIV(k, 31), jd: CAL_MOD(k, 31) + 1 };
      k -= 186;
    } else {
      jy -= 1;
      k += 179;
      if (r.leap === 1) k += 1;
    }
    return { jy, jm: 7 + CAL_DIV(k, 30), jd: CAL_MOD(k, 30) + 1 };
  }
  function calJ2D(jy, jm, jd) {
    const r = calJalCal(jy);
    return calG2D(r.gy, 3, r.march) + (jm - 1) * 31 - CAL_DIV(jm, 7) * (jm - 7) + jd - 1;
  }
  function calJalMonthLen(jy, jm) {
    // leapValue 0 marks the Kabiseh (leap) year — Esfand gets 30 days
    return jm <= 6 ? 31 : jm <= 11 ? 30 : calJalCal(jy).leap === 0 ? 30 : 29;
  }

  const FA_MONTHS = ["فروردین", "اردیبهشت", "خرداد", "تیر", "مرداد", "شهریور",
    "مهر", "آبان", "آذر", "دی", "بهمن", "اسفند"];
  const EN_MONTHS = ["January", "February", "March", "April", "May", "June", "July",
    "August", "September", "October", "November", "December"];
  const FA_WEEK = ["ش", "ی", "د", "س", "چ", "پ", "ج"]; // Saturday-first
  const EN_WEEK = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"]; // Monday-first

  const isFa = () => (document.documentElement.lang || "fa").startsWith("fa");
  const pad = (n) => String(n).padStart(2, "0");
  const isoOf = (dt) => dt.getFullYear() + "-" + pad(dt.getMonth() + 1) + "-" + pad(dt.getDate());

  let calEl = null;
  let cal = null; // {input, labels, jy, jm, view: "j"|"g", selected: Date|null, today}

  function calFromISO(iso) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(iso || "")) return null;
    const [y, m, d] = iso.split("-").map(Number);
    if (isFa()) {
      const jdn = calG2D(y, m, d);
      const j = calD2J(jdn);
      return { view: "j", jy: j.jy, jm: j.jm, jd: j.jd, g: new Date(y, m - 1, d) };
    }
    return { view: "g", jy: y, jm: m, jd: d, g: new Date(y, m - 1, d) };
  }

  function calOpen(trigger) {
    const input = document.querySelector(trigger.dataset.calendar);
    if (!input || input.disabled) return;
    if (!calEl) {
      calEl = document.createElement("div");
      calEl.className = "calendar";
      document.body.appendChild(calEl);
      calEl.addEventListener("click", (e) => {
        const day = e.target.closest("[data-cal-day]");
        if (day && !day.disabled) { calPick(Number(day.dataset.calDay)); return; }
        if (e.target.closest("[data-cal-prev]")) { calMove(-1); return; }
        if (e.target.closest("[data-cal-next]")) { calMove(1); return; }
        if (e.target.closest("[data-cal-clear]")) {
          cal.input.value = "";
          calClose();
        }
      });
    }
    const existing = calFromISO(input.value);
    const t = new Date();
    cal = {
      input, trigger,
      labels: trigger.dataset,
      today: t,
      view: existing ? existing.view : (isFa() ? "j" : "g"),
      jy: existing ? existing.jy : (isFa() ? calD2J(calG2D(t.getFullYear(), t.getMonth() + 1, t.getDate())).jy : t.getFullYear()),
      jm: existing ? existing.jm : (isFa() ? calD2J(calG2D(t.getFullYear(), t.getMonth() + 1, t.getDate())).jm : t.getMonth() + 1),
      selected: existing ? existing.g : null,
    };
    calRender();
    calEl.classList.add("is-open");
    calPosition();
  }

  function calClose() { calEl?.classList.remove("is-open"); }

  function calMove(dir) {
    if (cal.view === "j") {
      let m = cal.jm + dir, y = cal.jy;
      if (m > 12) { m = 1; y++; }
      if (m < 1) { m = 12; y--; }
      cal.jy = y; cal.jm = m;
    } else {
      let m = cal.jm - 1 + dir, y = cal.jy;
      if (m > 11) { m = 0; y++; }
      if (m < 0) { m = 11; y--; }
      cal.jy = y; cal.jm = m + 1;
    }
    calRender();
  }

  function calPick(day) {
    let y, m, d;
    if (cal.view === "j") {
      const jdn = calJ2D(cal.jy, cal.jm, day);
      const g = calD2G(jdn);
      y = g.gy; m = g.gm; d = g.gd;
    } else {
      y = cal.jy; m = cal.jm; d = day;
    }
    cal.input.value = y + "-" + pad(m) + "-" + pad(d);
    cal.input.dispatchEvent(new Event("change", { bubbles: true }));
    calClose();
  }

  function calRender() {
    const fa = cal.view === "j";
    const title = fa
      ? FA_MONTHS[cal.jm - 1] + " " + cal.jy
      : EN_MONTHS[cal.jm - 1] + " " + cal.jy;
    const week = fa ? FA_WEEK : EN_WEEK;
    // first weekday index (0 = week start) and month length
    let first, len, firstG;
    if (fa) {
      const g1 = calD2G(calJ2D(cal.jy, cal.jm, 1));
      firstG = new Date(g1.gy, g1.gm - 1, g1.gd);
      len = calJalMonthLen(cal.jy, cal.jm);
      // Persian week starts Saturday → getDay(): Sat=6 → 0
      first = (firstG.getDay() + 1) % 7;
    } else {
      firstG = new Date(cal.jy, cal.jm - 1, 1);
      len = new Date(cal.jy, cal.jm, 0).getDate();
      // Gregorian week starts Monday → Mon=1 → 0
      first = (firstG.getDay() + 6) % 7;
    }
    const todayISO = isoOf(cal.today);
    let cells = "";
    for (let i = 0; i < first; i++) cells += "<span></span>";
    for (let d = 1; d <= len; d++) {
      let gy, gm, gd;
      if (fa) {
        const g = calD2G(calJ2D(cal.jy, cal.jm, d));
        gy = g.gy; gm = g.gm; gd = g.gd;
      } else {
        gy = cal.jy; gm = cal.jm; gd = d;
      }
      const iso = gy + "-" + pad(gm) + "-" + pad(gd);
      const past = new Date(gy, gm - 1, gd) < new Date(cal.today.getFullYear(), cal.today.getMonth(), cal.today.getDate());
      const cls = (iso === todayISO ? " is-today" : "") + (cal.selected && iso === isoOf(cal.selected) ? " is-selected" : "");
      cells += '<button type="button" class="cal-day' + cls + '" data-cal-day="' + d + '"' +
        (past ? " disabled" : "") + ">" + d + "</button>";
    }
    calEl.innerHTML =
      '<div class="cal-head">' +
      '<button type="button" class="icon-btn" data-cal-prev aria-label="' + (cal.labels.calPrev || "") + '">‹</button>' +
      '<span class="cal-title" aria-live="polite">' + title + "</span>" +
      '<button type="button" class="icon-btn" data-cal-next aria-label="' + (cal.labels.calNext || "") + '">›</button>' +
      "</div>" +
      '<div class="cal-grid">' + week.map((w) => '<span class="cal-wd">' + w + "</span>").join("") + cells + "</div>" +
      '<div class="cal-foot"><button type="button" class="btn btn--sm btn--ghost" data-cal-clear>' +
      (cal.labels.calClear || "") + "</button></div>";
  }

  function calPosition() {
    const r = cal.trigger.getBoundingClientRect();
    const w = Math.min(296, window.innerWidth - 16);
    calEl.style.width = w + "px";
    let x = r.left + r.width / 2 - w / 2;
    x = Math.max(8, Math.min(x, window.innerWidth - w - 8));
    let y = r.bottom + 6;
    calEl.style.insetInlineStart = "";
    calEl.style.left = x + "px";
    calEl.style.top = y + "px";
    // flip above if clipped at the bottom
    const h = calEl.offsetHeight;
    if (y + h > window.innerHeight - 8) calEl.style.top = Math.max(8, r.top - h - 6) + "px";
  }

  document.addEventListener("click", (e) => {
    const trigger = e.target.closest("[data-calendar]");
    if (trigger) {
      e.preventDefault();
      if (calEl?.classList.contains("is-open") && cal?.trigger === trigger) calClose();
      else calOpen(trigger);
      return;
    }
    if (calEl?.classList.contains("is-open") && !e.target.closest(".calendar")) calClose();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") calClose();
  });
  window.addEventListener("resize", () => { if (calEl?.classList.contains("is-open")) calPosition(); });

  /* ---------- boot ---------- */

  $$("[data-theme-choice]").forEach((b) =>
    b.addEventListener("click", () => setTheme(b.dataset.themeChoice)));

  /* PRG flash toast: show once, then clean the URL */
  const flash = $("[data-toast-msg]");
  if (flash) {
    toast(flash.dataset.toastMsg, "ok");
    history.replaceState(null, "", location.pathname + location.search
      .replace(/([?&])toast=[^&]*&?/, "$1").replace(/([?&])targ=[^&]*&?/, "$1")
      .replace(/[?&]+$/, ""));
  }
})();
