const KEY = "mallard.sidebar-width";
const MIN = 220;
const MAX = 560;
const DEFAULT = 280;
export function clampSidebarWidth(value: number, available = Infinity): number {
  const maximum = Math.max(MIN, Math.min(MAX, available - 325));
  return Math.max(MIN, Math.min(maximum, Number.isFinite(value) ? value : DEFAULT));
}
export function loadSidebarWidth(): number {
  try { const saved = localStorage.getItem(KEY); return saved === null ? DEFAULT : clampSidebarWidth(Number(saved)); }
  catch { return DEFAULT; }
}
/** Resize both explorer tabs together without coupling their width to Focus. */
export function createSidebarResize(handle: HTMLElement, workspace: HTMLElement): void {
  let preferred = loadSidebarWidth();
  let width = preferred;
  let dragging = false;
  function apply(): void {
    const available = workspace.getBoundingClientRect().width;
    width = clampSidebarWidth(preferred, available > 0 ? available : Infinity);
    workspace.style.setProperty("--sidebar-width", `${width}px`);
    handle.setAttribute("aria-valuenow", String(Math.round(width)));
    handle.setAttribute("aria-valuemax", String(clampSidebarWidth(MAX, available > 0 ? available : Infinity)));
    handle.setAttribute("aria-valuetext", `${Math.round(width)} pixels`);
  }
  function save(): void { try { localStorage.setItem(KEY, String(preferred)); } catch { /* Optional preference. */ } }
  function change(value: number): void {
    preferred = clampSidebarWidth(value, workspace.getBoundingClientRect().width || Infinity);
    apply();
  }
  handle.setAttribute("role", "separator");
  handle.setAttribute("aria-label", "Resize workspace sidebar");
  handle.setAttribute("aria-orientation", "vertical");
  handle.setAttribute("aria-valuemin", String(MIN));
  handle.tabIndex = 0;
  handle.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) return;
    dragging = true;
    handle.setPointerCapture(event.pointerId);
    handle.classList.add("dragging");
    event.preventDefault();
  });
  handle.addEventListener("pointermove", (event) => {
    if (dragging) change(event.clientX - workspace.getBoundingClientRect().left);
  });
  function finish(): void {
    if (!dragging) return;
    dragging = false; handle.classList.remove("dragging"); save();
  }
  handle.addEventListener("pointerup", finish);
  handle.addEventListener("pointercancel", finish);
  handle.addEventListener("lostpointercapture", finish);
  handle.addEventListener("keydown", (event) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    change(event.key === "Home" ? MIN : event.key === "End" ? MAX : width + (event.key === "ArrowRight" ? 20 : -20));
    save();
  });
  handle.addEventListener("dblclick", () => { change(DEFAULT); save(); });
  const observer = new ResizeObserver(apply);
  observer.observe(workspace);
  apply();
}
