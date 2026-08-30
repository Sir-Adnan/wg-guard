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
    t.setAttribute("aria-pressed", String(show));
    // exactly two icons: [eye, eye-off] — swap visibility
    const icons = t.querySelectorAll("svg");
    if (icons[0]) icons[0].classList.toggle("hidden", show);
    if (icons[1]) icons[1].classList.toggle("hidden", !show);
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
