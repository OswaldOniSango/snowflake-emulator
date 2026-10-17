import { afterEach, describe, expect, it } from "vitest";
import { clampDockRatio, createDockResize, loadDockRatio } from "./dock-resize";
afterEach(() => { localStorage.clear(); document.body.replaceChildren(); });
describe("results splitter", () => {
  it.each([[.2, .2], [-1, .15], [2, .85], [NaN, .58]])("bounds ratio %s", (input, expected) => {
    expect(clampDockRatio(input)).toBe(expected);
  });
  it("loads a safe default for corrupt preferences", () => {
    localStorage.setItem("mallard.dock-ratio", "broken"); expect(loadDockRatio()).toBe(.58);
  });
  it("resizes with the keyboard and remembers the chosen proportion", () => {
    const handle = document.createElement("div"); const editor = document.createElement("div"); const panel = document.createElement("section");
    createDockResize(handle, editor, panel);
    handle.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp" }));
    expect(handle.getAttribute("aria-valuenow")).toBe("63");
    expect(loadDockRatio()).toBeCloseTo(.63);
    handle.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown" }));
    expect(loadDockRatio()).toBeCloseTo(.58);
    handle.dispatchEvent(new KeyboardEvent("keydown", { key: "Home" }));
    expect(loadDockRatio()).toBe(.15);
    handle.dispatchEvent(new KeyboardEvent("keydown", { key: "End" }));
    expect(loadDockRatio()).toBe(.85);
    expect(panel.style.flex).toContain("0.85");
  });
});
