/* Shared on-demand configuration QR viewer for panel and public subscriptions. */
const $ = (selector, root = document) => root.querySelector(selector);
const { openModal } = await import(document.querySelector('meta[name="ui-module"]').content);
/* ---------- QR modal ---------- */

let qrURL = "";
function loadQR() {
  const dialog = $("#qr-modal"), img = $("#qr-img");
  if (!dialog || !img || !qrURL) return;
  const state = $("[data-qr-state]", dialog), retry = $("[data-qr-retry]", dialog);
  img.hidden = true;
  if (retry) retry.hidden = true;
  if (state) state.textContent = state.dataset.loading;
  img.onload = () => {
    if (!dialog.open) return;
    img.hidden = false;
    if (state) state.textContent = "";
  };
  img.onerror = () => {
    if (!dialog.open) return;
    img.hidden = true;
    if (state) state.textContent = state.dataset.error;
    if (retry) retry.hidden = false;
  };
  img.src = qrURL;
}
function clearQR() {
  const img = $("#qr-img");
  if (img) { img.onload = img.onerror = null; img.removeAttribute("src"); img.hidden = true; }
  qrURL = "";
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
  const btn = e.target.closest("[data-qr]");
  if (!btn) return;
  e.preventDefault();
  qrURL = btn.dataset.qr;
  openModal("qr-modal");
  loadQR();
});

window.addEventListener('pagehide', clearQR);
