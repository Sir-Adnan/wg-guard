// Routing workspace enhancements. Everything works without JavaScript;
// this only submits a chosen data file immediately instead of requiring a
// second click.
for (const input of document.querySelectorAll("input[type=file][data-auto-submit]")) {
  input.addEventListener("change", () => {
    if (input.files && input.files.length > 0) input.form?.requestSubmit();
  });
}
