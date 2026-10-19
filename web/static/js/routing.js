// Routing workspace enhancements. Everything works without JavaScript; this
// adds the outbound search, "Speed test all", keeps expanded rows open when
// a test swaps them, and submits a chosen data file immediately.
for (const input of document.querySelectorAll("input[type=file][data-auto-submit]")) {
  input.addEventListener("change", () => {
    if (input.files && input.files.length > 0) input.form?.requestSubmit();
  });
}

const panel = document.querySelector(".ob-panel");
if (panel) {
  const search = panel.querySelector("[data-ob-search]");
  const field = search?.querySelector("input");
  const noMatch = panel.querySelector(".ob-no-match");
  const rows = () => [...panel.querySelectorAll(".ob-row")];

  // Text search on top of the CSS state filter.
  const apply = () => {
    const q = (field?.value || "").trim().toLowerCase();
    for (const row of rows()) {
      const hay = (row.dataset.search || "").toLowerCase();
      row.classList.toggle("is-filtered-out", q !== "" && !hay.includes(q));
    }
    if (noMatch) noMatch.hidden = rows().some(row => row.offsetParent !== null);
  };
  if (search && field) {
    search.hidden = false;
    field.addEventListener("input", apply);
  }
  panel.addEventListener("change", event => {
    if (event.target.matches("input[name=ob-filter]")) apply();
  });

  // Expanded rows stay expanded when a test swaps the row or the list.
  let open = new Set();
  document.body.addEventListener("htmx:beforeSwap", event => {
    if (event.detail.target?.closest?.(".ob-list, .ob-row")) {
      open = new Set([...panel.querySelectorAll(".ob-details[open]")].map(d => d.closest(".ob-row").id));
    }
  });
  document.body.addEventListener("htmx:afterSettle", () => {
    for (const id of open) document.getElementById(id)?.querySelector(".ob-details")?.setAttribute("open", "");
    apply();
  });

  // Speed test all: one outbound at a time (the node refuses a second
  // concurrent test), visible rows only, stoppable.
  const all = panel.querySelector("[data-speed-all]");
  if (all && window.htmx) {
    all.hidden = false;
    const label = all.querySelector("span");
    let running = false;
    const setRunning = on => {
      running = on;
      all.setAttribute("aria-pressed", on ? "true" : "false");
      if (label) label.textContent = on ? all.dataset.labelStop : all.dataset.labelRun;
    };
    all.addEventListener("click", async () => {
      if (running) { setRunning(false); return; }
      setRunning(true);
      const queue = rows().filter(row => row.offsetParent !== null && row.dataset.state !== "disabled").map(row => row.id);
      for (const id of queue) {
        if (!running) break;
        const form = document.getElementById(id)?.querySelector("[data-speed-form]");
        if (!form) continue;
        try {
          await window.htmx.ajax("POST", form.getAttribute("hx-post"), { source: form, target: "#" + id, swap: "outerHTML" });
        } catch { /* a failed row shows its own error; continue */ }
      }
      setRunning(false);
    });
  }
}

// Rule editor: search the service tiles (the category filter is CSS), show
// the selection count and a live summary, and suggest a name from the picked
// services while the name field is still untouched.
const ruleForm = document.querySelector(".rule-form");
if (ruleForm) {
  const tiles = [...ruleForm.querySelectorAll(".preset-tile")];
  const picks = [...ruleForm.querySelectorAll("input[name=preset]")];
  const search = ruleForm.querySelector("[data-preset-search]");
  const field = search?.querySelector("input");
  const empty = ruleForm.querySelector(".preset-empty");
  const count = ruleForm.querySelector("[data-preset-count]");
  const countValue = ruleForm.querySelector("[data-preset-count-value]");
  const summary = ruleForm.querySelector("[data-rule-summary]");
  const matchers = ruleForm.querySelector("textarea[name=matchers]");
  const name = ruleForm.querySelector("[data-rule-name]");
  let named = !!name?.value.trim();
  name?.addEventListener("input", () => { named = name.value.trim() !== ""; });

  const filter = () => {
    const q = (field?.value || "").trim().toLowerCase();
    for (const tile of tiles) tile.classList.toggle("is-filtered-out", q !== "" && !(tile.dataset.search || "").toLowerCase().includes(q));
    if (empty) empty.hidden = tiles.some(tile => tile.offsetParent !== null);
  };
  const update = () => {
    const chosen = picks.filter(p => p.checked);
    if (count && countValue) { count.hidden = chosen.length === 0; countValue.textContent = String(chosen.length); }
    const lines = (matchers?.value || "").split("\n").map(l => l.trim()).filter(l => l && !l.startsWith("#")).length;
    if (summary) {
      const parts = [];
      if (chosen.length) parts.push(summary.dataset.fmtServices.replace("{n}", chosen.length));
      if (lines) parts.push(summary.dataset.fmtLines.replace("{n}", lines));
      summary.textContent = parts.length ? parts.join(" · ") : summary.dataset.fmtNone;
    }
    if (name && !named) {
      const names = chosen.map(p => p.dataset.presetName);
      name.value = names.length > 2 ? names.slice(0, 2).join(" + ") + " …" : names.join(" + ");
    }
  };
  if (search && field) { search.hidden = false; field.addEventListener("input", filter); }
  ruleForm.addEventListener("change", event => {
    if (event.target.matches("input[name=preset-cat]")) filter();
    if (event.target.matches("input[name=preset]")) update();
  });
  matchers?.addEventListener("input", update);
  update();
}
