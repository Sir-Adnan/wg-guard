/* Page-specific form behaviors — vanilla ES module, CSP-safe (no eval or
 * inline handlers). Shared navigation, overlays, themes and request lifecycle
 * live in ui.js. Forms keep their server-rendered values and validators. */

(async () => {
  "use strict";

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

  const { openModal, toast } = await import(document.querySelector('meta[name="ui-module"]').content);

  /* ---------- show-once secrets: select on focus ---------- */
  document.addEventListener("focusin", (e) => {
    if (e.target.matches("[data-token-once]")) e.target.select();
  });
  window.addEventListener("pagehide", () => {
    $$("[data-token-once]").forEach(input => { input.value = ""; input.removeAttribute("value"); });
  });

  /* ---------- schedule form progressive disclosure ---------- */
  const schedForm = $('[data-sched-form]');
  if (schedForm) {
    const kindInput = $('[data-sched-kind-input]', schedForm);
    const setKind = () => {
      const kind = kindInput.value;
      $$('[data-sched-panel]', schedForm).forEach(panel => {
        const visible = panel.dataset.schedPanel.split(',').includes(kind);
        panel.hidden = !visible;
        $$('input,select', panel).forEach(el => { el.disabled = !visible; });
      });
      const weekday = $('[data-sched-weekday-row]', schedForm);
      if (weekday) {
        weekday.hidden = kind !== 'weekly';
        $('select', weekday).disabled = kind !== 'weekly';
      }
    };
    kindInput.addEventListener('change', setKind);
    setKind();
  }
  /* ---------- bulk selection (users table) ---------- */

  function updateBulkSelection() {
    const n = $$(".js-sel:checked").length;
    $$("[data-sel-count]").forEach((el) => { el.textContent = n; });
    $$("[data-bulk-bar]").forEach((el) => el.classList.toggle("hidden", n === 0));
    const allBox = $("#sel-all");
    if (allBox) {
      allBox.checked = n > 0 && n === $$(".js-sel").length;
      allBox.indeterminate = n > 0 && n < $$(".js-sel").length;
    }
    $$("[data-bulk-action] button[type='submit']").forEach(button => { button.disabled = n === 0; });
  }
  updateBulkSelection();
  document.body.addEventListener("htmx:afterSwap", updateBulkSelection);
  document.addEventListener("change", (e) => {
    const all = e.target.id === "sel-all";
    if (all) $$(".js-sel").forEach((c) => { c.checked = e.target.checked; });
    if (all || e.target.classList.contains("js-sel")) updateBulkSelection();
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
    const action = f.querySelector('[name="action"]');
    if (action) {
      f.dataset.confirmTitle = action.selectedOptions[0].textContent;
      f.dataset.confirmKind = action.value === "delete" ? "danger" : "ok";
    }
  }, true); // capture: runs before the confirm-flow listener reads the message

  /* ---------- confirm flow ---------- */

  let pendingConfirm = null;

  document.addEventListener("submit", (e) => {
    const form = e.target;
    if (!form.matches("[data-confirm]") || form.dataset.confirmed === "1") return;
    e.preventDefault();
    pendingConfirm = { form, submitter: e.submitter };
    const dlg = openModal("confirm-dialog", e.submitter || document.activeElement);
    if (!dlg) return;
    $("[data-confirm-title]", dlg).textContent = form.dataset.confirmTitle || "";
    const msgEl = $("[data-confirm-message]", dlg);
    msgEl.textContent = form.dataset.confirmMessage || "";
    const okBtn = $("[data-confirm-ok]", dlg);
    okBtn.className = "btn " + (form.dataset.confirmKind === "ok" ? "btn--primary" : "btn--danger");
    okBtn.textContent = form.dataset.confirmLabel || e.submitter?.getAttribute('aria-label') || e.submitter?.textContent?.trim() || document.querySelector('meta[name="ui-confirm"]')?.content || "";
  });

  document.addEventListener("click", (e) => {
    if (!e.target.closest("[data-confirm-ok]")) return;
    const pending = pendingConfirm;
    pendingConfirm = null;
    if (!pending) return;
    const f = pending.form;
    document.getElementById('confirm-dialog')?.close();
    f.dataset.confirmed = "1";
    f.requestSubmit(pending.submitter || undefined);
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
      toast(btn.dataset.copiedMsg || document.querySelector('meta[name="ui-copied"]').content, "ok");
    } catch {
      toast(document.querySelector('meta[name="ui-copy-error"]').content, "err");
    }
  });

  /* Obfuscation profiles are generated and validated on the server. The
   * browser only requests a policy and populates the returned form values. */
  const obfToggle = $("[data-obf-toggle]");
  if (obfToggle) {
  const obfBox = obfToggle.closest("[data-obf-box]") || obfToggle.closest(".collapse-body");
    const profilePolicy = $("[data-profile-policy]");
    const profileToken = $("[data-profile-token]");
    const profileLabel = $('[data-profile-label]');
    const syncPolicyLabel = () => {
    const policy = profilePolicy?.value || 'plain';
    if (profileLabel) profileLabel.textContent = profileLabel.dataset['policy' + policy.replace(/^./, c => c.toUpperCase())] || profileLabel.dataset.policyCustom;
    $$('[data-generate-obf],[data-profile-plain]', obfBox).forEach(button => {
      const selected = button.hasAttribute('data-profile-plain') ? policy === 'plain' : button.dataset.generateObf === policy;
      button.setAttribute('aria-pressed', String(selected));
    });
    };
    let applyingProfile = false;
    let generatingProfile = false;
    const sync = () => {
      const fields = $("#obf-fields");
      if (fields) fields.hidden = !obfToggle.checked;
      obfBox?.querySelectorAll("input:not([data-obf-toggle])").forEach((inp) => {
        inp.disabled = !obfToggle.checked;
      });
    };

    const generate = async (policy, source) => {
      if (generatingProfile) return;
      const csrf = $('meta[name="csrf-token"]')?.content;
      if (!csrf) {
        toast(source?.dataset.generationError || "Error", "err");
        return;
      }
      generatingProfile = true;
      obfBox?.classList.add('is-generating');
      obfBox?.setAttribute('aria-busy', 'true');
      const buttons = $$('[data-generate-obf]', obfBox);
      buttons.forEach((button) => { button.disabled = true; });
      try {
        const response = await fetch("/interfaces/profile-preview", {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
            "X-CSRF-Token": csrf,
          },
          body: new URLSearchParams({ policy }),
        });
        if (!response.ok) throw new Error("profile generation failed");
        const payload = await response.json();
        if (!payload?.fields || !payload.token || payload.policy !== policy) throw new Error("invalid profile response");

        applyingProfile = true;
        obfToggle.checked = true;
        sync();
        for (const [name, value] of Object.entries(payload.fields)) {
    const input = obfBox.querySelector('input[name="' + name + '"]') || obfToggle.form?.querySelector('[name="' + name + '"]');
          if (!input) continue;
          if (input.type === "checkbox") input.checked = value === "1";
          else input.value = value;
        }
        if (profilePolicy) profilePolicy.value = payload.policy;
        if (profileToken) profileToken.value = payload.token;
        syncPolicyLabel();
        const advanced = $('#awg-advanced');
    if (advanced) advanced.open = true;
      } catch {
        toast(source?.dataset.generationError || "Error", "err");
      } finally {
        applyingProfile = false;
        generatingProfile = false;
        obfBox?.classList.remove('is-generating');
        obfBox?.removeAttribute('aria-busy');
        buttons.forEach((button) => { button.disabled = false; });
      }
    };

  const clearProfile = () => {
    applyingProfile = true;
    obfToggle.checked = false;
    obfBox?.querySelectorAll('#obf-fields input').forEach(input => {
      if (input.type === 'checkbox') input.checked = false;
      else input.value = '';
    });
    if (profilePolicy) profilePolicy.value = 'plain';
    if (profileToken) profileToken.value = '';
    sync();
    syncPolicyLabel();
    applyingProfile = false;
  };

    sync();
  syncPolicyLabel();
    obfToggle.addEventListener("change", () => {
      sync();
      if (applyingProfile) return;
      if (!obfToggle.checked) {
        if (profilePolicy) profilePolicy.value = "plain";
        if (profileToken) profileToken.value = "";
    syncPolicyLabel();
        return;
      }
      if (profilePolicy) profilePolicy.value = "custom";
      if (profileToken) profileToken.value = "";
      syncPolicyLabel();
    const suggested = obfBox.querySelector('[data-generate-obf="suggested"]');
    generate("suggested", suggested);
    });

    const markCustom = (event) => {
      if (applyingProfile || event.target === obfToggle || !obfToggle.checked) return;
      if (profilePolicy) profilePolicy.value = "custom";
      if (profileToken) profileToken.value = "";
      syncPolicyLabel();
    };
    obfBox.addEventListener("input", markCustom);
    obfBox.addEventListener("change", markCustom);
    obfBox.addEventListener("click", (event) => {
    const plain = event.target.closest("[data-profile-plain]");
    if (plain) {
    event.preventDefault();
    clearProfile();
    return;
    }
      const button = event.target.closest("[data-generate-obf]");
      if (!button) return;
      event.preventDefault();
      generate(button.dataset.generateObf, button);
    });
  if (obfBox.dataset.autoloadProfile) {
    const policy = obfBox.dataset.autoloadProfile;
    generate(policy, obfBox.querySelector('[data-generate-obf="' + policy + '"]'));
  }
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
    const group = chip.closest(".chips");
    if (group) {
      $$("[data-fill-value]", group).forEach((c) =>
        c.classList.toggle("is-active", c === chip));
    }
  });

  /* ---------- username generator ---------- */

    const WORDS = ("amber,azure,brave,coral,cosmo,delta,eager,ember,fjord,lunar,maple,misty," +
      "noble,ocean,pearl,polar,quiet,raven,river,solar,storm,tidal,topaz,umber,vivid,zesty").split(",");
  const pick = (arr) => arr[Math.floor(Math.random() * arr.length)];

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-generate]");
    if (!btn) return;
    e.preventDefault();
    const input = document.querySelector(btn.dataset.generate);
    if (!input || input.disabled) return;
      input.value = pick(WORDS) + String(Math.floor(Math.random() * 1000)).padStart(3, "0");
      input.dispatchEvent(new Event("input", { bubbles: true }));
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
    const date = new Date(y, m - 1, d);
    if (date.getFullYear() !== y || date.getMonth() !== m - 1 || date.getDate() !== d) return null;
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
      calEl.id = "date-calendar";
      calEl.setAttribute("role", "dialog");
      calEl.setAttribute("aria-labelledby", "calendar-title");
      // A <dialog> renders in the top layer: anything appended to <body>
      // paints BELOW it. Mount the popover inside the dialog when the
      // trigger lives in one (create-user drawer), otherwise on <body>.
      (trigger.closest("dialog") || document.body).appendChild(calEl);
      calEl.addEventListener("click", (e) => {
        const day = e.target.closest("[data-cal-day]");
        if (day && !day.disabled) { calPick(Number(day.dataset.calDay)); return; }
        if (e.target.closest("[data-cal-prev]")) { calMove(-1); return; }
        if (e.target.closest("[data-cal-next]")) { calMove(1); return; }
        if (e.target.closest("[data-cal-clear]")) {
          cal.input.value = "";
          cal.input.dispatchEvent(new Event("change", { bubbles: true }));
          calClose();
        }
      });
    }
    (trigger.closest("dialog") || document.body).appendChild(calEl);
    cal?.trigger?.setAttribute("aria-expanded", "false");
    trigger.setAttribute("aria-expanded", "true");
    trigger.setAttribute("aria-controls", calEl.id);
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
      focusDate: existing && existing.g >= new Date(t.getFullYear(), t.getMonth(), t.getDate()) ? existing.g : new Date(t.getFullYear(), t.getMonth(), t.getDate()),
    };
    calSetView(cal.focusDate);
    calRender();
    calEl.classList.add("is-open");
    calPosition();
    $("[data-cal-day][tabindex='0']", calEl)?.focus({ preventScroll: true });
  }

  function calClose(returnFocus = true) {
    if (!calEl?.classList.contains("is-open")) return;
    calEl.classList.remove("is-open");
    cal.trigger.setAttribute("aria-expanded", "false");
    if (returnFocus) cal.trigger.focus({ preventScroll: true });
  }

  function calSetView(date) {
    const parts = calFromISO(isoOf(date));
    cal.jy = parts.jy; cal.jm = parts.jm;
  }

  function calFocus(date) {
    const today = new Date(cal.today.getFullYear(), cal.today.getMonth(), cal.today.getDate());
    cal.focusDate = date < today ? today : date;
    calSetView(cal.focusDate);
    calRender();
    $("[data-cal-day][tabindex='0']", calEl)?.focus({ preventScroll: true });
  }

  function calMove(dir) {
    const day = calFromISO(isoOf(cal.focusDate)).jd;
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
    const len = cal.view === "j" ? calJalMonthLen(cal.jy, cal.jm) : new Date(cal.jy, cal.jm, 0).getDate();
    const d = Math.min(day, len);
    const g = cal.view === "j" ? calD2G(calJ2D(cal.jy, cal.jm, d)) : { gy: cal.jy, gm: cal.jm, gd: d };
    calFocus(new Date(g.gy, g.gm - 1, g.gd));
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
      const label = new Intl.DateTimeFormat(fa ? "fa-IR" : "en", { dateStyle: "full" }).format(new Date(gy, gm - 1, gd));
      cells += '<button type="button" class="cal-day' + cls + '" data-cal-day="' + d + '"' +
        ' tabindex="' + (iso === isoOf(cal.focusDate) ? '0' : '-1') + '" aria-label="' + label + '"' +
        (iso === todayISO ? ' aria-current="date"' : '') +
        ' aria-pressed="' + Boolean(cal.selected && iso === isoOf(cal.selected)) + '"' +
        (past ? " disabled" : "") + ">" + d + "</button>";
    }
    calEl.innerHTML =
      '<div class="cal-head">' +
      '<button type="button" class="icon-btn" data-cal-prev aria-label="' + (cal.labels.calPrev || "") + '">‹</button>' +
      '<span id="calendar-title" class="cal-title" aria-live="polite">' + title + "</span>" +
      '<button type="button" class="icon-btn" data-cal-next aria-label="' + (cal.labels.calNext || "") + '">›</button>' +
      "</div>" +
      '<div class="cal-grid">' + week.map((w) => '<span class="cal-wd">' + w + "</span>").join("") + cells + "</div>" +
      '<div class="cal-foot"><button type="button" class="btn btn--sm btn--ghost" data-cal-clear>' +
      (cal.labels.calClear || "") + "</button></div>";
  }

  function calPosition() {
    const r = cal.trigger.getBoundingClientRect();
    const touch = matchMedia("(pointer: coarse)").matches || window.innerWidth <= 800;
    // Seven 44px day targets need 338px with regular padding/gaps. At 320px,
    // CSS removes grid gaps and uses 5px padding; no outer gutter is possible.
    const gutter = touch && window.innerWidth < 354 ? 0 : 8;
    const w = Math.min(touch ? 338 : 296, window.innerWidth - gutter * 2);
    calEl.style.width = w + "px";
    let x = r.left + r.width / 2 - w / 2;
    x = Math.max(gutter, Math.min(x, window.innerWidth - w - gutter));
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
    if (calEl?.classList.contains("is-open") && !e.target.closest(".calendar")) calClose(false);
  });
  document.addEventListener("keydown", (e) => {
    if (!calEl?.classList.contains("is-open")) return;
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); calClose(); return; }
    if (!e.target.matches("[data-cal-day]")) return;
    if (["PageUp", "PageDown"].includes(e.key)) { e.preventDefault(); calMove(e.key === "PageUp" ? -1 : 1); return; }
    const date = new Date(cal.focusDate);
    const weekday = (date.getDay() + (cal.view === "j" ? 1 : 6)) % 7;
    const rtl = getComputedStyle(calEl).direction === "rtl";
    const offsets = { ArrowLeft: rtl ? 1 : -1, ArrowRight: rtl ? -1 : 1, ArrowUp: -7, ArrowDown: 7, Home: -weekday, End: 6 - weekday };
    if (!(e.key in offsets)) return;
    e.preventDefault();
    date.setDate(date.getDate() + offsets[e.key]);
    calFocus(date);
  }, true);
  document.addEventListener("focusin", e => {
    if (calEl?.classList.contains("is-open") && !calEl.contains(e.target) && e.target !== cal.trigger) calClose(false);
  });
  document.addEventListener("close", e => { if (e.target.contains?.(calEl)) calClose(false); }, true);
  window.addEventListener("resize", () => { if (calEl?.classList.contains("is-open")) calPosition(); });

})();
