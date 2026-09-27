/* Shared on-demand configuration QR viewer for panel and public subscriptions. */
const $ = (selector, root = document) => root.querySelector(selector);
const { openModal } = await import(document.querySelector('meta[name="ui-module"]').content);
/* ---------- QR modal ---------- */

let qrURL = "";
let revision = 0;
const dialog = () => $("#qr-modal");
const title = () => $("[data-qr-dialog-title]", dialog());

function showSingleView() {
  const modal = dialog();
  const single = $("[data-qr-single]", modal);
  const all = $("[data-qr-all-panel]", modal);
  modal?.classList.remove("modal--qr-all");
  if (single) single.hidden = false;
  if (all) all.hidden = true;
  const heading = title();
  if (heading) heading.textContent = heading.dataset.singleTitle || "";
}

function loadQR() {
  const modal = dialog(), img = $("#qr-img");
  if (!modal || !img || !qrURL) return;
  const state = $("[data-qr-state]", modal), retry = $("[data-qr-retry]", modal);
  img.hidden = true;
  if (retry) retry.hidden = true;
  if (state) state.textContent = state.dataset.loading;
  img.onload = () => {
    if (!modal.open) return;
    img.hidden = false;
    if (state) state.textContent = "";
  };
  img.onerror = () => {
    if (!modal.open) return;
    img.hidden = true;
    if (state) state.textContent = state.dataset.error;
    if (retry) retry.hidden = false;
  };
  // Every open/retry requests the current configuration, including after failures.
  const source = new URL(qrURL, location.href);
  source.searchParams.set('_qr', String(++revision));
  img.src = source.href;
}

function clearAllQR() {
  const grid = $("[data-qr-all-grid]", dialog());
  if (!grid) return;
  grid.querySelectorAll("img").forEach(img => {
    img.onload = img.onerror = null;
    img.removeAttribute("src");
  });
  grid.replaceChildren();
}

function clearQR() {
  const img = $("#qr-img");
  if (img) { img.onload = img.onerror = null; img.removeAttribute("src"); img.hidden = true; }
  qrURL = "";
  clearAllQR();
  showSingleView();
}

function loadAllQR(trigger) {
  const modal = dialog();
  const group = trigger.closest("[data-qr-group]");
  const grid = $("[data-qr-all-grid]", modal);
  const single = $("[data-qr-single]", modal);
  const panel = $("[data-qr-all-panel]", modal);
  if (!modal || !group || !grid || !single || !panel) return;
  const links = [...group.querySelectorAll("[data-qr]:not([data-qr-all])")];
  if (!links.length) return;

  clearAllQR();
  modal.classList.add("modal--qr-all");
  single.hidden = true;
  panel.hidden = false;
  const heading = title();
  if (heading) heading.textContent = trigger.dataset.qrAllTitle || heading.dataset.singleTitle || "";

  openModal("qr-modal", trigger);
  for (const link of links) {
    const card = document.createElement("figure");
    card.className = "qr-all-card";
    const caption = document.createElement("figcaption");
    caption.textContent = link.dataset.qrName || link.textContent.trim();
    caption.dir = "auto";
    const state = document.createElement("span");
    state.className = "hint";
    state.setAttribute("role", "status");
    state.textContent = modal.dataset.qrLoading || "";
    const img = document.createElement("img");
    img.alt = (trigger.dataset.qrAllTitle || heading?.dataset.singleTitle || "QR") + " — " + caption.textContent;
    img.width = 240;
    img.height = 240;
    img.hidden = true;
    img.onload = () => {
      if (!modal.open) return;
      img.hidden = false;
      state.textContent = "";
    };
    img.onerror = () => {
      if (!modal.open) return;
      state.textContent = modal.dataset.qrError || "";
    };
    const source = new URL(link.dataset.qr, location.href);
    source.searchParams.set("_qr", String(++revision));
    img.src = source.href;
    card.append(caption, state, img);
    grid.append(card);
  }
}
document.addEventListener("close", (e) => {
  if (e.target.id === "qr-modal") clearQR();
}, true);
document.addEventListener("cancel", (e) => {
  if (e.target.id === "qr-modal") clearQR();
}, true);
document.addEventListener("click", (e) => {
  if (e.target.closest("#qr-modal [data-close-modal]")) clearQR();
}, true);
document.addEventListener("click", (e) => {
  if (e.target.closest("[data-qr-retry]")) { loadQR(); return; }
  const all = e.target.closest("[data-qr-all]");
  if (all) {
    e.preventDefault();
    loadAllQR(all);
    return;
  }
  const btn = e.target.closest("[data-qr]");
  if (!btn) return;
  e.preventDefault();
  showSingleView();
  qrURL = btn.dataset.qr;
  openModal("qr-modal", btn);
  loadQR();
});

window.addEventListener('pagehide', clearQR);
