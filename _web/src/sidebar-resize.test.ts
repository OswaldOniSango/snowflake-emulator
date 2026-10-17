import { afterEach, describe, expect, it, vi } from "vitest";
import { clampSidebarWidth, createSidebarResize, loadSidebarWidth } from "./sidebar-resize";
afterEach(() => { localStorage.clear(); document.body.replaceChildren(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
function setup() {
  let available = 1200;
  let resize: () => void = () => {};
  vi.stubGlobal("ResizeObserver", class { constructor(callback: () => void) { resize = callback; } observe() {} });
  const workspace = document.createElement("main"); const handle = document.createElement("div"); workspace.append(handle); document.body.append(workspace);
  vi.spyOn(workspace, "getBoundingClientRect").mockImplementation(() => ({ left: 100, width: available }) as DOMRect);
  handle.setPointerCapture = vi.fn();
  createSidebarResize(handle, workspace);
  return { workspace, handle, resize: (width: number) => { available = width; resize(); } };
}
describe("workspace sidebar resize", () => {
  it.each([[NaN, 280], [0, 220], [1000, 560]])("bounds saved width %s", (value, expected) => expect(clampSidebarWidth(value)).toBe(expected));
  it("handles corrupt preferences", () => { localStorage.setItem("mallard.sidebar-width", "bad"); expect(loadSidebarWidth()).toBe(280); });
  it("supports keyboard resizing, persistence and reset", () => {
    const { workspace, handle } = setup();
    handle.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight" }));
    expect(workspace.style.getPropertyValue("--sidebar-width")).toBe("300px");
    expect(loadSidebarWidth()).toBe(300);
    handle.dispatchEvent(new KeyboardEvent("keydown", { key: "Home" })); expect(loadSidebarWidth()).toBe(220);
    handle.dispatchEvent(new KeyboardEvent("keydown", { key: "End" })); expect(loadSidebarWidth()).toBe(560);
    handle.dispatchEvent(new MouseEvent("dblclick")); expect(loadSidebarWidth()).toBe(280);
  });
  it("clamps on viewport changes without forgetting the preferred width", () => {
    localStorage.setItem("mallard.sidebar-width", "500");
    const { handle, resize } = setup();
    resize(700); expect(handle.getAttribute("aria-valuenow")).toBe("375");
    expect(handle.getAttribute("aria-valuemax")).toBe("375");
    resize(1200); expect(handle.getAttribute("aria-valuenow")).toBe("500");
  });
  it("drags from the workspace edge and saves on release", () => {
    const { handle } = setup();
    handle.dispatchEvent(new MouseEvent("pointerdown", { button: 0 }));
    handle.dispatchEvent(new MouseEvent("pointermove", { clientX: 500 }));
    expect(handle.getAttribute("aria-valuenow")).toBe("400");
    handle.dispatchEvent(new MouseEvent("pointerup"));
    expect(loadSidebarWidth()).toBe(400);
    handle.dispatchEvent(new MouseEvent("pointermove", { clientX: 650 }));
    expect(handle.getAttribute("aria-valuenow")).toBe("400");
  });
});
