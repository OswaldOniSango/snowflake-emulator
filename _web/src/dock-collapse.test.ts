import { describe, expect, it } from "vitest";
import { createDockCollapseToggle, loadDockCollapsed, saveDockCollapsed } from "./dock-collapse";

function fakeStorage(initial: Record<string, string> = {}, failing = false): Storage {
  const data = new Map(Object.entries(initial));
  const boom = () => {
    throw new Error("storage unavailable");
  };
  return {
    get length() {
      return data.size;
    },
    clear: () => data.clear(),
    key: (index: number) => [...data.keys()][index] ?? null,
    getItem: (key: string) => (failing ? boom() : (data.get(key) ?? null)),
    setItem: (key: string, value: string) => {
      if (failing) {
        boom();
      }
      data.set(key, value);
    },
    removeItem: (key: string) => void data.delete(key),
  } as Storage;
}

describe("loadDockCollapsed", () => {
  it("is expanded by default", () => {
    expect(loadDockCollapsed(fakeStorage())).toBe(false);
  });

  it("restores a stored choice", () => {
    expect(loadDockCollapsed(fakeStorage({ "mallard.dock-collapsed": "1" }))).toBe(true);
  });

  it("stays expanded when storage is missing or refuses", () => {
    expect(loadDockCollapsed(null)).toBe(false);
    expect(loadDockCollapsed(fakeStorage({}, true))).toBe(false);
  });
});

describe("saveDockCollapsed", () => {
  it("round-trips", () => {
    const storage = fakeStorage();
    saveDockCollapsed(true, storage);
    expect(loadDockCollapsed(storage)).toBe(true);

    saveDockCollapsed(false, storage);
    expect(loadDockCollapsed(storage)).toBe(false);
  });

  it("says nothing when storage refuses", () => {
    expect(() => saveDockCollapsed(true, fakeStorage({}, true))).not.toThrow();
    expect(() => saveDockCollapsed(true, null)).not.toThrow();
  });
});

describe("createDockCollapseToggle", () => {
  it("hides the dock and flips it back on each click", () => {
    const dock = document.createElement("section");
    document.body.append(dock);

    const button = createDockCollapseToggle(dock);
    expect(dock.hidden).toBe(false);
    expect(button.getAttribute("aria-label")).toBe("Hide results panel");

    button.click();
    expect(dock.hidden).toBe(true);
    expect(button.getAttribute("aria-label")).toBe("Show results panel");

    button.click();
    expect(dock.hidden).toBe(false);
    expect(button.getAttribute("aria-label")).toBe("Hide results panel");
  });
});
