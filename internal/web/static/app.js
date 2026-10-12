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

  // The current value of a named control: checked radio, ticked checkbox
  // ("" when unticked) or plain value.
  const valueOf = (name) => {
    const els = editor.querySelectorAll(`[name="${name}"]`);
    for (const el of els) {
      if (el.type === "radio" || el.type === "checkbox") {
        if (el.checked) return el.value;
      } else {
        return el.value;
      }
    }
    return "";
  };

  // Show option groups only when they apply: data-when names a control,
  // data-is lists the values that show it ("*" = any non-empty value).
  const conditionals = editor.querySelectorAll("[data-when]");
  const applyConditions = () => {
    conditionals.forEach((el) => {
      const v = valueOf(el.dataset.when);
      const allowed = el.dataset.is.split(",");
      el.hidden = !(allowed.includes("*") ? v !== "" : allowed.includes(v));
    });
  };
  applyConditions();

  // Slider readouts.
  editor.querySelectorAll("output[data-mirror]").forEach((out) => {
    const input = document.getElementById(out.dataset.mirror);
    input?.addEventListener("input", () => { out.value = input.value; });
  });

  // Live preview: re-render on any design or content change, debounced.
  let timer = 0;
  let seq = 0;
  const refresh = async () => {
    const mine = ++seq;
    const fd = new FormData(editor);
    fd.delete("logo_file"); // uploads go through the Upload action only
    const body = new URLSearchParams(fd);
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
    applyConditions();
    if (e.target.matches("[data-nopreview]")) return;
    clearTimeout(timer);
    timer = setTimeout(refresh, 150);
  };
  editor.addEventListener("input", schedule);
  editor.addEventListener("change", schedule);

  // Choosing a preset or a logo file submits the matching action.
  editor.querySelectorAll("[data-autosubmit]").forEach((el) => {
    el.addEventListener("change", () => {
      if (!el.value) return;
      editor.requestSubmit(document.getElementById(el.dataset.autosubmit));
    });
  });

  // Copy the PNG to the clipboard.
  const copy = document.getElementById("copy-png");
  copy?.addEventListener("click", async () => {
    const label = copy.textContent;
    try {
      const fd = new FormData(editor);
      fd.delete("logo_file");
      const body = new URLSearchParams(fd);
      body.set("action", "download");
      body.set("format", "png");
      const res = await fetch(editor.action, { method: "POST", body });
      const type = res.headers.get("Content-Type") || "";
      if (!res.ok || !type.startsWith("image/png")) throw new Error("not an image");
      const blob = await res.blob();
      await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
      copy.textContent = copy.dataset.done || "Copied";
    } catch {
      copy.textContent = copy.dataset.failed || "Copy failed";
    }
    setTimeout(() => { copy.textContent = label; }, 1500);
  });
});
