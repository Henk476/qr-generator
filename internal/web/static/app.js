// Progressive enhancement for the server-rendered UI. Every feature here has
// a no-JavaScript fallback in the HTML.
"use strict";
document.documentElement.classList.add("js");

document.addEventListener("DOMContentLoaded", () => {
  // Confirm destructive forms.
  document.querySelectorAll("form[data-confirm]").forEach((form) => {
    form.addEventListener("submit", (e) => {
      if (!window.confirm(form.dataset.confirm)) e.preventDefault();
    });
  });

  const editor = document.getElementById("editor");
  if (!editor) return;
  const preview = document.getElementById("preview");

  // Live preview: re-render on any design or content change, debounced.
  let timer = 0;
  let seq = 0;
  const refresh = async () => {
    const mine = ++seq;
    const body = new URLSearchParams(new FormData(editor));
    body.set("action", "preview");
    preview.querySelector(".qr")?.classList.add("is-loading");
    try {
      const res = await fetch("/preview", { method: "POST", body });
      if (mine !== seq) return; // a newer request superseded this one
      preview.innerHTML = await res.text();
    } catch {
      preview.querySelector(".qr")?.classList.remove("is-loading");
    }
  };
  const schedule = (e) => {
    if (e.target.matches("[data-nopreview]")) return;
    clearTimeout(timer);
    timer = setTimeout(refresh, 150);
  };
  editor.addEventListener("input", schedule);
  editor.addEventListener("change", schedule);

  // Quiet-zone slider readout.
  const quiet = document.getElementById("quiet");
  const quietOut = document.getElementById("quiet-out");
  quiet?.addEventListener("input", () => { quietOut.value = quiet.value; });

  // Applying a preset reloads the form with its design.
  editor.querySelectorAll("[data-autosubmit]").forEach((el) => {
    el.addEventListener("change", () => {
      if (!el.value) return;
      const button = document.getElementById(el.dataset.autosubmit);
      editor.requestSubmit(button);
    });
  });

  // Copy the PNG to the clipboard.
  const copy = document.getElementById("copy-png");
  copy?.addEventListener("click", async () => {
    const label = copy.textContent;
    try {
      const body = new URLSearchParams(new FormData(editor));
      body.set("action", "download");
      body.set("format", "png");
      const res = await fetch(editor.action, { method: "POST", body });
      const type = res.headers.get("Content-Type") || "";
      if (!res.ok || !type.startsWith("image/png")) throw new Error("not an image");
      const blob = await res.blob();
      await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
      copy.textContent = "Copied";
    } catch {
      copy.textContent = "Copy failed";
    }
    setTimeout(() => { copy.textContent = label; }, 1500);
  });
});
