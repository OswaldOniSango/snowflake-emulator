const KEY = "mallard.dock-ratio";
export function clampDockRatio(value: number): number {
  return Number.isFinite(value) ? Math.max(.15, Math.min(.85, value)) : .58;
}
export function loadDockRatio(): number {
  try { const value = localStorage.getItem(KEY); return value === null ? .58 : clampDockRatio(Number(value)); }
  catch { return .58; }
}
/** A proportional splitter keeps both panes usable when the viewport changes. */
export function createDockResize(handle: HTMLElement, editor: HTMLElement, panel: HTMLElement): void {
  let ratio = loadDockRatio();
  let dragging = false;
  function apply(value: number): void {
    ratio = clampDockRatio(value);
    editor.style.flex = `${1 - ratio} 1 0px`;
    panel.style.flex = `${ratio} 1 0px`;
    handle.setAttribute("aria-valuenow", String(Math.round(ratio * 100)));
    handle.setAttribute("aria-valuetext", `Results ${Math.round(ratio * 100)} percent`);
  }
  function save(): void { try { localStorage.setItem(KEY, String(ratio)); } catch { /* Optional preference. */ } }
  handle.setAttribute("role", "separator");
  handle.setAttribute("aria-label", "Resize results panel");
  handle.setAttribute("aria-orientation", "horizontal");
  handle.setAttribute("aria-valuemin", "15");
  handle.setAttribute("aria-valuemax", "85");
  handle.tabIndex = 0;
  handle.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) return;
    dragging = true;
    handle.setPointerCapture(event.pointerId);
    handle.classList.add("dragging");
    event.preventDefault();
  });
  handle.addEventListener("pointermove", (event) => {
    if (!dragging) return;
    const top = editor.getBoundingClientRect().top;
    const bottom = panel.getBoundingClientRect().bottom;
    const available = editor.getBoundingClientRect().height + panel.getBoundingClientRect().height;
    if (available > 0) apply((bottom - Math.max(top, event.clientY)) / available);
  });
  const finish = (): void => { if (dragging) { dragging = false; handle.classList.remove("dragging"); save(); } };
  handle.addEventListener("pointerup", finish);
  handle.addEventListener("pointercancel", finish);
  handle.addEventListener("lostpointercapture", finish);
  handle.addEventListener("keydown", (event) => {
    if (!["ArrowUp", "ArrowDown", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    apply(event.key === "Home" ? .15 : event.key === "End" ? .85 : ratio + (event.key === "ArrowUp" ? .05 : -.05));
    save();
  });
  apply(ratio);
}
