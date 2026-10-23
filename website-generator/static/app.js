/* Go Mechanical Sympathy Workshop — shared client script.
 * Syntax highlighting (highlight.js), copy buttons on code blocks and
 * per-module progress tracking stored in localStorage.
 */

(function () {
  const STORAGE_KEY = "gms.progress.v1";

  // ——— storage helpers ———
  function load(key, fallback) {
    try { return JSON.parse(localStorage.getItem(key)) ?? fallback; }
    catch (e) { return fallback; }
  }
  function save(key, value) {
    try { localStorage.setItem(key, JSON.stringify(value)); } catch (e) {}
  }

  // ——— progress ———
  function getProgress() { return load(STORAGE_KEY, {}); }
  function setComplete(slug, done) {
    const p = getProgress();
    if (done) p[slug] = { done: true, ts: Date.now() };
    else delete p[slug];
    save(STORAGE_KEY, p);
  }
  function isComplete(slug) { return !!getProgress()[slug]; }

  // ——— syntax highlighting ———
  // Only blocks with a language are highlighted: untagged blocks are program
  // output (benchstat tables, pprof listings) and auto-detection mangles them.
  function highlight() {
    if (!window.hljs) return;
    document.querySelectorAll("pre code[class*='language-']").forEach(function (el) {
      window.hljs.highlightElement(el);
    });
  }

  // ——— copy buttons ———
  function wireCopyButtons() {
    document.querySelectorAll("pre").forEach(function (pre) {
      const code = pre.querySelector("code");
      if (!code) return;
      const button = document.createElement("button");
      button.className = "copy-button";
      button.innerHTML = '<i class="far fa-copy"></i>';
      button.title = "Copy to clipboard";
      button.addEventListener("click", function () {
        navigator.clipboard.writeText(code.textContent).then(function () {
          button.innerHTML = '<i class="fas fa-check"></i>';
          button.classList.add("copied");
          setTimeout(function () {
            button.innerHTML = '<i class="far fa-copy"></i>';
            button.classList.remove("copied");
          }, 2000);
        });
      });
      pre.appendChild(button);
    });
  }

  // ——— complete button ———
  function wireCompleteButton() {
    const chapter = document.body.dataset.chapter;
    const btn = document.querySelector(".complete-btn[data-complete]");
    if (!chapter || !btn) return;
    function refresh() {
      if (isComplete(chapter)) {
        btn.dataset.done = "true";
        btn.textContent = btn.dataset.doneLabel;
      } else {
        delete btn.dataset.done;
        btn.textContent = btn.dataset.label;
      }
    }
    refresh();
    btn.addEventListener("click", function () {
      setComplete(chapter, !isComplete(chapter));
      refresh();
      refreshProgressPill();
    });
  }

  // ——— progress counter in the navbar ———
  // Only modules that are part of this build count, so disabled modules
  // completed in an earlier build don't inflate the number.
  function refreshProgressPill() {
    document.querySelectorAll("[data-progress-pill]").forEach(function (pill) {
      const slugs = (pill.dataset.slugs || "").split(",").filter(Boolean);
      const prog = getProgress();
      const done = slugs.filter(function (s) { return prog[s]; }).length;
      const txt = pill.querySelector("[data-progress-text]");
      if (txt) txt.textContent = done + "/" + slugs.length + " " + (pill.dataset.label || "complete");
    });
  }

  // ——— module card statuses on the index ———
  function refreshModuleStatuses() {
    const prog = getProgress();
    document.querySelectorAll(".module-card[data-chapter]").forEach(function (card) {
      if (prog[card.dataset.chapter]) card.dataset.status = "done";
      else delete card.dataset.status;
    });
  }

  function init() {
    highlight();
    wireCopyButtons();
    wireCompleteButton();
    refreshProgressPill();
    refreshModuleStatuses();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
