import { afterEach, describe, expect, it, vi } from "vitest";
import { createRunMenu, type RunMenu } from "./run-menu";
let control: RunMenu | undefined;
afterEach(() => { control?.destroy(); document.body.replaceChildren(); });
function setup(count = 2) {
  const parent = document.createElement("div"); document.body.append(parent);
  const current = vi.fn(); const all = vi.fn();
  control = createRunMenu(parent, current, all, "Ctrl+↵");
  control.setStatementCount(count);
  const toggle = parent.querySelector<HTMLButtonElement>('[data-role="run-menu-toggle"]')!;
  const menu = parent.querySelector<HTMLElement>('[role="menu"]')!;
  return { parent, current, all, toggle, menu, control };
}
describe("split Run control", () => {
  it("runs only the current-query callback from the primary button", () => {
    const { current, all, control } = setup(); control.runButton.click();
    expect(current).toHaveBeenCalledOnce(); expect(all).not.toHaveBeenCalled();
  });
  it("offers only Run all with its shortcut on a second line", () => {
    const { parent, current, all, toggle, menu, control } = setup();
    toggle.click(); expect(menu.hidden).toBe(false);
    expect(parent.querySelectorAll('[role="menuitem"]')).toHaveLength(1);
    expect(control.runAllButton.querySelector("span")!.textContent).toBe("Run all");
    expect(control.runAllButton.querySelector("kbd")!.textContent).toBe("Ctrl + Shift + Enter");
    control.runAllButton.click();
    expect(all).toHaveBeenCalledOnce(); expect(current).not.toHaveBeenCalled();
    expect(menu.hidden).toBe(true);
  });
  it.each([0, 1, 3])("handles a worksheet with %i statements", (count) => {
    const { toggle, control } = setup(count);
    expect(control.runAllButton.querySelector("span")!.textContent).toBe("Run all");
    expect(toggle.disabled).toBe(count === 0);
    expect(control.runButton.disabled).toBe(count === 0);
  });
  it("disables all execution controls while running", () => {
    const { current, all, toggle, menu, control } = setup();
    toggle.click(); control.setRunning(true);
    expect(menu.hidden).toBe(true); expect(toggle.disabled).toBe(true);
    control.runButton.click(); control.runAllButton.click();
    expect(current).not.toHaveBeenCalled(); expect(all).not.toHaveBeenCalled();
    control.setRunning(false); expect(toggle.disabled).toBe(false);
  });
  it("closes on outside click and Escape restores focus", () => {
    const { parent, toggle, menu } = setup(); toggle.click();
    parent.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(menu.hidden).toBe(true); expect(document.activeElement).toBe(toggle);
    toggle.click(); document.body.click(); expect(menu.hidden).toBe(true);
  });
  it("navigates menu items with the arrow keys", () => {
    const { toggle, control } = setup();
    toggle.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    expect(document.activeElement).toBe(control.runAllButton);
    control.runAllButton.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    expect(document.activeElement).toBe(control.runAllButton);
  });
});
