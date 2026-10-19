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

// Rule editor: the service dropdown is a native disclosure holding the
// checkboxes; this adds its search, closes it on Escape or an outside click,
// mirrors the picks in the "selected" box (with remove buttons), and keeps
// the count, the save-bar summary and a suggested name up to date.
const ruleForm = document.querySelector(".rule-form");
if (ruleForm) {
  const dropdown = ruleForm.querySelector("[data-svc-dropdown]");
  const options = [...ruleForm.querySelectorAll(".svc-option")];
  const picks = [...ruleForm.querySelectorAll("input[name=preset]")];
  const search = ruleForm.querySelector("[data-preset-search]");
  const field = search?.querySelector("input");
  const none = ruleForm.querySelector(".svc-none");
  const empty = ruleForm.querySelector(".svc-empty");
  const count = ruleForm.querySelector("[data-preset-count]");
  const countValue = ruleForm.querySelector("[data-preset-count-value]");
  const summary = ruleForm.querySelector("[data-rule-summary]");
  const matchers = ruleForm.querySelector("textarea[name=matchers]");
  const name = ruleForm.querySelector("[data-rule-name]");
  let named = !!name?.value.trim();
  name?.addEventListener("input", () => { named = name.value.trim() !== ""; });

  const filter = () => {
    const q = (field?.value || "").trim().toLowerCase();
    for (const option of options) option.classList.toggle("is-filtered-out", q !== "" && !(option.dataset.search || "").toLowerCase().includes(q));
    if (none) none.hidden = options.some(option => !option.classList.contains("is-filtered-out"));
  };
  const update = () => {
    const chosen = picks.filter(p => p.checked);
    for (const p of picks) {
      const row = ruleForm.querySelector(`.svc-row[data-preset="${CSS.escape(p.value)}"]`);
      if (row) row.hidden = !p.checked;
    }
    if (empty) empty.hidden = chosen.length > 0;
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

  if (search && field) {
    search.hidden = false;
    field.addEventListener("input", filter);
    // Enter in the search must not submit the rule form.
    field.addEventListener("keydown", event => { if (event.key === "Enter") event.preventDefault(); });
  }
  if (dropdown) {
    dropdown.addEventListener("toggle", () => { if (dropdown.open) field?.focus({ preventScroll: true }); });
    dropdown.addEventListener("keydown", event => {
      if (event.key === "Escape" && dropdown.open) { dropdown.open = false; dropdown.querySelector("summary")?.focus(); }
    });
    document.addEventListener("click", event => { if (dropdown.open && !dropdown.contains(event.target)) dropdown.open = false; });
  }
  for (const button of ruleForm.querySelectorAll("[data-svc-remove]")) {
    button.hidden = false;
    button.addEventListener("click", () => {
      const pick = picks.find(p => p.value === button.dataset.svcRemove);
      if (pick) { pick.checked = false; update(); }
    });
  }
  ruleForm.addEventListener("change", event => { if (event.target.matches("input[name=preset]")) update(); });
  matchers?.addEventListener("input", update);
  update();
}
